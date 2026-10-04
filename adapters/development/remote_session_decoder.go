package development

import (
	"github.com/ruipengliu/lerna/adapters/collaboration"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

// 原 create Journal 的恢复保留准确旧decoder；不重置原身份与责任。
func retainRemoteSessionCreateDecoder(client *harness.Client) error {
	contract, err := collaboration.RemoteSessionLegacyCreateContract()
	if err != nil {
		return err
	}
	return client.RetainDecoder(contract)
}
