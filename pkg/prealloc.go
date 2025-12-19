package pkg

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/token"
	"slices"
	"strconv"

	"golang.org/x/tools/go/analysis"
)

type sliceDeclaration struct {
	name      string
	pos       token.Pos
	level     int      // Nesting level of this slice. Will be disqualified if appended at a deeper level.
	lenExpr   ast.Expr // Initial length of this slice.
	exclude   bool     // Whether this slice has been disqualified due to an unsupported pattern.
	hasReturn bool     // Whether a return statement has been found after the first append. Any subsequent appends will disqualify this slice in simple mode.
}

type sliceAppend struct {
	index     int      // Index of the target slice.
	countExpr ast.Expr // Number of items appended.
}

type returnsVisitor struct {
	// flags
	simple            bool
	includeRangeLoops bool
	includeForLoops   bool
	// visitor fields
	sliceDeclarations []*sliceDeclaration
	sliceAppends      []*sliceAppend
	preallocHints     []analysis.Diagnostic
	level             int  // Current nesting level. Loops do not increment the level.
	hasReturn         bool // Whether a return statement has been found. Slices appended before and after a return are disqualified in simple mode.
	hasGoto           bool // Whether a goto statement has been found. Goto disqualifies pending and subsequent slices in simple mode.
	hasBranch         bool // Whether a branch statement has been found. Loops with branch statements are unsupported in simple mode.
}

func Check(files []*ast.File, simple, includeRangeLoops, includeForLoops bool) []analysis.Diagnostic {
	retVis := &returnsVisitor{
		simple:            simple,
		includeRangeLoops: includeRangeLoops,
		includeForLoops:   includeForLoops,
	}
	for _, f := range files {
		ast.Walk(retVis, f)
	}
	return retVis.preallocHints
}

