package execution

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
)

type collectionHead struct {
	Revision uint64 `json:"revision"`
	Count    uint64 `json:"count"`
}
type cursorSlot struct {
	Revision             uint64 `json:"revision"`
	Kind                 string `json:"kind"`
	SubjectID            string `json:"subject_id"`
	CredentialGeneration uint64 `json:"credential_generation"`
	CollectionRevision   uint64 `json:"collection_revision"`
	AfterID              string `json:"after_id"`
	ExpiresAt            string `json:"expires_at"`
}

func bumpCollection(ctx context.Context, tx rt.Tx, kind string, created bool) error {
	var head collectionHead
	rev, err := tx.Get(ctx, Namespace+".collections", kind, &head)
	if api.IsCode(err, "not_found") {
		head = collectionHead{Revision: 1}
		if created {
			head.Count = 1
		}
		return tx.Create(ctx, Namespace+".collections", kind, "", head)
	}
	if err != nil {
		return err
	}
	head.Revision = rev + 1
	if created {
		head.Count++
	}
	return tx.Put(ctx, Namespace+".collections", kind, rev, head)
}
func putEnvironment(ctx context.Context, tx rt.Tx, id string, revision uint64, env Environment) error {
	if err := tx.Put(ctx, Namespace+".environments", id, revision, env); err != nil {
		return err
	}
	return bumpCollection(ctx, tx, "environments", false)
}

// Cursor使用耐久、受认证主体约束的100个有限slot。轮换推进slot修订，旧token不可读旧位置。
// slot是元数据对象，不复用其身份；到期或被后继轮换时返回cursor_expired。
func pageRecords(ctx context.Context, st rt.Store, sc rt.Scope, a rt.Auth, kind string, p api.ListInput) ([]rt.Record, uint64, string, bool, error) {
	if p.Limit < 1 || p.Limit > 100 {
		return nil, 0, "", false, api.E("invalid_request", "invalid_page_limit")
	}
	var records []rt.Record
	var collectionRevision uint64 = 1
	var next string
	var exhausted = true
	status, err := st.Within(ctx, sc, []string{Namespace}, func(tx rt.Tx) error {
		var head collectionHead
		if _, err := tx.Get(ctx, Namespace+".collections", kind, &head); api.IsCode(err, "not_found") {
			if p.Cursor != "" {
				return api.E("cursor_expired", "cursor_not_available")
			}
			records = []rt.Record{}
			return nil
		} else if err != nil {
			return err
		}
		collectionRevision = head.Revision
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		after := ""
		if p.Cursor != "" {
			parts := strings.Split(p.Cursor, ":")
			if len(parts) != 2 || !api.ValidID(parts[0]) {
				return api.E("invalid_request", "invalid_cursor")
			}
			version, err := strconv.ParseUint(parts[1], 10, 64)
			if err != nil || version == 0 {
				return api.E("invalid_request", "invalid_cursor")
			}
			var cursor cursorSlot
			rev, err := tx.Get(ctx, Namespace+".cursors", parts[0], &cursor)
			if err != nil {
				return api.E("cursor_expired", "cursor_not_available")
			}
			if rev != version || cursor.Kind != kind || cursor.SubjectID != a.SubjectID || cursor.CredentialGeneration != a.CredentialGeneration {
				return api.E("cursor_expired", "cursor_scope_changed")
			}
			deadline, err := api.ParseTime(cursor.ExpiresAt)
			if err != nil || !now.Before(deadline) {
				return api.E("cursor_expired", "cursor_expired")
			}
			if cursor.CollectionRevision != head.Revision {
				return api.E("snapshot_required", "collection_changed")
			}
			after = cursor.AfterID
		}
		records, err = tx.List(ctx, Namespace+"."+kind, "", after, int(p.Limit)+1)
		if err != nil {
			return err
		}
		exhausted = len(records) <= int(p.Limit)
		if exhausted {
			return nil
		}
		records = records[:p.Limit]
		for slot := 0; slot < 100; slot++ {
			id := stableID("cursor", kind+":"+strconv.Itoa(slot))
			var cursor cursorSlot
			rev, err := tx.Get(ctx, Namespace+".cursors", id, &cursor)
			if err == nil {
				deadline, e := api.ParseTime(cursor.ExpiresAt)
				if e != nil {
					return e
				}
				if now.Before(deadline) {
					continue
				}
			} else if !api.IsCode(err, "not_found") {
				return err
			}
			cursor = cursorSlot{Revision: rev + 1, Kind: kind, SubjectID: a.SubjectID, CredentialGeneration: a.CredentialGeneration, CollectionRevision: head.Revision, AfterID: records[len(records)-1].ID, ExpiresAt: api.Time(now.Add(5 * time.Minute))}
			if rev == 0 {
				err = tx.Create(ctx, Namespace+".cursors", id, kind, cursor)
			} else {
				err = tx.Put(ctx, Namespace+".cursors", id, rev, cursor)
			}
			if err != nil {
				return err
			}
			next = fmt.Sprintf("%s:%d", id, cursor.Revision)
			return nil
		}
		return api.E("overloaded", "cursor_capacity")
	})
	if status == rt.CommitUnknown {
		return nil, 0, "", false, rt.ErrCommitUnknown
	}
	return records, collectionRevision, next, exhausted, err
}
