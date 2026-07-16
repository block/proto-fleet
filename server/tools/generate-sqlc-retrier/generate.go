package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

type templateData struct {
	Imports []importSpec
	Methods []methodSpec
}

type importSpec struct {
	Name string
	Path string
}

type methodSpec struct {
	Name       string
	Signature  string
	ContextArg string
	CallArgs   string
	ResultType string
	ErrorOnly  bool
}

func generate(querierPath string) ([]byte, error) {
	data, err := parseQuerier(querierPath)
	if err != nil {
		return nil, err
	}

	var output bytes.Buffer
	if err := retryingQuerierTemplate.Execute(&output, data); err != nil {
		return nil, fmt.Errorf("render generated source: %w", err)
	}

	formatted, err := format.Source(output.Bytes())
	if err != nil {
		return nil, fmt.Errorf("format generated source: %w\n%s", err, output.Bytes())
	}
	return formatted, nil
}

func parseQuerier(path string) (templateData, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return templateData{}, fmt.Errorf("parse querier: %w", err)
	}
	if file.Name.Name != "sqlc" {
		return templateData{}, fmt.Errorf("querier package is %q, want sqlc", file.Name.Name)
	}

	methods, err := querierMethods(fset, file)
	if err != nil {
		return templateData{}, err
	}
	imports, err := querierImports(file)
	if err != nil {
		return templateData{}, err
	}
	return templateData{Imports: imports, Methods: methods}, nil
}

func querierMethods(fset *token.FileSet, file *ast.File) ([]methodSpec, error) {
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.TYPE {
			continue
		}
		for _, spec := range genDecl.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != "Querier" {
				continue
			}
			iface, ok := typeSpec.Type.(*ast.InterfaceType)
			if !ok {
				return nil, errors.New("Querier is not an interface")
			}

			methods := make([]methodSpec, 0, len(iface.Methods.List))
			for _, field := range iface.Methods.List {
				method, err := normalizeMethod(fset, field)
				if err != nil {
					return nil, err
				}
				methods = append(methods, method)
			}
			return methods, nil
		}
	}
	return nil, errors.New("Querier interface not found")
}

func normalizeMethod(fset *token.FileSet, field *ast.Field) (methodSpec, error) {
	if len(field.Names) != 1 {
		return methodSpec{}, errors.New("Querier contains an embedded or unnamed method")
	}
	name := field.Names[0].Name
	funcType, ok := field.Type.(*ast.FuncType)
	if !ok {
		return methodSpec{}, fmt.Errorf("Querier method %s is not a function", name)
	}

	contextArg, callArgs, err := methodParameters(name, funcType)
	if err != nil {
		return methodSpec{}, err
	}
	resultType, errorOnly, err := methodResult(fset, name, funcType)
	if err != nil {
		return methodSpec{}, err
	}
	signature, err := formatNode(fset, funcType)
	if err != nil {
		return methodSpec{}, fmt.Errorf("format %s signature: %w", name, err)
	}

	return methodSpec{
		Name:       name,
		Signature:  strings.TrimPrefix(signature, "func"),
		ContextArg: contextArg,
		CallArgs:   strings.Join(callArgs, ", "),
		ResultType: resultType,
		ErrorOnly:  errorOnly,
	}, nil
}

func methodParameters(name string, funcType *ast.FuncType) (string, []string, error) {
	params := funcType.Params
	if params == nil || len(params.List) == 0 {
		return "", nil, fmt.Errorf("Querier method %s has no context parameter", name)
	}

	args := make([]string, 0, len(params.List))
	for _, field := range params.List {
		if len(field.Names) != 1 {
			return "", nil, fmt.Errorf("Querier method %s has an unnamed or grouped parameter", name)
		}
		arg := field.Names[0].Name
		if _, ok := field.Type.(*ast.Ellipsis); ok {
			arg += "..."
		}
		args = append(args, arg)
	}

	contextParam := params.List[0]
	selector, ok := contextParam.Type.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Context" {
		return "", nil, fmt.Errorf("Querier method %s first parameter is not context.Context", name)
	}
	packageName, ok := selector.X.(*ast.Ident)
	if !ok || packageName.Name != "context" {
		return "", nil, fmt.Errorf("Querier method %s first parameter is not context.Context", name)
	}
	return contextParam.Names[0].Name, args, nil
}

func methodResult(fset *token.FileSet, name string, funcType *ast.FuncType) (string, bool, error) {
	results := funcType.Results
	if results == nil {
		return "", false, fmt.Errorf("Querier method %s has unsupported return shape", name)
	}
	for _, field := range results.List {
		if len(field.Names) != 0 {
			return "", false, fmt.Errorf("Querier method %s has named results", name)
		}
	}
	if len(results.List) == 1 && isError(results.List[0].Type) {
		return "", true, nil
	}
	if len(results.List) != 2 || !isError(results.List[1].Type) {
		return "", false, fmt.Errorf("Querier method %s has unsupported return shape", name)
	}

	resultType, err := formatNode(fset, results.List[0].Type)
	if err != nil {
		return "", false, fmt.Errorf("format %s result type: %w", name, err)
	}
	return resultType, false, nil
}

func isError(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "error"
}

func querierImports(file *ast.File) ([]importSpec, error) {
	imports := make([]importSpec, 0, len(file.Imports))
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return nil, fmt.Errorf("parse import %s: %w", spec.Path.Value, err)
		}
		name := ""
		if spec.Name != nil {
			name = spec.Name.Name
		}
		imports = append(imports, importSpec{Name: name, Path: path})
	}
	return imports, nil
}

func formatNode(fset *token.FileSet, node any) (string, error) {
	var output bytes.Buffer
	if err := format.Node(&output, fset, node); err != nil {
		return "", fmt.Errorf("format node: %w", err)
	}
	return output.String(), nil
}
