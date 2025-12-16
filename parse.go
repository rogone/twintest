package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

// Branch represents a control-flow branch (if, for, switch case, return, etc.)
type Branch struct {
	Line      int
	CodeLine  string
	Children  []*Branch
	hasReturn bool // internal memo: true if this or any descendant is a return path
}

// HasReturn returns true if this branch or any of its descendants leads to a return statement.
// It caches the result by setting hasReturn = true when a return is found downstream.
func (b *Branch) HasReturn() bool {
	if b.hasReturn {
		return true
	}
	for _, child := range b.Children {
		if child.HasReturn() {
			b.hasReturn = true // promote upward
			return true
		}
	}
	return false
}

type FuncInfo struct {
	//IsMethod   bool
	Receiver   string
	Name       string
	IsExported bool
	Branches   []*Branch
}

type StructInfo struct {
	Name       string
	IsExported bool
	Methods    []FuncInfo
}

func ParseFile(filename string) ([]*StructInfo, string, error) {
	src, err := os.ReadFile(filename)
	if err != nil {
		return nil, "", err
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, "", err
	}
	//src := bytes.Split(srcBytes, []byte("\n"))

	structs := make([]*StructInfo, 0)
	structTypes := make(map[string]*StructInfo)
	for _, decl := range node.Decls {
		if genDecl, ok := decl.(*ast.GenDecl); ok {
			for _, spec := range genDecl.Specs {
				if typeSpec, ok := spec.(*ast.TypeSpec); ok {
					if _, ok := typeSpec.Type.(*ast.StructType); ok {
						info := &StructInfo{
							Name:       typeSpec.Name.Name,
							IsExported: ast.IsExported(typeSpec.Name.Name),
						}
						structTypes[typeSpec.Name.Name] = info
						structs = append(structs, info)
					}
				}
			}
		}
	}

	if _, ok := structTypes[""]; !ok {
		structTypes[""] = &StructInfo{}
	}

	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			receiverType := GetReceiverType(fn)
			si := structTypes[receiverType]

			branches := ExtractBranches(fn.Body, fset, src)

			info := FuncInfo{
				Name: fn.Name.Name,
				//IsMethod:   fn.Recv != nil,
				Receiver:   receiverType,
				Branches:   branches,
				IsExported: ast.IsExported(fn.Name.Name),
			}

			si.Methods = append(si.Methods, info)
		}
	}
	return structs, node.Name.Name, nil
}

