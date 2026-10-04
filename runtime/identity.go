package runtime

import "github.com/ruipengliu/lerna/api"

// Identity 在负责方创建原身份时调用；构造 Service 不生成身份，也不执行外部 I/O。
// 宿主可为每个实例注入独立实现，不使用共享可变全局。
type Identity interface {
	NewID(prefix string) string
}

type RandomIdentity struct{}

func (RandomIdentity) NewID(prefix string) string { return api.NewID(prefix) }

// IdentityOrDefault 只选择端口，随机生成仅在 NewID 调用时发生。
func IdentityOrDefault(identity Identity) Identity {
	if identity == nil {
		return RandomIdentity{}
	}
	return identity
}
