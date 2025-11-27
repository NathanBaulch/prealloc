package pkg

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"strconv"

	"golang.org/x/tools/go/analysis"
)

type sliceDeclaration struct {
	name       string
	pos        token.Pos
	eligible   bool
	ineligible bool
	capExpr    ast.Expr
}

type returnsVisitor struct {
	// flags
	simple            bool
	includeRangeLoops bool
	includeForLoops   bool
	// visitor fields
	sliceDeclarations []*sliceDeclaration
	preallocHints     []analysis.Diagnostic
}

func Check(files []*ast.File, simple, includeRangeLoops, includeForLoops bool) []analysis.Diagnostic {
	var hints []analysis.Diagnostic
	for _, f := range files {
		retVis := &returnsVisitor{
			simple:            simple,
			includeRangeLoops: includeRangeLoops,
			includeForLoops:   includeForLoops,
		}
		ast.Walk(retVis, f)
		hints = append(hints, retVis.preallocHints...)
	}

	return hints
}

func (v *returnsVisitor) Visit(node ast.Node) ast.Visitor {
	v.sliceDeclarations = nil

	blockStmt, ok := node.(*ast.BlockStmt)
	if !ok {
		return v
	}

	for _, stmt := range blockStmt.List {
		switch s := stmt.(type) {
		// Find non pre-allocated slices
		case *ast.DeclStmt:
			genD, ok := s.Decl.(*ast.GenDecl)
			if !ok || genD.Tok != token.VAR {
				continue
			}
			for _, spec := range genD.Specs {
				vSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}

				if len(vSpec.Values) == 0 {
					if _, ok := inferExprType(vSpec.Type).(*ast.ArrayType); ok {
						for _, vName := range vSpec.Names {
							v.sliceDeclarations = append(v.sliceDeclarations, &sliceDeclaration{name: vName.Name, pos: s.Pos()})
						}
					}
				} else {
					for i, vName := range vSpec.Names {
						if isCreateEmptyArray(vSpec.Values[i]) {
							v.sliceDeclarations = append(v.sliceDeclarations, &sliceDeclaration{name: vName.Name, pos: s.Pos()})
						}
					}
				}
			}

		case *ast.AssignStmt:
			if len(s.Lhs) != len(s.Rhs) {
				continue
			}
			for index := range s.Lhs {
				ident, ok := s.Lhs[index].(*ast.Ident)
				if !ok {
					continue
				}
				if isCreateEmptyArray(s.Rhs[index]) {
					v.sliceDeclarations = append(v.sliceDeclarations, &sliceDeclaration{name: ident.Name, pos: s.Pos()})
				}
			}

		case *ast.RangeStmt:
			if !v.includeRangeLoops || len(v.sliceDeclarations) == 0 {
				continue
			}
			// Check the value being ranged over and ensure it's not a channel or an iterator function.
			switch inferExprType(s.X).(type) {
			case *ast.ChanType, *ast.FuncType:
				continue
			}
			if s.Body != nil {
				v.handleLoops(s, s.Body)
			}

		case *ast.ForStmt:
			if !v.includeForLoops || len(v.sliceDeclarations) == 0 {
				continue
			}
			if s.Body != nil {
				v.handleLoops(s, s.Body)
			}
		}
	}

	buf := bytes.NewBuffer(nil)

	for _, sliceDecl := range v.sliceDeclarations {
		if !sliceDecl.eligible || sliceDecl.ineligible {
			continue
		}

		buf.Reset()
		buf.WriteString("Consider preallocating ")
		buf.WriteString(sliceDecl.name)

		if sliceDecl.capExpr != nil {
			undo := buf.Len()
			buf.WriteString(" with capacity ")
			if format.Node(buf, token.NewFileSet(), sliceDecl.capExpr) != nil {
				buf.Truncate(undo)
			}
		}

		v.preallocHints = append(v.preallocHints, analysis.Diagnostic{
			Pos:     sliceDecl.pos,
			Message: buf.String(),
		})
	}

	return v
}

