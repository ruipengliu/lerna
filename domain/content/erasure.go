package content

import (
	"context"
	"errors"
	v "github.com/ruipengliu/lerna/contract/v1_2"
)

// ErasureIdentity binds a trusted holder's closed key to the original seal.
// It is internal lifecycle data, never a public delete method or permission.
type ErasureIdentity struct {
	Binding   string       `json:"binding"`
	Ref       v.ContentRef `json:"content_ref"`
	ObjectKey string       `json:"object_key"`
	HolderID  string       `json:"holder_id"`
	SealID    string       `json:"seal_id"`
}

type ErasureObservation struct {
	Identity   ErasureIdentity `json:"identity"`
	Fenced     bool            `json:"fenced"`
	Erased     bool            `json:"erased"`
	Residual   []string        `json:"residual"`
	NextCursor string          `json:"next_cursor"`
}

var ErrBodySealed = errors.New("Content exact body is irreversibly sealed")
var ErrHolderBinding = errors.New("Content holder physical binding mismatch")

// ErasingObjects is consumed by the trusted Content lifecycle, outside DB Tx.
// Erased requires durable fencing and a separate exact-body observation.
type ErasingObjects interface {
	Objects
	FenceAndErase(context.Context, ErasureIdentity, []string) (ErasureObservation, error)
	ObserveErasure(context.Context, ErasureIdentity, string, int) (ErasureObservation, error)
}
