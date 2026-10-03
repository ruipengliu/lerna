package memory

import (
	"context"

	"github.com/ruipengliu/lerna/api"
)

// ForeignUseProvider 只读本次入口内存中的已取得证明，不执行 I/O 或读取旧磁盘许可。
// 提供的证明仍须通过每次原完整的 held copy、主体、签名及当前否决检查。
type ForeignUseProvider interface{ ForeignUses() ([]ForeignUse, error) }
type foreignProviderKey struct{}

// WithForeignUseProvider 建立新的入口载体，不隐式继承上一个入口的正面证明。
// 同调用树延续的准确深拷贝由显式宿主完成，领域门禁不推断扩大用途。
func WithForeignUseProvider(ctx context.Context, p ForeignUseProvider) context.Context {
	ctx = context.WithValue(ctx, foreignContextKey{}, []ForeignUse(nil))
	return context.WithValue(ctx, foreignProviderKey{}, struct{ Provider ForeignUseProvider }{p})
}

func foreignUses(ctx context.Context) ([]ForeignUse, error) {
	explicit, _ := ctx.Value(foreignContextKey{}).([]ForeignUse)
	provided := []ForeignUse{}
	if value, ok := ctx.Value(foreignProviderKey{}).(struct{ Provider ForeignUseProvider }); ok && value.Provider != nil {
		var err error
		provided, err = value.Provider.ForeignUses()
		if err != nil {
			return nil, err
		}
	}
	if len(explicit) > 100 || len(provided) > 100 {
		return nil, api.E("overloaded", "foreign_source_limit")
	}
	// 显式本次ctx里的同副本证明优先；其他已核用途可由宿主载体补齐。
	keys := map[string]bool{}
	merged := append([]ForeignUse{}, explicit...)
	for _, use := range explicit {
		keys[use.Reference.CopyID] = true
	}
	for _, use := range provided {
		if !keys[use.Reference.CopyID] {
			merged = append(merged, use)
		}
	}
	if len(merged) > 100 {
		return nil, api.E("overloaded", "foreign_source_limit")
	}
	if len(api.Raw(merged)) > 1<<20 {
		return nil, api.E("overloaded", "foreign_context_bytes_exceeded")
	}
	copyUses := make([]ForeignUse, len(merged))
	for i, use := range merged {
		body := api.Raw(use)
		if len(body) > 128<<10 {
			return nil, api.E("invalid_request", "foreign_proof_too_large")
		}
		if err := api.Decode(body, &copyUses[i]); err != nil {
			return nil, err
		}
	}
	return copyUses, nil
}