func isCreateEmptyArray(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.CompositeLit:
		// []any{}
		_, ok := inferExprType(e.Type).(*ast.ArrayType)
		return ok && len(e.Elts) == 0
	case *ast.CallExpr:
		switch len(e.Args) {
		case 1:
			// []any(nil)
			arg, ok := e.Args[0].(*ast.Ident)
			if !ok || arg.Name != "nil" {
				return false
			}
			_, ok = inferExprType(e.Fun).(*ast.ArrayType)
			return ok
		case 2:
			// make([]any, 0)
			ident, ok := e.Fun.(*ast.Ident)
			if !ok || ident.Name != "make" {
				return false
			}
			arg, ok := e.Args[1].(*ast.BasicLit)
			if !ok || arg.Value != "0" {
				return false
			}
			_, ok = inferExprType(e.Args[0]).(*ast.ArrayType)
			return ok
		}
	}
	return false
}

// handleLoops is a helper function to share the logic required for both *ast.RangeLoops and *ast.ForLoops
func (v *returnsVisitor) handleLoops(loopStmt ast.Stmt, blockStmt *ast.BlockStmt) {
	appendCounters := make(map[string]int, len(v.sliceDeclarations))
	var returnsInsideOfLoop bool

	for _, stmt := range blockStmt.List {
		switch bodyStmt := stmt.(type) {
		case *ast.AssignStmt:
			asgnStmt := bodyStmt
			for index, expr := range asgnStmt.Rhs {
				if index >= len(asgnStmt.Lhs) {
					continue
				}

				lhsIdent, ok := asgnStmt.Lhs[index].(*ast.Ident)
				if !ok {
					continue
				}

				if count, ok := appendCounters[lhsIdent.Name]; ok && count == 0 {
					// already ineligible due to unsupported append pattern
					continue
				}

				callExpr, ok := expr.(*ast.CallExpr)
				if !ok {
					continue
				}

				rhsFuncIdent, ok := callExpr.Fun.(*ast.Ident)
				if !ok || rhsFuncIdent.Name != "append" {
					continue
				}

				// e.g., `x = append(x)`
				// Pointless, but pre-allocation will not help.
				if len(callExpr.Args) < 2 {
					continue
				}

				rhsIdent, ok := callExpr.Args[0].(*ast.Ident)
				if !ok {
					continue
				}

				// e.g., `x = append(y, a)`
				// This is weird (and maybe a logic error),
				// but we cannot recommend pre-allocation.
				if lhsIdent.Name != rhsIdent.Name {
					appendCounters[lhsIdent.Name] = 0
					continue
				}

				// e.g., `x = append(x, y...)`
				// we should ignore this. Pre-allocating in this case
				// is confusing and is not possible in general.
				if callExpr.Ellipsis.IsValid() {
					appendCounters[lhsIdent.Name] = 0
					continue
				}

				appendCounters[lhsIdent.Name] += len(callExpr.Args) - 1
			}
		case *ast.IfStmt:
			ifStmt := bodyStmt
			if ifStmt.Body == nil {
				continue
			}
			for _, ifBodyStmt := range ifStmt.Body.List {
				// TODO: should probably handle embedded ifs here
				switch ifBodyStmt.(type) {
				case *ast.BranchStmt, *ast.ReturnStmt:
					returnsInsideOfLoop = true
				}
			}
		}
	}

	if len(appendCounters) == 0 {
		return
	}

	var capExpr ast.Expr
	switch s := loopStmt.(type) {
	case *ast.RangeStmt:
		capExpr = rangeLoopCount(s)
	case *ast.ForStmt:
		capExpr = forLoopCount(s)
	}

	for name, appendCount := range appendCounters {
		for _, sliceDecl := range v.sliceDeclarations {
			if sliceDecl.name != name {
				continue
			}

			if sliceDecl.ineligible {
				break
			}

			if v.simple && returnsInsideOfLoop {
				// ineligible due to return/break whilst in simple mode
				sliceDecl.ineligible = true
				break
			}

			if appendCount == 0 {
				// ineligible due to unsupported append pattern
				sliceDecl.ineligible = true
				break
			}

			sliceDecl.eligible = true

			if capExpr != nil {
				capExpr := capExpr
				if appendCount > 1 {
					if capInt, ok := exprIntValue(capExpr); ok {
						capExpr = &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(appendCount * capInt)}
					} else {
						capExpr = &ast.BinaryExpr{
							X:  &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(appendCount)},
							Op: token.MUL,
							Y:  capExpr,
						}
					}
				}
				if sliceDecl.capExpr != nil {
					capExpr = &ast.BinaryExpr{X: sliceDecl.capExpr, Op: token.ADD, Y: capExpr}
				}
				sliceDecl.capExpr = capExpr
			}
		}
	}
}

