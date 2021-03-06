package main

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

func main() {

	if len(os.Args) < 3 {
		fmt.Println("Usage: go-gen-proxy <packageImportPath> <destinationFolder> [noop]")
		return
	}

	noop := false
	importPath := os.Args[1]
	outPath := os.Args[2]
	if len(os.Args) >= 4 && os.Args[3] == "noop" {
		noop = true
	}

	err := doPackage(importPath, outPath, noop)
	if err != nil {
		log.Fatal(err)
	}
}

func doPackage(importPath, outPath string, noop bool) error {
	err := os.MkdirAll(outPath, os.ModePerm)
	if err != nil {
		return err
	}

	pkg, err := build.Import(importPath, ".", build.FindOnly)
	if err != nil {
		return err
	}

	// parse file
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, pkg.Dir, nil, parser.ParseComments)
	if err != nil {
		return err
	}

	for _, pkg := range pkgs {
		if strings.HasSuffix(pkg.Name, "_test") {
			continue
		}
		for name, file := range pkgs[pkg.Name].Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}

			if noop {
				file = doFileNoop(file, importPath)
				if file == nil {
					continue
				}
			} else {
				file = doFile(file, importPath)
				if file == nil {
					continue
				}
			}

			_, fileName := filepath.Split(name)
			path := filepath.Join(outPath, fileName)
			f, err := os.Create(path)
			if err != nil {
				return err
			}
			if err := printer.Fprint(f, fset, file); err != nil {
				return err
			}
			f.Close()
		}

		proxyTempl := template.Must(template.New("proxy").Parse(`
// Code generated by github.com/Qendolin/go-gen-proxy. DO NOT EDIT.
package {{ .Package }}

import "sync/atomic"

var __callId int64
var ProxyInvocationHandler func(string, int64)

func __invokeHandler(funcName string, callId int64) (string, int64) {
	if callId == -1 {
		callId = atomic.AddInt64(&__callId, 1)
	}
	if ProxyInvocationHandler == nil {
		return funcName, callId
	}
	ProxyInvocationHandler(funcName, callId)
	return funcName, callId
}`))

		if !noop {
			proxyWriter, err := os.OpenFile(filepath.Join(outPath, "proxy__.go"), os.O_WRONLY|os.O_CREATE, 0666)
			if err != nil {
				return err
			}
			data := struct {
				Package string
			}{
				Package: pkg.Name,
			}
			err = proxyTempl.Execute(proxyWriter, data)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

const codeGeneratedWarningFormat = "// Code generated by github.com/Qendolin/go-gen-proxy. Proxy for %q. DO NOT EDIT."

func doFileNoop(root *ast.File, importPath string) *ast.File {
	orgPkgRef := fmt.Sprintf("__%s", root.Name.Name)
	newDecls := []ast.Decl{}
	anyExported := false
	for _, decl := range root.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok {
			if gen.Tok == token.VAR {
				newSpecs := getExportedVarSpecs(gen.Specs, orgPkgRef)
				if len(newSpecs) == 0 {
					continue
				}
				anyExported = true
				gen.Specs = newSpecs
				newDecls = append(newDecls, gen)
			} else if gen.Tok == token.TYPE {
				newSpecs := getExportedTypeSpecs(gen.Specs, orgPkgRef)
				if len(newSpecs) == 0 {
					continue
				}
				anyExported = true
				gen.Specs = newSpecs
				newDecls = append(newDecls, gen)
			}
		} else if fn, ok := decl.(*ast.FuncDecl); ok {
			if !fn.Name.IsExported() {
				continue
			}
			newDecls = append(newDecls, &ast.GenDecl{
				Tok:    token.VAR,
				Doc:    fn.Doc,
				TokPos: fn.Pos(),
				Specs: []ast.Spec{
					&ast.ValueSpec{
						Names: []*ast.Ident{
							ast.NewIdent(fn.Name.Name),
						},
						Values: []ast.Expr{
							&ast.SelectorExpr{
								X:   ast.NewIdent(orgPkgRef),
								Sel: ast.NewIdent(fn.Name.Name),
							},
						},
					},
				},
			})
			anyExported = true
		}
	}

	if !anyExported {
		return nil
	}

	newDecls = addOrgRefImport(newDecls, orgPkgRef, importPath)
	root.Comments = append([]*ast.CommentGroup{&ast.CommentGroup{
		List: []*ast.Comment{
			&ast.Comment{
				Slash: 1,
				Text:  fmt.Sprintf(codeGeneratedWarningFormat, importPath),
			},
		},
	}}, root.Comments...)

	root.Decls = newDecls
	return root
}