func (v *returnsVisitor) Visit(node ast.Node) ast.Visitor {
	switch s := node.(type) {
	case *ast.FuncDecl:
		if s.Body == nil {
			return nil
		}
		v.level = 0
		v.hasReturn = false
		v.hasGoto = false
		ast.Walk(v, s.Body)
		return nil

	case *ast.FuncLit:
		if s.Body == nil {
			return nil
		}
		wasReturn := v.hasReturn
		wasGoto := v.hasGoto
		v.hasReturn = false
		ast.Walk(v, s.Body)
		v.hasReturn = wasReturn
		v.hasGoto = wasGoto
		return nil

	case *ast.BlockStmt:
		declIdx := len(v.sliceDeclarations)
		appendIdx := len(v.sliceAppends)
		v.level++
		for _, stmt := range s.List {
			ast.Walk(v, stmt)
		}
		v.level--

		buf := bytes.NewBuffer(nil)
		for i := declIdx; i < len(v.sliceDeclarations); i++ {
			sliceDecl := v.sliceDeclarations[i]
			if sliceDecl.exclude || v.hasGoto {
				continue
			}

			capExpr := sliceDecl.lenExpr
			for j := appendIdx; j < len(v.sliceAppends); j++ {
				if v.sliceAppends[j] != nil && v.sliceAppends[j].index == i {
					capExpr = addIntExpr(capExpr, v.sliceAppends[j].countExpr)
				}
			}
			if capExpr == sliceDecl.lenExpr {
				// nothing appended
				continue
			}
			if capVal, ok := intValue(capExpr); ok && capVal <= 0 {
				continue
			}

			buf.Reset()
			buf.WriteString("Consider preallocating ")
			buf.WriteString(sliceDecl.name)
			if capExpr != nil {
				undo := buf.Len()
				buf.WriteString(" with capacity ")
				if format.Node(buf, token.NewFileSet(), capExpr) != nil {
					buf.Truncate(undo)
				}
			}
			v.preallocHints = append(v.preallocHints, analysis.Diagnostic{
				Pos:     sliceDecl.pos,
				Message: buf.String(),
			})
		}

		// discard slices and associated appends that are falling out of scope
		v.sliceDeclarations = v.sliceDeclarations[:declIdx]
		for i := appendIdx; i < len(v.sliceAppends); i++ {
			if v.sliceAppends[i] != nil {
				if v.sliceAppends[i].index >= declIdx {
					v.sliceAppends[i] = nil
				} else {
					appendIdx = i + 1
				}
			}
		}
		v.sliceAppends = v.sliceAppends[:appendIdx]
		return nil

	case *ast.ValueSpec:
		_, isArrayType := inferExprType(s.Type).(*ast.ArrayType)
		for i, name := range s.Names {
			var lenExpr ast.Expr
			if i >= len(s.Values) {
				if !isArrayType {
					continue
				}
				lenExpr = intExpr(0)
			} else if lenExpr = isCreateArray(s.Values[i]); lenExpr == nil {
				if id, ok := s.Values[i].(*ast.Ident); !ok || id.Name != "nil" {
					continue
				}
				lenExpr = intExpr(0)
			}
			v.sliceDeclarations = append(v.sliceDeclarations, &sliceDeclaration{name: name.Name, pos: s.Pos(), level: v.level, lenExpr: lenExpr})
		}

	case *ast.AssignStmt:
		if len(s.Lhs) != len(s.Rhs) {
			return nil
		}
		for i, lhs := range s.Lhs {
			ident, ok := lhs.(*ast.Ident)
			if !ok {
				continue
			}
			if lenExpr := isCreateArray(s.Rhs[i]); lenExpr != nil {
				v.sliceDeclarations = append(v.sliceDeclarations, &sliceDeclaration{name: ident.Name, pos: s.Pos(), level: v.level, lenExpr: lenExpr})
			} else {
				switch expr := s.Rhs[i].(type) {
				case *ast.Ident:
					// create a new slice declaration when reinitializing an existing slice to nil
					if s.Tok != token.ASSIGN || expr.Name != "nil" {
						continue
					}
					if slices.ContainsFunc(v.sliceDeclarations, func(sliceDecl *sliceDeclaration) bool { return sliceDecl.name == ident.Name }) {
						v.sliceDeclarations = append(v.sliceDeclarations, &sliceDeclaration{name: ident.Name, pos: s.Pos(), level: v.level, lenExpr: intExpr(0)})
					}
				case *ast.CallExpr:
					if len(expr.Args) < 2 {
						continue
					}
					if funIdent, ok := expr.Fun.(*ast.Ident); !ok || funIdent.Name != "append" {
						continue
					}
					rhsIdent, ok := expr.Args[0].(*ast.Ident)
					if !ok {
						continue
					}
					for i := len(v.sliceDeclarations) - 1; i >= 0; i-- {
						sliceDecl := v.sliceDeclarations[i]
						if sliceDecl.name == ident.Name {
							if expr.Ellipsis.IsValid() || ident.Name != rhsIdent.Name || sliceDecl.hasReturn || sliceDecl.level != v.level {
								sliceDecl.exclude = true
							} else {
								v.sliceAppends = append(v.sliceAppends, &sliceAppend{index: i, countExpr: intExpr(len(expr.Args) - 1)})
							}
							break
						}
					}
				}
			}
		}

	case *ast.RangeStmt:
		return v.walkLoop(v.includeRangeLoops, s.Body, func() (ast.Expr, bool) { return rangeLoopCount(s) })

	case *ast.ForStmt:
		return v.walkLoop(v.includeForLoops, s.Body, func() (ast.Expr, bool) { return forLoopCount(s) })

	case *ast.SwitchStmt:
		return v.walkSwitchSelect(s.Body)

	case *ast.TypeSwitchStmt:
		return v.walkSwitchSelect(s.Body)

	case *ast.SelectStmt:
		return v.walkSwitchSelect(s.Body)

	case *ast.ReturnStmt:
		if !v.simple {
			return nil
		}
		v.hasReturn = true
		// flag all slices that have been appended at least once
		for _, sliceApp := range v.sliceAppends {
			if sliceApp != nil {
				v.sliceDeclarations[sliceApp.index].hasReturn = true
			}
		}

	case *ast.BranchStmt:
		if !v.simple {
			return nil
		}
		if s.Label != nil {
			v.hasGoto = true
		} else {
			v.hasBranch = true
		}
	}

	return v
}