func rangeLoopCount(stmt *ast.RangeStmt) ast.Expr {
	switch xType := inferExprType(stmt.X).(type) {
	case *ast.ArrayType, *ast.MapType:
	case *ast.StarExpr:
		if _, ok := xType.X.(*ast.ArrayType); !ok {
			return nil
		}
	case *ast.Ident:
		switch xType.Name {
		case "byte", "rune", "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
			return stmt.X
		case "string":
			if lit, ok := stmt.X.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if str, err := strconv.Unquote(lit.Value); err == nil {
					return &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(len(str))}
				}
			}
		default:
			return nil
		}
	default:
		return nil
	}

	return &ast.CallExpr{Fun: &ast.Ident{Name: "len"}, Args: []ast.Expr{stmt.X}}
}

func forLoopCount(stmt *ast.ForStmt) ast.Expr {
	initStmt, ok := stmt.Init.(*ast.AssignStmt)
	if !ok {
		return nil
	}

	condExpr, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok {
		return nil
	}

	postStmt, ok := stmt.Post.(*ast.IncDecStmt)
	if !ok {
		return nil
	}

	postIdent, ok := postStmt.X.(*ast.Ident)
	if !ok {
		return nil
	}

	index := -1
	for i := range initStmt.Lhs {
		if ident, ok := initStmt.Lhs[i].(*ast.Ident); ok && ident.Name == postIdent.Name {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}

	lower := initStmt.Rhs[index]
	var upper ast.Expr
	op := condExpr.Op
	if x, ok := condExpr.X.(*ast.Ident); ok && x.Name == postIdent.Name {
		upper = condExpr.Y
	} else if y, ok := condExpr.Y.(*ast.Ident); ok && y.Name == postIdent.Name {
		// reverse the inequality
		upper = condExpr.X
		switch op {
		case token.LSS:
			op = token.GTR
		case token.GTR:
			op = token.LSS
		case token.LEQ:
			op = token.GEQ
		case token.GEQ:
			op = token.LEQ
		default:
		}
	} else {
		return nil
	}

	if postStmt.Tok == token.INC {
		if op == token.GTR || op == token.GEQ {
			return nil
		}
	} else {
		if op == token.LSS || op == token.LEQ {
			return nil
		}
		lower, upper = upper, lower
	}

	plusOne := op == token.LEQ || op == token.GEQ

	if upperInt, ok := exprIntValue(upper); ok {
		if plusOne {
			upperInt++
		}
		if lowerInt, ok := exprIntValue(lower); ok {
			if count := upperInt - lowerInt; count > 0 {
				return &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(count)}
			}
			return nil
		}
		if upperInt == 0 {
			return &ast.UnaryExpr{Op: token.SUB, X: lower}
		}
		return &ast.BinaryExpr{
			X:  &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(upperInt)},
			Op: token.SUB,
			Y:  lower,
		}
	} else if lowerInt, ok := exprIntValue(lower); ok {
		if plusOne {
			lowerInt--
		}
		if lowerInt == 0 {
			return upper
		}
		if lowerInt < 0 {
			return &ast.BinaryExpr{
				X:  upper,
				Op: token.ADD,
				Y:  &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(-lowerInt)},
			}
		}
		return &ast.BinaryExpr{
			X:  upper,
			Op: token.SUB,
			Y:  &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(lowerInt)},
		}
	}
	subExpr := &ast.BinaryExpr{X: upper, Op: token.SUB, Y: lower}
	if plusOne {
		return &ast.BinaryExpr{
			X:  subExpr,
			Op: token.ADD,
			Y:  &ast.BasicLit{Kind: token.INT, Value: "1"},
		}
	}
	return subExpr
}

func exprIntValue(expr ast.Expr) (int, bool) {
	var negate bool
	if unary, ok := expr.(*ast.UnaryExpr); ok {
		switch unary.Op {
		case token.ADD:
			expr = unary.X
		case token.SUB:
			expr = unary.X
			negate = true
		default:
			return 0, false
		}
	}
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.INT {
		if i, err := strconv.Atoi(lit.Value); err == nil {
			if negate {
				return -i, true
			}
			return i, true
		}
	}
	return 0, false
}