func doFile(root *ast.File, importPath string) *ast.File {
	orgPkgRef := fmt.Sprintf("__%s", root.Name.Name)
	newDecls := []ast.Decl{}
	anyExported := false
	imports := make(map[string]string)
	usedImports := make(map[string]bool)

declLoop:
	for _, decl := range root.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if ok {
			if gen.Tok == token.VAR {
				newSpecs := getExportedVarSpecs(gen.Specs, orgPkgRef)
				if len(newSpecs) == 0 {
					continue
				}
				anyExported = true
				gen.Specs = newSpecs
				newDecls = append(newDecls, gen)
			} else if gen.Tok == token.IMPORT {
				for _, spec := range gen.Specs {
					imp, ok := spec.(*ast.ImportSpec)
					if ok {
						if imp.Path != nil {
							path := strings.ReplaceAll(imp.Path.Value, "\"", "")
							if imp.Name == nil {
								pkg, err := build.Import(path, ".", build.FindOnly)
								if err != nil {
									continue
								}
								imports[pkg.ImportPath] = path
							} else {
								imports[imp.Name.Name] = path
							}
						}
					}
				}
			} else if gen.Tok == token.TYPE {
				newSpecs := getExportedTypeSpecs(gen.Specs, orgPkgRef)
				if len(newSpecs) == 0 {
					continue
				}
				anyExported = true
				gen.Specs = newSpecs
				newDecls = append(newDecls, gen)
			}
			continue
		}

		fn, ok := decl.(*ast.FuncDecl)
		if ok {
			if !fn.Name.IsExported() {
				continue
			}
			ellipsis := 0
			newImports := []string{}
			args := make([]ast.Expr, fn.Type.Params.NumFields())
			for i, arg := range fn.Type.Params.List {
				ident, ok := arg.Type.(*ast.Ident)
				if ok {
					if !ident.IsExported() && ident.Obj != nil {
						continue declLoop
					}
				}
				_, ok = arg.Type.(*ast.Ellipsis)
				if ok {
					ellipsis = 1
				}
				args[i] = &ast.Ident{
					Name: arg.Names[0].Name,
				}
				selExpr, ok := arg.Type.(*ast.SelectorExpr)
				if ok {
					ident, ok = selExpr.X.(*ast.Ident)
					if ok {
						newImports = append(newImports, ident.Name)
					}
				}
			}

			hasResults := false
			if fn.Type.Results != nil {
				for _, arg := range fn.Type.Results.List {
					hasResults = true
					ident, ok := arg.Type.(*ast.Ident)
					if ok {
						if !ident.IsExported() && ident.Obj != nil {
							continue declLoop
						}
					}
					selExpr, ok := arg.Type.(*ast.SelectorExpr)
					if ok {
						ident, ok = selExpr.X.(*ast.Ident)
						if ok {
							newImports = append(newImports, ident.Name)
						}
					}
				}
			}

			fn.Body.List = []ast.Stmt{
				&ast.DeferStmt{
					Defer: 1,
					Call: &ast.CallExpr{
						Fun: ast.NewIdent("__invokeHandler"),
						Args: []ast.Expr{
							&ast.CallExpr{
								Fun: ast.NewIdent("__invokeHandler"),
								Args: []ast.Expr{
									&ast.BasicLit{
										Kind:  token.STRING,
										Value: fmt.Sprintf("\"%s\"", fn.Name.Name),
									},
									&ast.BasicLit{
										Kind:  token.INT,
										Value: "-1",
									},
								},
							},
						},
					},
				},
			}
			if hasResults {
				fn.Body.List = append(fn.Body.List, &ast.ReturnStmt{
					Results: []ast.Expr{
						&ast.CallExpr{
							Fun: &ast.SelectorExpr{
								X:   ast.NewIdent(orgPkgRef),
								Sel: ast.NewIdent(fn.Name.Name),
							},
							Args:     args,
							Ellipsis: token.Pos(ellipsis),
						},
					},
				})
			}
			anyExported = true
			newDecls = append(newDecls, decl)
			for _, imp := range newImports {
				usedImports[imp] = true
			}
		}
	}

	if !anyExported {
		return nil
	}

	newImports := make([]ast.Decl, 0)
	for pkg := range usedImports {
		if _, ok := imports[pkg]; !ok {
			continue
		}
		name := new(ast.Ident)
		if imports[pkg] != pkg {
			name = ast.NewIdent(pkg)
		}
		newImports = append(newImports, &ast.GenDecl{
			Tok: token.IMPORT,
			Specs: []ast.Spec{
				&ast.ImportSpec{
					Name: name,
					Path: &ast.BasicLit{
						Kind:  token.STRING,
						Value: fmt.Sprintf("\"%s\"", imports[pkg]),
					},
				},
			},
		})
	}
	newDecls = append(newImports, newDecls...)

	newDecls = addOrgRefImport(newDecls, orgPkgRef, importPath)

	root.Comments = append([]*ast.CommentGroup{&ast.CommentGroup{
		List: []*ast.Comment{
			&ast.Comment{
				Slash: 1,
				Text:  fmt.Sprintf(codeGeneratedWarningFormat, importPath),
			},
		},
	}}, root.Comments...)

	root.Decls = newDecls
	return root
}

