package durable

import (
	"fmt"
	"reflect"
)

// Dependency 声明消费模块必须连接的命名依赖。
type Dependency struct {
	Name  string
	Value any
}

// RequireDependencies 按声明顺序拒绝首个缺项，包含接口内的 typed nil。
func RequireDependencies(module string, dependencies ...Dependency) error {
	for _, dependency := range dependencies {
		missing := dependency.Value == nil
		if !missing {
			value := reflect.ValueOf(dependency.Value)
			switch value.Kind() {
			case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
				missing = value.IsNil()
			}
		}
		if missing {
			return fmt.Errorf("missing required dependency: %s.%s", module, dependency.Name)
		}
	}
	return nil
}
