package durableworkdemo

import (
	"context"

	"github.com/ruipengliu/lerna/contract"
)

type Permission struct {
	Subject               contract.SubjectBinding
	Owner                 contract.OwnerRef
	Record, Read, Cleanup bool
}

// Permissions is an immutable exact trusted allow table, not payload policy.
type Permissions struct{ entries map[string]Permission }

func NewPermissions(entries []Permission) *Permissions {
	table := &Permissions{entries: make(map[string]Permission)}
	for _, entry := range entries {
		key, err := permissionKey(entry.Subject, entry.Owner)
		if err != nil {
			continue
		}
		// Only retain encoded immutable identity; no caller-owned delegation slice.
		table.entries[key] = Permission{Record: entry.Record, Read: entry.Read, Cleanup: entry.Cleanup}
	}
	return table
}
func permissionKey(subject contract.SubjectBinding, owner contract.OwnerRef) (string, error) {
	subjectJSON, err := contract.Encode(subject)
	if err != nil {
		return "", err
	}
	ownerJSON, err := contract.Encode(owner)
	if err != nil {
		return "", err
	}
	return string(subjectJSON) + "\n" + string(ownerJSON), nil
}
func (p *Permissions) Allows(subject contract.SubjectBinding, owner contract.OwnerRef, operation string) bool {
	if p == nil || subject.TenantID != owner.TenantID {
		return false
	}
	key, err := permissionKey(subject, owner)
	if err != nil {
		return false
	}
	entry := p.entries[key]
	switch operation {
	case "record":
		return entry.Record
	case "read":
		return entry.Read
	case "cleanup":
		return entry.Cleanup
	default:
		return false
	}
}
func (p *Permissions) AuthorizeCommandRead(ctx context.Context, subject contract.SubjectBinding, ref contract.CommandRef) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return p.Allows(subject, ref.Owner, "read"), nil
}
