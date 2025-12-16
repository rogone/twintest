// main.go
package main

import (
	"flag"
	"fmt"

	"os"
)

var (
	srcFile = flag.String("src", "", "source go file to analyze")
	scope   = flag.String("scope", "all", "test scope: 'func', 'struct', or 'all'")
	paths   = flag.String("paths", "all", "path filtering: 'all' or 'return'")
)

func main() {
	flag.Parse()

	if *srcFile == "" {
		fmt.Fprintln(os.Stderr, "error: -src is required")
		os.Exit(1)
	}

	validScope := map[string]bool{"func": true, "struct": true, "all": true}
	if !validScope[*scope] {
		fmt.Fprintf(os.Stderr, "error: -scope must be one of 'func', 'struct', 'all'\n")
		os.Exit(1)
	}

	validPaths := map[string]bool{"all": true, "return": true}
	if !validPaths[*paths] {
		fmt.Fprintf(os.Stderr, "error: -paths must be 'all' or 'return'\n")
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
