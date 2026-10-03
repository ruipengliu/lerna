package harness

import (
	"context"
	"errors"
	"os"

	"github.com/ruipengliu/lerna/api"
)

// Receipt 查询本地保留的原命令；准确摘要、负责方与输出解码器仍由原 Journal 约束。
func (c *Client) Receipt(ctx context.Context, commandID string) (api.Receipt, error) {
	entry, err := c.Journal.Read(ctx, commandID)
	if errors.Is(err, os.ErrNotExist) {
		return api.Receipt{}, api.E("not_found", "original_journal_entry_not_found")
	}
	if err != nil {
		return api.Receipt{}, err
	}
	if err = c.checkOriginal(entry); err != nil {
		return api.Receipt{}, err
	}
	raw, err := c.Transport.Call(ctx, "receipt_lookup", api.Raw(api.ReceiptLookup{LogicalServiceID: entry.Command.LogicalServiceID, CommandID: entry.Command.CommandID}))
	if err != nil {
		return api.Receipt{}, err
	}
	return c.saveReceipt(ctx, entry, raw)
}

func (c *Client) methodSchemaDigest(name string) string {
	for _, method := range c.Discovery.Methods {
		if method.Name == name && method.Kind == "command" {
			return method.SchemaDigest
		}
	}
	return ""
}

func (c *Client) checkOriginal(entry Entry) error {
	if entry.IdentityScope != c.Discovery.IdentityScope || entry.Command.LogicalServiceID != c.Discovery.LogicalServiceID {
		return api.E("forbidden", "recovery_scope_mismatch")
	}
	if entry.SchemaDigest != c.Discovery.SchemaDigest || entry.Command.Protocol != c.Discovery.Protocol || entry.Command.Profile != c.Discovery.Profile || entry.MethodSchemaDigest == "" || entry.MethodSchemaDigest != c.methodSchemaDigest(entry.Command.Method) {
		return api.E("unsupported", "original_decoder_unavailable")
	}
	return nil
}
