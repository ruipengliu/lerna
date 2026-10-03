package harness

import (
	"github.com/ruipengliu/lerna/api"
)

type originalDecoder struct {
	input  *api.Validator
	output *api.Validator
	kind   string
}

func decoderKey(name, digest string) string { return name + ":" + digest }

// RetainDecoder 只能由受信宿主显式安装固定历史合同；它不从远端或journal猜Schema。
// 当前 owner/profile/core/identity scope 仍由原 Entry 校验，旧decoder只恢复原责任。
func (c *Client) RetainDecoder(contract api.MethodContract) error {
	digest, err := api.Digest([]any{contract.InputSchema, contract.OutputSchema})
	if err != nil || digest != contract.SchemaDigest || !hasMethodOwner(c.Discovery.Methods, contract) {
		return api.E("unsupported", "retained_decoder_contract_mismatch")
	}
	input, err := api.NewValidator(contract.InputSchema)
	if err != nil {
		return err
	}
	output, err := api.NewValidator(contract.OutputSchema)
	if err != nil {
		return err
	}
	c.decoderMu.Lock()
	defer c.decoderMu.Unlock()
	key := decoderKey(contract.Name, contract.SchemaDigest)
	if _, ok := c.retained[key]; ok {
		return nil
	}
	if len(c.retained) >= 64 {
		return api.E("overloaded", "retained_decoder_limit")
	}
	c.retained[key] = originalDecoder{input: input, output: output, kind: contract.Kind}
	return nil
}
func hasMethodOwner(current []api.MethodContract, old api.MethodContract) bool {
	if old.Kind != "command" && old.Kind != "query" {
		return false
	}
	for _, contract := range current {
		if contract.Name == old.Name && contract.Owner == old.Owner && contract.Kind == old.Kind {
			return true
		}
	}
	return false
}
func (c *Client) entryDecoder(entry Entry) (originalDecoder, bool) {
	if entry.MethodSchemaDigest == c.methodSchemaDigest(entry.Command.Method) && entry.MethodSchemaDigest != "" {
		in, ok := c.inputs[entry.Command.Method]
		out, outputOK := c.outputs[entry.Command.Method]
		return originalDecoder{input: in, output: out, kind: "command"}, ok && outputOK
	}
	c.decoderMu.RLock()
	decoder, ok := c.retained[decoderKey(entry.Command.Method, entry.MethodSchemaDigest)]
	c.decoderMu.RUnlock()
	return decoder, ok && decoder.kind == "command"
}