func getExportedVarSpecs(oldSpecs []ast.Spec, orgPkgRef string) []ast.Spec {
	newSpecs := make([]ast.Spec, 0)
	for i, _ := range oldSpecs {
		spec := oldSpecs[i]
		val, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		val.Type = nil
		newValues := []ast.Expr{}
		newNames := []*ast.Ident{}
		for _, name := range val.Names {
			if !name.IsExported() {
				continue
			}
			newNames = append(newNames, name)
			newValues = append(newValues, &ast.SelectorExpr{
				X:   ast.NewIdent(orgPkgRef),
				Sel: ast.NewIdent(name.Name),
			})
		}
		if len(newNames) == 0 {
			continue
		}
		val.Names = newNames
		val.Values = newValues
		newSpecs = append(newSpecs, spec)
	}
	return newSpecs
}

func getExportedTypeSpecs(oldSpecs []ast.Spec, orgPkgRef string) []ast.Spec {
	newSpecs := make([]ast.Spec, 0)
	for i, _ := range oldSpecs {
		spec := oldSpecs[i]
		typ, ok := spec.(*ast.TypeSpec)
		if !ok {
			continue
		}
		if !typ.Name.IsExported() {
			continue
		}
		typ.Type = &ast.SelectorExpr{
			X:   ast.NewIdent(orgPkgRef),
			Sel: ast.NewIdent(typ.Name.Name),
		}
		newSpecs = append(newSpecs, spec)
	}
	return newSpecs
}

func addOrgRefImport(decls []ast.Decl, orgPkgRef, importPath string) []ast.Decl {
	lastImportIdx := 0
	for lastImportIdx = range decls {
		decl := decls[lastImportIdx]
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			break
		}
		if gen.Tok != token.IMPORT {
			break
		}
	}

	decls = append(decls[:lastImportIdx], append([]ast.Decl{
		&ast.GenDecl{
			Tok: token.IMPORT,
			Specs: []ast.Spec{
				&ast.ImportSpec{
					Name: ast.NewIdent(orgPkgRef),
					Path: &ast.BasicLit{
						Kind:  token.STRING,
						Value: fmt.Sprintf("\"%s\"", importPath),
					},
				},
			},
		},
	}, decls[lastImportIdx:]...)...)
	return decls
}
