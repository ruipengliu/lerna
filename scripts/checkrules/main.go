// 检查开发规范第 5 节要求的测试规则标注。
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	failed := false
	for _, base := range []string{"core/egress", "core/grants", "core/budget", "core/durable", "core/ledger", "infra/postgres", "infra/sqlite"} {
		path := filepath.Join(root, base)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(path, func(dir string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() {
				return nil
			}
			files, err := os.ReadDir(dir)
			if err != nil {
				return err
			}
			hasGo, annotated := false, false
			for _, file := range files {
				if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") {
					continue
				}
				hasGo = true
				if !strings.HasSuffix(file.Name(), "_test.go") {
					continue
				}
				parsed, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, file.Name()), nil, parser.ParseComments)
				if err != nil {
					return err
				}
				for _, decl := range parsed.Decls {
					fn, ok := decl.(*ast.FuncDecl)
					if !ok || fn.Recv != nil || !isTest(fn) || fn.Doc == nil {
						continue
					}
					for _, comment := range fn.Doc.List {
						if strings.HasPrefix(comment.Text, "// 规则：") && strings.TrimSpace(strings.TrimPrefix(comment.Text, "// 规则：")) != "" {
							annotated = true
						}
					}
				}
			}
			if hasGo && !annotated {
				fmt.Fprintf(os.Stderr, "%s: 缺少带 // 规则： 标注的测试\n", dir)
				failed = true
			}
			return nil
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

// isTest 只接受 Go 测试入口，辅助函数的注释不能代替测试证据。
func isTest(fn *ast.FuncDecl) bool {
	name := fn.Name.Name
	if !strings.HasPrefix(name, "Test") {
		return false
	}
	if len(name) > 4 {
		r, _ := utf8.DecodeRuneInString(name[4:])
		if unicode.IsLower(r) {
			return false
		}
	}
	if fn.Type.Results != nil || fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return false
	}
	param := fn.Type.Params.List[0]
	if len(param.Names) > 1 {
		return false
	}
	pointer, ok := param.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	typ, ok := pointer.X.(*ast.SelectorExpr)
	return ok && typ.Sel.Name == "T"
}
