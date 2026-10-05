//go:build fault

package fault_test

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/infra/sqlite"
)

// 规则：G3、R6
func TestEveryPersistencePointIsRegistered(t *testing.T) {
	registered := make(map[string]bool)
	for _, point := range sqlite.FaultPoints() {
		if _, exists := registered[point]; exists {
			t.Fatalf("duplicate persistence point: %s", point)
		}
		registered[point] = false
	}
	for _, root := range []string{"../../core", "../../infra"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				index := -1
				if selector.Sel.Name == "Transaction" {
					index = 1
				}
				if selector.Sel.Name == "Execute" {
					index = 4
				}
				if selector.Sel.Name == "transact" {
					index = 2
				}
				if index < 0 || len(call.Args) <= index {
					return true
				}
				// 受信存储的转发保留原标签；模块入口必须传字面名称。
				if id, ok := call.Args[index].(*ast.Ident); ok && id.Name == "point" && (strings.HasSuffix(path, "infra/sqlite/sqlite.go") || strings.HasSuffix(path, "core/durable/commands.go")) {
					return true
				}
				literal, ok := call.Args[index].(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					t.Errorf("%s: persistence point must be named", path)
					return true
				}
				point, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Error(err)
					return true
				}
				if _, ok := registered[point]; !ok {
					t.Errorf("%s: unregistered persistence point %q", path, point)
				} else {
					registered[point] = true
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for point, used := range registered {
		if !used {
			t.Errorf("registered point has no transaction: %s", point)
		}
	}
	if _, err := sqlite.WithFault(context.Background(), "misspelled.point", sqlite.CrashBeforeCommit); err == nil {
		t.Fatal("unknown point accepted")
	}
	if _, err := sqlite.WithFault(context.Background(), "content.stage", sqlite.FaultMode("misspelled")); err == nil {
		t.Fatal("unknown mode accepted")
	}
}

// 规则：R6、G3
func TestProductionBuildExcludesFaultConfiguration(t *testing.T) {
	cmd := exec.Command("go", "list", "-json", "-tags=", "github.com/ruipengliu/lerna/infra/sqlite")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var p struct{ GoFiles, IgnoredGoFiles []string }
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatal(err)
	}
	included, excluded := strings.Join(p.GoFiles, ","), strings.Join(p.IgnoredGoFiles, ",")
	if strings.Contains(included, "hooks_fault.go") || !strings.Contains(included, "hooks_production.go") || !strings.Contains(excluded, "hooks_fault.go") {
		t.Fatalf("fault configuration entered production: %s ignored=%s", included, excluded)
	}
}
