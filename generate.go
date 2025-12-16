package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed template/suite.tmpl
var suiteTemplate string

//go:embed template/func.tmpl
var funcTemplate string

func GenerateTestFiles(src string, ss []*StructInfo, packageName string) error {
	absPath, err := filepath.Abs(src)
	if err != nil {
		return err
	}

	dir := filepath.Dir(absPath)
	base := filepath.Base(absPath)

	for i := range ss {
		si := ss[i]

		outFile := strings.TrimSuffix(base, ".go") // + "_test.go"
		if si.Name == "" {
			outFile = fmt.Sprintf("%s_test.go", outFile)
		} else {
			outFile = fmt.Sprintf("%s_%s_suite_test.go", outFile, strings.ToLower(si.Name))
		}

		outFile = filepath.Join(dir, outFile)

		err = GenerateTestFile(outFile, si, packageName)
		if err != nil {
			return err
		}

		fmt.Printf("Generated %s\n", outFile)
	}
	return nil
}

func GenerateTestFile(filename string, si *StructInfo, packageName string) error {
	data := struct {
		PackageName string
		StructInfo  *StructInfo
	}{
		PackageName: packageName,
		StructInfo:  si,
	}

	tmplFile := suiteTemplate
	if si.Name == "" {
		tmplFile = funcTemplate
	}

	tmpl := template.Must(template.New("test").Funcs(template.FuncMap{
		"quote": func(s string) string { return fmt.Sprintf("%q", s) },
	}).Parse(tmplFile))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}

	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		// If formatting fails, use raw bytes (helpful for debugging)
		formatted = buf.Bytes()
	}

	return os.WriteFile(filename, formatted, 0644)
}
