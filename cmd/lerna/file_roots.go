package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// fileRootFlags 只解析固定宿主映射；原生路径与权限在出口内核验。
type fileRootFlags map[string]string

func (f *fileRootFlags) String() string {
	if f == nil {
		return ""
	}
	values := make([]string, 0, len(*f))
	for name, path := range *f {
		values = append(values, name+"="+path)
	}
	sort.Strings(values)
	return strings.Join(values, ",")
}
func (f *fileRootFlags) Set(value string) error {
	name, path, ok := strings.Cut(value, "=")
	if !ok || name == "" || len(name) > 128 || strings.HasPrefix(name, ".") || !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return fmt.Errorf("INVALID_FILE_ROOT_CONFIG: expected unique NAME=/absolute/path")
	}
	for _, c := range name {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != '.' {
			return fmt.Errorf("INVALID_FILE_ROOT_CONFIG: invalid root name")
		}
	}
	if _, exists := (*f)[name]; exists {
		return fmt.Errorf("INVALID_FILE_ROOT_CONFIG: duplicate root name")
	}
	if *f == nil {
		*f = make(fileRootFlags)
	}
	(*f)[name] = path
	return nil
}