func GetReceiverType(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	recv := fn.Recv.List[0].Type
	switch r := recv.(type) {
	case *ast.Ident:
		return r.Name
	case *ast.StarExpr:
		if id, ok := r.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

func ExtractBranches(block *ast.BlockStmt, fset *token.FileSet, src []byte) []*Branch {
	var children []*Branch
	for _, stmt := range block.List {
		visitStmt(stmt, fset, src, &children)
	}
	return children
}

func visitStmt(stmt ast.Stmt, fset *token.FileSet, src []byte, out *[]*Branch) {
	// Note: *ast.FuncLit is not a Stmt, so no need to check for it here

	var b *Branch
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		b = parseReturnStmt(s, fset, src)
	case *ast.IfStmt:
		b = parseIfStmt(s, fset, src)
	case *ast.ForStmt:
		b = parseForStmt(s, fset, src)
	case *ast.RangeStmt:
		b = parseRangeStmt(s, fset, src)
	case *ast.SwitchStmt:
		b = parseSwitchStmt(s, fset, src)
	case *ast.TypeSwitchStmt:
		b = parseTypeSwitchStmt(s, fset, src)
	case *ast.SelectStmt:
		b = parseSelectStmt(s, fset, src)
	case *ast.BlockStmt:
		b = parseBlockStmt(s, fset, src)
	default:
		// Ignore non-control-flow statements (assignments, exprs, etc.)
		return
	}

	if b != nil {
		*out = append(*out, b)
	}
}

func parseReturnStmt(s *ast.ReturnStmt, fset *token.FileSet, src []byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := nodeToCode(s, fset, src)
	return &Branch{
		Line:      lineNo,
		CodeLine:  code,
		Children:  nil,
		hasReturn: true,
	}
}

func parseIfStmt(s *ast.IfStmt, fset *token.FileSet, src []byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := nodeToCode(s, fset, src)
	bodyStart := strings.Index(code, "{")
	if bodyStart > 0 {
		code = strings.TrimSpace(code[:bodyStart])
	}
	b := &Branch{
		Line:     lineNo,
		CodeLine: code,
		Children: []*Branch{ // if
			{
				Line:      lineNo,
				CodeLine:  code,
				Children:  ExtractBranches(s.Body, fset, src),
				hasReturn: false,
			},
		},
	}

	if s.Else != nil {
		if elseIf, ok := s.Else.(*ast.IfStmt); ok {
			curr := elseIf
			for curr != nil {
				b.Children = append(b.Children, &Branch{
					Line:      fset.Position(curr.Pos()).Line,
					CodeLine:  nodeToCode(curr.Cond, fset, src),
					Children:  ExtractBranches(curr.Body, fset, src),
					hasReturn: false,
				})

				if curr.Else != nil {
					if next, ok := curr.Else.(*ast.IfStmt); ok {
						curr = next
						continue
					} else {
						b.Children = append(b.Children, &Branch{
							Line:      fset.Position(curr.Else.Pos()).Line,
							CodeLine:  "else",
							Children:  ExtractBranches(curr.Else.(*ast.BlockStmt), fset, src),
							hasReturn: false,
						})
						break
					}
				}
				break
			}
		} else {
			b.Children = append(b.Children, &Branch{
				Line:      fset.Position(s.Else.Pos()).Line,
				CodeLine:  "else",
				Children:  ExtractBranches(s.Else.(*ast.BlockStmt), fset, src),
				hasReturn: false,
			})
		}
	}

	if len(b.Children) == 1 {
		return b.Children[0]
	}

	return b
}

func parseForStmt(s *ast.ForStmt, fset *token.FileSet, src []byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := nodeToCode(s, fset, src)
	bodyStart := strings.Index(code, "{")
	if bodyStart > 0 {
		code = strings.TrimSpace(code[:bodyStart])
	}

	return &Branch{
		Line:      lineNo,
		CodeLine:  code,
		Children:  ExtractBranches(s.Body, fset, src),
		hasReturn: false,
	}
}

func parseRangeStmt(s *ast.RangeStmt, fset *token.FileSet, src []byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := nodeToCode(s, fset, src)
	bodyStart := strings.Index(code, "{")
	if bodyStart > 0 {
		code = strings.TrimSpace(code[:bodyStart])
	}

	return &Branch{
		Line:      lineNo,
		CodeLine:  code,
		Children:  ExtractBranches(s.Body, fset, src),
		hasReturn: false,
	}
}

func parseSwitchStmt(s *ast.SwitchStmt, fset *token.FileSet, src []byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := nodeToCode(s, fset, src)
	bodyStart := strings.Index(code, "{")
	if bodyStart > 0 {
		code = strings.TrimSpace(code[:bodyStart])
	}

	b := &Branch{
		Line:      lineNo,
		CodeLine:  code,
		Children:  nil,
		hasReturn: false,
	}

	for _, cc := range s.Body.List {
		if cs, ok := cc.(*ast.CaseClause); ok {
			caseLine := fset.Position(cs.Pos()).Line
			caseCode := ""
			if caseLine > 0 && caseLine <= len(src) {
				caseCode = strings.TrimSpace(string(src[caseLine-1]))
			}
			caseChildren := extractFromStmtList(cs.Body, fset, src)
			b.Children = append(b.Children, &Branch{
				Line:      caseLine,
				CodeLine:  caseCode,
				Children:  caseChildren,
				hasReturn: false,
			})
		}
	}

	return b
}

func parseTypeSwitchStmt(s *ast.TypeSwitchStmt, fset *token.FileSet, src []byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := nodeToCode(s, fset, src)
	bodyStart := strings.Index(code, "{")
	if bodyStart > 0 {
		code = strings.TrimSpace(code[:bodyStart])
	}

	b := &Branch{
		Line:      lineNo,
		CodeLine:  code,
		Children:  nil,
		hasReturn: false,
	}

	for _, cc := range s.Body.List {
		if cs, ok := cc.(*ast.CaseClause); ok {
			caseLine := fset.Position(cs.Pos()).Line
			caseCode := ""
			if caseLine > 0 && caseLine <= len(src) {
				caseCode = strings.TrimSpace(string(src[caseLine-1]))
			}
			caseChildren := extractFromStmtList(cs.Body, fset, src)
			b.Children = append(b.Children, &Branch{
				Line:      caseLine,
				CodeLine:  caseCode,
				Children:  caseChildren,
				hasReturn: false,
			})
		}
	}

	return b
}

func parseSelectStmt(s *ast.SelectStmt, fset *token.FileSet, src []byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := nodeToCode(s, fset, src)
	bodyStart := strings.Index(code, "{")
	if bodyStart > 0 {
		code = strings.TrimSpace(code[:bodyStart])
	}

	b := &Branch{
		Line:      lineNo,
		CodeLine:  code,
		Children:  nil,
		hasReturn: false,
	}

	for _, cc := range s.Body.List {
		if cs, ok := cc.(*ast.CommClause); ok {
			commLine := fset.Position(cs.Pos()).Line
			commCode := ""
			if commLine > 0 && commLine <= len(src) {
				commCode = strings.TrimSpace(string(src[commLine-1]))
			}
			commChildren := extractFromStmtList(cs.Body, fset, src)
			b.Children = append(b.Children, &Branch{
				Line:      commLine,
				CodeLine:  commCode,
				Children:  commChildren,
				hasReturn: false,
			})
		}
	}

	return b
}

func parseBlockStmt(s *ast.BlockStmt, fset *token.FileSet, src []byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := "<block>"
	return &Branch{
		Line:      lineNo,
		CodeLine:  code,
		Children:  ExtractBranches(s, fset, src),
		hasReturn: false,
	}
}

func extractFromStmtList(stmts []ast.Stmt, fset *token.FileSet, src []byte) []*Branch {
	var children []*Branch
	for _, stmt := range stmts {
		visitStmt(stmt, fset, src, &children)
	}
	return children
}

func FilterReturnPaths(branches []*Branch) []*Branch {
	var result []*Branch
	for _, b := range branches {
		if pruneBranch(b) {
			result = append(result, b)
		}
	}
	return result
}

func pruneBranch(b *Branch) bool {
	if b.HasReturn() {
		var kept []*Branch
		for _, child := range b.Children {
			if pruneBranch(child) {
				kept = append(kept, child)
			}
		}
		b.Children = kept
		return true
	}
	return false
}

func nodeToCode(n ast.Node, fset *token.FileSet, src []byte) string {
	start := fset.Position(n.Pos()).Offset
	end := fset.Position(n.End()).Offset
	if start >= 0 && end <= len(src) && start < end {
		return strings.TrimSpace(string(src[start:end]))
	}
	return "<invalid>"
}
