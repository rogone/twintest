package main

import (
	"bytes"
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
	srcBytes, err := os.ReadFile(filename)
	if err != nil {
		return nil, "", err
	}
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "", srcBytes, parser.ParseComments)
	if err != nil {
		return nil, "", err
	}
	lines := bytes.Split(srcBytes, []byte("\n"))

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

	if _, ok := structTypes[""]; ok {
		structTypes[""] = &StructInfo{}
	}

	var FuncInfos []FuncInfo
	for _, decl := range node.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			receiverType := GetReceiverType(fn)
			si := structTypes[receiverType]

			branches := ExtractBranches(fn.Body, fset, lines)

			FuncInfos = append(FuncInfos, FuncInfo{
				Name: fn.Name.Name,
				//IsMethod:   fn.Recv != nil,
				Receiver:   receiverType,
				Branches:   branches,
				IsExported: ast.IsExported(fn.Name.Name),
			})

			si.Methods = append(si.Methods, FuncInfo{})
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

func ExtractBranches(block *ast.BlockStmt, fset *token.FileSet, lines [][]byte) []*Branch {
	var children []*Branch
	for _, stmt := range block.List {
		visitStmt(stmt, fset, lines, &children)
	}
	return children
}

func visitStmt(stmt ast.Stmt, fset *token.FileSet, lines [][]byte, out *[]*Branch) {
	// Note: *ast.FuncLit is not a Stmt, so no need to check for it here

	var b *Branch
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		b = parseReturnStmt(s, fset, lines)
	case *ast.IfStmt:
		b = parseIfStmt(s, fset, lines)
	case *ast.ForStmt:
		b = parseForStmt(s, fset, lines)
	case *ast.RangeStmt:
		b = parseRangeStmt(s, fset, lines)
	case *ast.SwitchStmt:
		b = parseSwitchStmt(s, fset, lines)
	case *ast.TypeSwitchStmt:
		b = parseTypeSwitchStmt(s, fset, lines)
	case *ast.SelectStmt:
		b = parseSelectStmt(s, fset, lines)
	case *ast.BlockStmt:
		b = parseBlockStmt(s, fset, lines)
	default:
		// Ignore non-control-flow statements (assignments, exprs, etc.)
		return
	}

	if b != nil {
		*out = append(*out, b)
	}
}

func parseReturnStmt(s *ast.ReturnStmt, fset *token.FileSet, lines [][]byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := ""
	if lineNo > 0 && lineNo <= len(lines) {
		code = strings.TrimSpace(string(lines[lineNo-1]))
	}
	return &Branch{
		Line:      lineNo,
		CodeLine:  code,
		Children:  nil,
		hasReturn: true,
	}
}

func parseIfStmt(s *ast.IfStmt, fset *token.FileSet, lines [][]byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := ""
	if lineNo > 0 && lineNo <= len(lines) {
		code = strings.TrimSpace(string(lines[lineNo-1]))
	}

	b := &Branch{
		Line:      lineNo,
		CodeLine:  code,
		Children:  ExtractBranches(s.Body, fset, lines),
		hasReturn: false,
	}

	if s.Else != nil {
		elseLine := fset.Position(s.Else.Pos()).Line
		elseCode := ""
		if elseLine > 0 && elseLine <= len(lines) {
			elseCode = strings.TrimSpace(string(lines[elseLine-1]))
		}

		var elseChildren []*Branch
		if elseBlock, ok := s.Else.(*ast.BlockStmt); ok {
			elseChildren = ExtractBranches(elseBlock, fset, lines)
		} else if elseIf, ok := s.Else.(*ast.IfStmt); ok {
			elseIfBranch := parseIfStmt(elseIf, fset, lines)
			if elseIfBranch != nil {
				elseChildren = []*Branch{elseIfBranch}
			}
		}
		b.Children = append(b.Children, &Branch{
			Line:      elseLine,
			CodeLine:  elseCode,
			Children:  elseChildren,
			hasReturn: false,
		})
	}

	return b
}

func parseForStmt(s *ast.ForStmt, fset *token.FileSet, lines [][]byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := ""
	if lineNo > 0 && lineNo <= len(lines) {
		code = strings.TrimSpace(string(lines[lineNo-1]))
	}
	return &Branch{
		Line:      lineNo,
		CodeLine:  code,
		Children:  ExtractBranches(s.Body, fset, lines),
		hasReturn: false,
	}
}

func parseRangeStmt(s *ast.RangeStmt, fset *token.FileSet, lines [][]byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := ""
	if lineNo > 0 && lineNo <= len(lines) {
		code = strings.TrimSpace(string(lines[lineNo-1]))
	}
	return &Branch{
		Line:      lineNo,
		CodeLine:  code,
		Children:  ExtractBranches(s.Body, fset, lines),
		hasReturn: false,
	}
}

func parseSwitchStmt(s *ast.SwitchStmt, fset *token.FileSet, lines [][]byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := ""
	if lineNo > 0 && lineNo <= len(lines) {
		code = strings.TrimSpace(string(lines[lineNo-1]))
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
			if caseLine > 0 && caseLine <= len(lines) {
				caseCode = strings.TrimSpace(string(lines[caseLine-1]))
			}
			caseChildren := extractFromStmtList(cs.Body, fset, lines)
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

func parseTypeSwitchStmt(s *ast.TypeSwitchStmt, fset *token.FileSet, lines [][]byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := ""
	if lineNo > 0 && lineNo <= len(lines) {
		code = strings.TrimSpace(string(lines[lineNo-1]))
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
			if caseLine > 0 && caseLine <= len(lines) {
				caseCode = strings.TrimSpace(string(lines[caseLine-1]))
			}
			caseChildren := extractFromStmtList(cs.Body, fset, lines)
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

func parseSelectStmt(s *ast.SelectStmt, fset *token.FileSet, lines [][]byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := ""
	if lineNo > 0 && lineNo <= len(lines) {
		code = strings.TrimSpace(string(lines[lineNo-1]))
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
			if commLine > 0 && commLine <= len(lines) {
				commCode = strings.TrimSpace(string(lines[commLine-1]))
			}
			commChildren := extractFromStmtList(cs.Body, fset, lines)
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

func parseBlockStmt(s *ast.BlockStmt, fset *token.FileSet, lines [][]byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := ""
	if lineNo > 0 && lineNo <= len(lines) {
		code = strings.TrimSpace(string(lines[lineNo-1]))
	}
	return &Branch{
		Line:      lineNo,
		CodeLine:  code,
		Children:  ExtractBranches(s, fset, lines),
		hasReturn: false,
	}
}

func extractFromStmtList(stmts []ast.Stmt, fset *token.FileSet, lines [][]byte) []*Branch {
	var children []*Branch
	for _, stmt := range stmts {
		visitStmt(stmt, fset, lines, &children)
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
