package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

const (
	BranchIfHost = iota + 1
	BranchIf
	BranchElseIf
	BranchElse
	BranchFor
	BranchRange
	BranchSwitch
	BranchTypeSwitch
	BranchCase
	BranchDefault
	BranchSelect
	BranchCommClause
	BranchCommClauseDefault
	BranchBlock
	BranchReturn
)

// Branch represents a control-flow branch (if, for, switch case, return, etc.)
type Branch struct {
	Type      int
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
		dummy := &StructInfo{}
		structTypes[""] = dummy
		structs = append(structs, dummy)
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
		Type:      BranchReturn,
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
		Type:     BranchIfHost,
		Line:     lineNo,
		CodeLine: code,
		Children: []*Branch{ // if
			{
				Type:      BranchIf,
				Line:      lineNo,
				CodeLine:  code,
				Children:  ExtractBranches(s.Body, fset, src),
				hasReturn: false,
			},
		},
		hasReturn: false,
	}

	if s.Else != nil {
		if elseIf, ok := s.Else.(*ast.IfStmt); ok {
			curr := elseIf
			for curr != nil {
				b.Children = append(b.Children, &Branch{
					Type:      BranchElseIf,
					Line:      fset.Position(curr.Pos()).Line,
					CodeLine:  "else " + nodeToCode(curr, fset, src),
					Children:  ExtractBranches(curr.Body, fset, src),
					hasReturn: false,
				})

				if curr.Else != nil {
					if next, ok := curr.Else.(*ast.IfStmt); ok {
						curr = next
						continue
					} else {
						b.Children = append(b.Children, &Branch{
							Type:      BranchElse,
							Line:      fset.Position(curr.Else.Pos()).Line,
							CodeLine:  fmt.Sprintf("else // of [%s]:@%d", code, lineNo),
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
				Type:      BranchElse,
				Line:      fset.Position(s.Else.Pos()).Line,
				CodeLine:  fmt.Sprintf("else // of [%s]:@%d", code, lineNo),
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

	return &Branch{
		Type:      BranchFor,
		Line:      lineNo,
		CodeLine:  code,
		Children:  ExtractBranches(s.Body, fset, src),
		hasReturn: false,
	}
}

func parseRangeStmt(s *ast.RangeStmt, fset *token.FileSet, src []byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := nodeToCode(s, fset, src)

	return &Branch{
		Type:      BranchRange,
		Line:      lineNo,
		CodeLine:  code,
		Children:  ExtractBranches(s.Body, fset, src),
		hasReturn: false,
	}
}

func parseSwitchStmt(s *ast.SwitchStmt, fset *token.FileSet, src []byte) *Branch {
	lineNo := fset.Position(s.Pos()).Line
	code := nodeToCode(s, fset, src)

	b := &Branch{
		Type:      BranchSwitch,
		Line:      lineNo,
		CodeLine:  code,
		Children:  nil,
		hasReturn: false,
	}

	for _, cc := range s.Body.List {
		if cs, ok := cc.(*ast.CaseClause); ok {
			caseLine := fset.Position(cs.Pos()).Line
			caseCode := nodeToCode(cs, fset, src)
			typ := BranchCase
			if len(cs.List) == 0 { //default
				caseCode = fmt.Sprintf("%s // [%s]:@%d", caseCode, code, lineNo)
				typ = BranchDefault
			}
			caseChildren := extractFromStmtList(cs.Body, fset, src)
			b.Children = append(b.Children, &Branch{
				Type:      typ,
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

	b := &Branch{
		Type:      BranchTypeSwitch,
		Line:      lineNo,
		CodeLine:  code,
		Children:  nil,
		hasReturn: false,
	}

	for _, cc := range s.Body.List {
		if cs, ok := cc.(*ast.CaseClause); ok {
			caseLine := fset.Position(cs.Pos()).Line
			caseCode := nodeToCode(cs, fset, src)
			typ := BranchCase
			if len(cs.List) == 0 { //default
				caseCode = fmt.Sprintf("%s // of [%s]:@%d", caseCode, code, lineNo)
				typ = BranchDefault
			}
			caseChildren := extractFromStmtList(cs.Body, fset, src)
			b.Children = append(b.Children, &Branch{
				Type:      typ,
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

	b := &Branch{
		Type:      BranchSelect,
		Line:      lineNo,
		CodeLine:  code,
		Children:  nil,
		hasReturn: false,
	}

	for _, cc := range s.Body.List {
		if cs, ok := cc.(*ast.CommClause); ok {
			commLine := fset.Position(cs.Pos()).Line
			commCode := nodeToCode(cs, fset, src)
			typ := BranchCommClause
			if cs.Comm == nil { //default
				commCode = fmt.Sprintf("%s // of [%s]:@%d", commCode, code, lineNo)
				typ = BranchCommClauseDefault
			}
			commChildren := extractFromStmtList(cs.Body, fset, src)
			b.Children = append(b.Children, &Branch{
				Type:      typ,
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
	b := &Branch{
		Type:      BranchBlock,
		Line:      lineNo,
		CodeLine:  code,
		Children:  ExtractBranches(s, fset, src),
		hasReturn: false,
	}
	if len(b.Children) == 1 {
		return b.Children[0]
	}
	return b
}

func extractFromStmtList(stmts []ast.Stmt, fset *token.FileSet, src []byte) []*Branch {
	var children []*Branch
	for _, stmt := range stmts {
		visitStmt(stmt, fset, src, &children)
	}
	return children
}

func nodeToCode(stmt ast.Stmt, fset *token.FileSet, src []byte) string {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		start := fset.Position(stmt.Pos()).Offset
		end := fset.Position(stmt.End()).Offset
		return strings.TrimSpace(string(src[start:end]))
	case *ast.IfStmt:
		start := fset.Position(stmt.Pos()).Offset
		end := fset.Position(s.Cond.End()).Offset
		return strings.TrimSpace(string(src[start:end]))
	case *ast.ForStmt:
		start := fset.Position(stmt.Pos()).Offset
		end := fset.Position(s.Body.Pos() - 1).Offset
		//if s.Post != nil {
		//	end = fset.Position(s.Post.End()).Offset
		//} else if s.Cond != nil {
		//	end = fset.Position(s.Cond.End()).Offset
		//} else if s.Init != nil {
		//	end = fset.Position(s.Init.End()).Offset
		//}
		return strings.TrimSpace(string(src[start:end]))
	case *ast.RangeStmt:
		start := fset.Position(s.Pos()).Offset
		end := fset.Position(s.X.End()).Offset
		return strings.TrimSpace(string(src[start:end]))
	case *ast.SwitchStmt:
		start := fset.Position(s.Pos()).Offset
		end := fset.Position(s.Tag.End()).Offset
		return strings.TrimSpace(string(src[start:end]))
	case *ast.TypeSwitchStmt:
		start := fset.Position(s.Pos()).Offset
		end := fset.Position(s.Assign.End()).Offset
		return strings.TrimSpace(string(src[start:end]))
	case *ast.CaseClause:
		start := fset.Position(s.Pos()).Offset
		end := fset.Position(s.Colon).Offset
		return strings.TrimSpace(string(src[start:end]))
	case *ast.SelectStmt:
		start := fset.Position(s.Pos()).Offset
		end := start + 6
		return strings.TrimSpace(string(src[start:end]))
	case *ast.CommClause:
		start := fset.Position(s.Pos()).Offset
		end := fset.Position(s.Colon).Offset
		return strings.TrimSpace(string(src[start:end]))
	case *ast.BlockStmt:
		return "<block>"
	default:
		return "<invalid>"
	}
}
