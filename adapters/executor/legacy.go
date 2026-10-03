package executor

import (
	"embed"

	"github.com/ruipengliu/lerna/api"
)

// 两次真实升级的原合同：c6e3228 首片、3bfd14f 设备数据库绑定片。
// 来源文件只用于恢复本地原journal，当前方法和新准入始终使用本版Contracts。
//
//go:embed legacy/*.json
var legacyContractsFS embed.FS

func retainedContracts() ([]api.MethodContract, error) {
	contracts := []api.MethodContract{}
	for _, path := range []string{"legacy/c6e3228.json", "legacy/3bfd14f.json"} {
		raw, err := legacyContractsFS.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var original []api.MethodContract
		if err = api.DecodeLimit(raw, &original, 1<<20); err != nil {
			return nil, err
		}
		contracts = append(contracts, original...)
	}
	return contracts, nil
}