func (v *returnsVisitor) walkLoop(include bool, body *ast.BlockStmt, loopCount func() (ast.Expr, bool)) ast.Visitor {
	if len(v.sliceDeclarations) == 0 {
		return v
	}
	if body == nil {
		return nil
	}

	appendIdx := len(v.sliceAppends)
	hadBranch := v.hasBranch
	v.hasBranch = false
	v.level--
	ast.Walk(v, body)
	v.level++

	exclude := !include || v.hasReturn || v.hasGoto || v.hasBranch
	var loopCountExpr ast.Expr
	if !exclude {
		var ok bool
		loopCountExpr, ok = loopCount()
		exclude = !ok
	}
	if exclude {
		// exclude all slices that were appended within this loop
		for i := appendIdx; i < len(v.sliceAppends); i++ {
			if v.sliceAppends[i] != nil {
				v.sliceDeclarations[v.sliceAppends[i].index].exclude = true
			}
		}
	} else {
		for i := range v.sliceDeclarations {
			prev := -1
			for j := len(v.sliceAppends) - 1; j >= appendIdx; j-- {
				if v.sliceAppends[j] != nil && v.sliceAppends[j].index == i {
					if prev >= 0 {
						// consolidate appends to the same slice
						v.sliceAppends[j].countExpr = addIntExpr(v.sliceAppends[j].countExpr, v.sliceAppends[prev].countExpr)
						v.sliceAppends[prev] = nil
					} else if loopCountExpr == nil {
						// make appends indeterminate if the loop count is indeterminate
						v.sliceAppends[j].countExpr = nil
					}
					prev = j
				}
			}
			if prev >= 0 {
				v.sliceAppends[prev].countExpr = mulIntExpr(v.sliceAppends[prev].countExpr, loopCountExpr)
			}
		}
	}
	v.hasBranch = hadBranch
	return nil
}

func (v *returnsVisitor) walkSwitchSelect(body *ast.BlockStmt) ast.Visitor {
	hadBranch := v.hasBranch
	v.hasBranch = false
	ast.Walk(v, body)
	v.hasBranch = hadBranch
	return nil
}

func isCreateArray(expr ast.Expr) ast.Expr {
	switch e := expr.(type) {
	case *ast.CompositeLit:
		// []any{...}
		if _, ok := inferExprType(e.Type).(*ast.ArrayType); ok {
			return intExpr(len(e.Elts))
		}
	case *ast.CallExpr:
		switch len(e.Args) {
		case 1:
			// []any(nil)
			arg, ok := e.Args[0].(*ast.Ident)
			if !ok || arg.Name != "nil" {
				return nil
			}
			if _, ok = inferExprType(e.Fun).(*ast.ArrayType); ok {
				return intExpr(0)
			}
		case 2:
			// make([]any, n)
			ident, ok := e.Fun.(*ast.Ident)
			if ok && ident.Name == "make" {
				return e.Args[1]
			}
		}
	}
	return nil
}

func rangeLoopCount(stmt *ast.RangeStmt) (ast.Expr, bool) {
	switch xType := inferExprType(stmt.X).(type) {
	case *ast.ChanType, *ast.FuncType:
		return nil, false
	case *ast.ArrayType, *ast.MapType:
	case *ast.StarExpr:
		if _, ok := xType.X.(*ast.ArrayType); !ok {
			return nil, true
		}
	case *ast.Ident:
		switch xType.Name {
		case "byte", "rune", "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
			return stmt.X, true
		case "string":
			if lit, ok := stmt.X.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if str, err := strconv.Unquote(lit.Value); err == nil {
					return intExpr(len(str)), true
				}
			}
		default:
			return nil, true
		}
	default:
		return nil, true
	}

	return &ast.CallExpr{Fun: ast.NewIdent("len"), Args: []ast.Expr{stmt.X}}, true
}

func forLoopCount(stmt *ast.ForStmt) (ast.Expr, bool) {
	if stmt.Init == nil || stmt.Cond == nil || stmt.Post == nil {
		return nil, false
	}

	initStmt, ok := stmt.Init.(*ast.AssignStmt)
	if !ok {
		return nil, true
	}

	condExpr, ok := stmt.Cond.(*ast.BinaryExpr)
	if !ok {
		return nil, true
	}

	postStmt, ok := stmt.Post.(*ast.IncDecStmt)
	if !ok {
		return nil, true
	}

	postIdent, ok := postStmt.X.(*ast.Ident)
	if !ok {
		return nil, true
	}

	index := -1
	for i := range initStmt.Lhs {
		if ident, ok := initStmt.Lhs[i].(*ast.Ident); ok && ident.Name == postIdent.Name {
			index = i
			break
		}
	}
	if index < 0 {
		return nil, true
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
		return nil, true
	}

	if postStmt.Tok == token.INC {
		if op == token.GTR || op == token.GEQ {
			return nil, false
		}
	} else {
		if op == token.LSS || op == token.LEQ {
			return nil, false
		}
		lower, upper = upper, lower
	}

	// negate the lower bound before adding
	if unary, ok := lower.(*ast.UnaryExpr); ok && unary.Op == token.SUB {
		lower = unary.X
	} else {
		lower = &ast.UnaryExpr{Op: token.SUB, X: lower}
	}

	countExpr := addIntExpr(upper, lower)
	if op == token.LEQ || op == token.GEQ {
		countExpr = addIntExpr(countExpr, intExpr(1))
	}
	return countExpr, true
}
