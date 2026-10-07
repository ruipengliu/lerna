package ledger

import (
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func checkAPIWait(op *v1.Operation, now int64) error {
	if op.ApiWait != nil && now < op.ApiWait.ReadyAtUnixMs {
		return command.Fail("API_WAIT_NOT_DUE")
	}
	return nil
}
