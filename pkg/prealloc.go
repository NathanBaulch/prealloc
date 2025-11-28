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

var invalid = &ast.BadExpr{}

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
						if i >= len(vSpec.Values) {
							break
						}
						if lenExpr, ok := isCreateArray(vSpec.Values[i]); ok {
							v.sliceDeclarations = append(v.sliceDeclarations, &sliceDeclaration{name: vName.Name, pos: s.Pos(), capExpr: lenExpr})
						}
					}
				}
			}

		case *ast.AssignStmt:
			for i, lhs := range s.Lhs {
				if i >= len(s.Rhs) {
					break
				}
				ident, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}
				if lenExpr, ok := isCreateArray(s.Rhs[i]); ok {
					v.sliceDeclarations = append(v.sliceDeclarations, &sliceDeclaration{name: ident.Name, pos: s.Pos(), capExpr: lenExpr})
				}
			}

		case *ast.RangeStmt:
			if !v.includeRangeLoops || len(v.sliceDeclarations) == 0 {
				continue
			}
			if s.Body != nil {
				v.handleLoops(s, s.Body)
			}

		case *ast.ForStmt:
			if !v.includeForLoops || len(v.sliceDeclarations) == 0 {
				continue
			}
			if s.Init == nil || s.Cond == nil || s.Post == nil {
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

		if sliceDecl.capExpr != nil && sliceDecl.capExpr != invalid {
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

func isCreateArray(expr ast.Expr) (ast.Expr, bool) {
	switch e := expr.(type) {
	case *ast.CompositeLit:
		// []any{...}
		_, ok := inferExprType(e.Type).(*ast.ArrayType)
		if ok && len(e.Elts) > 0 {
			return &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(len(e.Elts))}, true
		}
		return nil, ok
	case *ast.CallExpr:
		switch len(e.Args) {
		case 1:
			// []any(nil)
			arg, ok := e.Args[0].(*ast.Ident)
			if !ok || arg.Name != "nil" {
				return nil, false
			}
			_, ok = inferExprType(e.Fun).(*ast.ArrayType)
			return nil, ok
		case 2:
			// make([]any, n)
			ident, ok := e.Fun.(*ast.Ident)
			if !ok || ident.Name != "make" {
				return nil, false
			}
			return e.Args[1], true
		}
	}
	return nil, false
}

// handleLoops is a helper function to share the logic required for both *ast.RangeLoops and *ast.ForLoops
func (v *returnsVisitor) handleLoops(loopStmt ast.Stmt, blockStmt *ast.BlockStmt) {
	appendCounters := make(map[string]int, len(v.sliceDeclarations))
	var hasReturnOrBranch bool

	for _, stmt := range blockStmt.List {
		switch bodyStmt := stmt.(type) {
		case *ast.AssignStmt:
			asgnStmt := bodyStmt
			for i, lhs := range asgnStmt.Lhs {
				if i >= len(asgnStmt.Rhs) {
					break
				}

				lhsIdent, ok := lhs.(*ast.Ident)
				if !ok {
					continue
				}

				if count, ok := appendCounters[lhsIdent.Name]; ok && count == 0 {
					// already ineligible due to unsupported append pattern
					continue
				}

				callExpr, ok := asgnStmt.Rhs[i].(*ast.CallExpr)
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
					hasReturnOrBranch = true
				}
			}
		}
	}

	if len(appendCounters) == 0 {
		return
	}

	var countExpr ast.Expr
	switch s := loopStmt.(type) {
	case *ast.RangeStmt:
		countExpr = rangeLoopCount(s)
	case *ast.ForStmt:
		countExpr = forLoopCount(s)
	}

	if count, ok := exprIntValue(countExpr); ok {
		if count <= 0 {
			// loop will definitely never iterate (probably a logic error)
			return
		}
		if _, ok := countExpr.(*ast.BasicLit); !ok {
			countExpr = &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(count)}
		}
	}

	for name, appendCount := range appendCounters {
		for _, sliceDecl := range v.sliceDeclarations {
			if sliceDecl.name != name {
				continue
			}

			if sliceDecl.ineligible {
				break
			}

			if countExpr == invalid {
				// ineligible due to indeterminate loop count
				sliceDecl.ineligible = true
				break
			}

			if v.simple && hasReturnOrBranch {
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

			if countExpr == nil {
				sliceDecl.capExpr = invalid
				break
			}

			capExpr := countExpr
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
			sliceDecl.capExpr = exprIntAdd(sliceDecl.capExpr, capExpr)
		}
	}
}

func rangeLoopCount(stmt *ast.RangeStmt) ast.Expr {
	switch xType := inferExprType(stmt.X).(type) {
	case *ast.ChanType, *ast.FuncType:
		return invalid
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

	return &ast.CallExpr{Fun: ast.NewIdent("len"), Args: []ast.Expr{stmt.X}}
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
	for i, lhs := range initStmt.Lhs {
		if i >= len(initStmt.Rhs) {
			break
		}
		if ident, ok := lhs.(*ast.Ident); ok && ident.Name == postIdent.Name {
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
			return invalid
		}
	} else {
		if op == token.LSS || op == token.LEQ {
			return invalid
		}
		lower, upper = upper, lower
	}

	// negate the lower bound before adding
	if unary, ok := lower.(*ast.UnaryExpr); ok && unary.Op == token.SUB {
		lower = unary.X
	} else {
		lower = &ast.UnaryExpr{Op: token.SUB, X: lower}
	}

	countExpr := exprIntAdd(upper, lower)
	if op == token.LEQ || op == token.GEQ {
		countExpr = exprIntAdd(countExpr, &ast.BasicLit{Kind: token.INT, Value: "1"})
	}
	return countExpr
}

func exprIntAdd(x, y ast.Expr) ast.Expr {
	if x == nil {
		return y
	}
	if y == nil {
		return x
	}

	xInt, xOK := exprIntValue(x)
	yInt, yOK := exprIntValue(y)

	if xOK && yOK {
		return &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(xInt + yInt)}
	}
	if xOK {
		if xInt == 0 {
			return y
		}
		if xInt < 0 {
			return &ast.BinaryExpr{X: y, Op: token.SUB, Y: &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(-xInt)}}
		}
	}
	if yOK {
		if yInt == 0 {
			return x
		}
		if yInt < 0 {
			return &ast.BinaryExpr{X: x, Op: token.SUB, Y: &ast.BasicLit{Kind: token.INT, Value: strconv.Itoa(-yInt)}}
		}
	}
	if unary, ok := y.(*ast.UnaryExpr); ok && unary.Op == token.SUB {
		return &ast.BinaryExpr{X: x, Op: token.SUB, Y: unary.X}
	}
	return &ast.BinaryExpr{X: x, Op: token.ADD, Y: y}
}

func exprIntValue(expr ast.Expr) (int, bool) {
	var negate bool
	for {
		switch e := expr.(type) {
		case *ast.UnaryExpr:
			if e.Op == token.SUB {
				negate = !negate
				expr = e.X
				continue
			}
		case *ast.CallExpr:
			if ident, ok := e.Fun.(*ast.Ident); ok && len(e.Args) == 1 {
				switch ident.Name {
				case "byte", "rune", "int", "int8", "int16", "int32", "int64",
					"uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
					expr = e.Args[0]
					continue
				}
			}
		}
		break
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
