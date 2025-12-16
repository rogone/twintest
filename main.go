// main.go
package main

import (
	"flag"
	"fmt"

	"os"
)

var (
	srcFile = flag.String("src", "", "source go file to analyze")
	scope   = flag.String("scope", "struct", "test scope: 'func', 'struct', or 'all'")
	paths   = flag.String("paths", "all", "path filtering: 'all' or 'return'")
	noctor  = flag.Bool("noctor", true, "no construct for type, use with -scope=struct")
)

func main() {
	flag.Parse()

	if *srcFile == "" {
		fmt.Fprintln(os.Stderr, "error: -src is required")
		flag.Usage()
		os.Exit(1)
	}

	validScope := map[string]bool{"func": true, "struct": true, "all": true}
	if !validScope[*scope] {
		fmt.Fprintf(os.Stderr, "error: -scope must be one of 'func', 'struct', 'all'\n")
		flag.Usage()
		os.Exit(1)
	}

	validPaths := map[string]bool{"all": true, "return": true}
	if !validPaths[*paths] {
		fmt.Fprintf(os.Stderr, "error: -paths must be 'all' or 'return'\n")
		flag.Usage()
		os.Exit(1)
	}

	structInfo, packageName, err := ParseFile(*srcFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if len(structInfo) == 0 {
		fmt.Println("No testable functions/methods found.")
		return
	}

	structInfo = trimByScope(structInfo)
	structInfo = trimByPaths(structInfo)
	if *noctor {
		structInfo = trimConstructor(structInfo)
	}
	structInfo = trimNoMethod(structInfo)

	err = GenerateTestFiles(*srcFile, structInfo, packageName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Done %s\n", *srcFile)
}

func trimByScope(structInfo []*StructInfo) []*StructInfo {
	switch *scope {
	case "func":
		for i := range structInfo {
			if structInfo[i].Name == "" {
				structInfo = []*StructInfo{structInfo[i]}
				break
			}
		}
	case "struct":
		newStructInfo := structInfo[:0]
		for i := range structInfo {
			if structInfo[i].Name != "" {
				newStructInfo = append(newStructInfo, structInfo[i])
			}
		}
		structInfo = newStructInfo
	}

	return structInfo
}

func trimByPaths(structInfo []*StructInfo) []*StructInfo {
	if *paths != "return" {
		return structInfo
	}

	for i := range structInfo {
		for ii := range structInfo[i].Methods {
			method := &structInfo[i].Methods[ii]

			branch := &Branch{
				Children: method.Branches,
			}

			trimNoReturnBranch(branch)
			method.Branches = branch.Children
		}
	}

	return structInfo
}

func trimNoReturnBranch(branch *Branch) {
	newBranch := branch.Children[:0]
	for i := range branch.Children {
		child := branch.Children[i]
		if child.HasReturn() {
			trimNoReturnBranch(child)
			newBranch = append(newBranch, child)
		}
	}
	branch.Children = newBranch
}

func trimNoMethod(structInfo []*StructInfo) []*StructInfo {
	newStructInfo := structInfo[:0]
	for i := range structInfo {
		if len(structInfo[i].Methods) == 0 {
			continue
		}
		newStructInfo = append(newStructInfo, structInfo[i])
	}
	return newStructInfo
}

func trimConstructor(structInfo []*StructInfo) []*StructInfo {
	m := make(map[string]bool, len(structInfo))
	var funcInfo *StructInfo
	for i := range structInfo {
		if name := structInfo[i].Name; name != "" {
			m["New"+name] = true
			m["new"+name] = true
		} else {
			funcInfo = structInfo[i]
		}
	}

	if funcInfo == nil {
		return structInfo
	}

	newMethods := funcInfo.Methods[:0]
	for i := range funcInfo.Methods {
		if name := funcInfo.Methods[i].Name; m[name] {
			continue
		}
		newMethods = append(newMethods, funcInfo.Methods[i])
	}
	funcInfo.Methods = newMethods

	return structInfo
}
