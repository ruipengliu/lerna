package execution

import (
	"github.com/ruipengliu/lerna/api"
	"sort"
)

func stableID(prefix, key string) string { return prefix + "_" + api.Hash([]byte(key))[7:39] }
func sortAmounts(a []api.Amount)         { sort.Slice(a, func(i, j int) bool { return a[i].Unit < a[j].Unit }) }

// BuiltinComponentID 固定内置组件身份；能力名称不能冒充线协议 Id。
func BuiltinComponentID(name string) string { return stableID("component", name) }
