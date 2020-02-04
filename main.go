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

	importPath := "github.com/go-gl/gl/v3.2-core/gl"
	outPath := "gl-proxy"

	pkg, err := build.Import(importPath, ".", build.FindOnly)
	if err != nil {
		log.Fatal(err)
	}

	// parse file
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, pkg.Dir, nil, parser.ParseComments)
	if err != nil {
		log.Fatal(err)
	}

	for _, pkg := range pkgs {
		if strings.HasSuffix(pkg.Name, "_test") {
			continue
		}
		for name, file := range pkgs[pkg.Name].Files {
			if strings.HasSuffix(name, "_test.go") {
				continue
			}

			file = doFile(file, importPath)
			if file == nil {
				continue
			}

			_, fileName := filepath.Split(name)
			path := filepath.Join(outPath, fileName)
			f, err := os.Create(path)
			if err != nil {
				log.Fatal(err)
			}
			if err := printer.Fprint(f, fset, file); err != nil {
				log.Fatal(err)
			}
			f.Close()
		}

		proxyTempl := template.Must(template.ParseFiles("__proxy.go.tmpl"))
		proxyWriter, err := os.OpenFile(filepath.Join(outPath, "proxy__.go"), os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			log.Fatal(err)
		}
		data := struct {
			Package string
		}{
			Package: pkg.Name,
		}
		err = proxyTempl.Execute(proxyWriter, data)
		if err != nil {
			log.Fatal(err)
		}
	}
}

/*func doFileNoop(root *ast.File, importPath string) *ast.File {

}*/

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
				newSpecs := make([]ast.Spec, 0)
				for i, _ := range gen.Specs {
					spec := gen.Specs[i]
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
					anyExported = true
				}
				if len(newSpecs) == 0 {
					continue
				}
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
				newSpecs := make([]ast.Spec, 0)
				for i, _ := range gen.Specs {
					spec := gen.Specs[i]
					typ, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if !typ.Name.IsExported() {
						continue
					}
					anyExported = true
					typ.Type = &ast.SelectorExpr{
						X:   ast.NewIdent(orgPkgRef),
						Sel: ast.NewIdent(typ.Name.Name),
					}
					newSpecs = append(newSpecs, spec)
				}
				if len(newSpecs) == 0 {
					continue
				}
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

	lastImportIdx := 0
	for lastImportIdx = range newDecls {
		decl := newDecls[lastImportIdx]
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			break
		}
		if gen.Tok != token.IMPORT {
			break
		}
	}

	newDecls = append(newDecls[:lastImportIdx], append([]ast.Decl{
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
		/*&ast.GenDecl{
			Tok: token.IMPORT,
			Specs: []ast.Spec{
				&ast.ImportSpec{
					Path: &ast.BasicLit{
						Kind:  token.STRING,
						Value: fmt.Sprintf("\"%s\"", "sync/atomic"),
					},
				},
			},
		},*/
	}, newDecls[lastImportIdx:]...)...)
	/*newDecls = append(newDecls, &ast.GenDecl{
		Tok: token.VAR,
		Specs: []ast.Spec{
			&ast.ValueSpec{
				Names: []*ast.Ident{
					ast.NewIdent("__callId"),
				},
				Type: ast.NewIdent("int64"),
			},
		},
	}, &ast.GenDecl{
		Tok: token.VAR,
		Specs: []ast.Spec{
			&ast.ValueSpec{
				Names: []*ast.Ident{
					ast.NewIdent("ProxyInvocationHandler"),
				},
				Type: &ast.FuncType{
					Params: &ast.FieldList{
						List: []*ast.Field{
							&ast.Field{
								Type: ast.NewIdent("string"),
							},
							&ast.Field{
								Type: ast.NewIdent("int64"),
							},
						},
					},
				},
			},
		},
	})
	newDecls = append(newDecls, &ast.FuncDecl{
		Name: ast.NewIdent("__invokeHandler"),
		Type: &ast.FuncType{
			Params: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Names: []*ast.Ident{
							ast.NewIdent("funcName"),
						},
						Type: ast.NewIdent("string"),
					},
					&ast.Field{
						Names: []*ast.Ident{
							ast.NewIdent("callId"),
						},
						Type: ast.NewIdent("int64"),
					},
				},
			},
			Results: &ast.FieldList{
				List: []*ast.Field{
					&ast.Field{
						Type: ast.NewIdent("string"),
					},
					&ast.Field{
						Type: ast.NewIdent("int64"),
					},
				},
			},
		},
		Body: &ast.BlockStmt{
			List: []ast.Stmt{
				&ast.IfStmt{
					Cond: &ast.BinaryExpr{
						X:  ast.NewIdent("callId"),
						Op: token.EQL,
						Y:  ast.NewIdent("-1"),
					},
					Body: &ast.BlockStmt{
						List: []ast.Stmt{
							&ast.AssignStmt{
								Tok: token.ASSIGN,
								Lhs: []ast.Expr{
									ast.NewIdent("callId"),
								},
								Rhs: []ast.Expr{
									&ast.CallExpr{
										Fun: &ast.SelectorExpr{
											X:   ast.NewIdent("atomic"),
											Sel: ast.NewIdent("AddInt64"),
										},
										Args: []ast.Expr{
											&ast.UnaryExpr{
												Op: token.AND,
												X:  ast.NewIdent("__callId"),
											},
											&ast.BasicLit{
												Kind:  token.INT,
												Value: "1",
											},
										},
									},
								},
							},
						},
					},
				},
				&ast.IfStmt{
					Cond: &ast.BinaryExpr{
						X:  ast.NewIdent("ProxyInvocationHandler"),
						Op: token.EQL,
						Y:  ast.NewIdent("nil"),
					},
					Body: &ast.BlockStmt{
						List: []ast.Stmt{
							&ast.ReturnStmt{
								Results: []ast.Expr{
									ast.NewIdent("funcName"),
									ast.NewIdent("callId"),
								},
							},
						},
					},
				},
				&ast.ExprStmt{
					X: &ast.CallExpr{
						Fun: ast.NewIdent("ProxyInvocationHandler"),
						Args: []ast.Expr{
							ast.NewIdent("funcName"),
							ast.NewIdent("callId"),
						},
					},
				},
				&ast.ReturnStmt{
					Results: []ast.Expr{
						ast.NewIdent("funcName"),
						ast.NewIdent("callId"),
					},
				},
			},
		},
	})*/

	root.Comments = append([]*ast.CommentGroup{&ast.CommentGroup{
		List: []*ast.Comment{
			&ast.Comment{
				Slash: 1,
				Text:  fmt.Sprintf("//Proxy for %q generated by github.com/Qendolin/go-gen-proxy. DO NOT EDIT.", importPath),
			},
		},
	}}, root.Comments...)

	root.Decls = newDecls
	return root
}
