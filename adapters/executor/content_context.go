package executor

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/runtime"
)

type deviceContentEntryKey struct{}
type deviceContentEntry struct {
	scope       runtime.Scope
	operationID string
	accounting  bool
}

// 工厂只投影本次已领取 Job/已核封套的原操作路由；它不读库、不取证明或授予用途。
// 每次入口覆写载体，后续 ReadBytes 仍强读该唯一原 admission 和当前已知撤权。
func (h *Host) contentEntry(ctx context.Context, flow runtime.Flow) context.Context {
	entry := deviceContentEntry{scope: flow.Scope}
	if flow.Scope == h.Scope {
		if flow.Kind == "job" && flow.Work != nil && (flow.Work.Job.Kind == execution.RunJob || flow.Work.Job.Kind == execution.ReconcileJob) {
			ref := flow.Work.Job.SourceRef
			if ref.TenantID == h.Scope.TenantID && ref.OwnerID == h.Scope.OwnerID && ref.Revision > 0 && api.ValidID(ref.ObjectID) {
				entry.operationID = ref.ObjectID
			}
		} else if flow.Kind == "query" && flow.Query != nil && flow.Query.Method == "execution.usage.get" && api.ValidID(flow.Query.TargetID) {
			entry.operationID = flow.Query.TargetID
			entry.accounting = true
		}
	}
	return context.WithValue(ctx, deviceContentEntryKey{}, entry)
}

func (c deviceContent) inputBytes(ctx context.Context, s runtime.Scope, auth runtime.Auth, ref api.ContentRef, purpose string, entry deviceContentEntry) (contentRecord, error) {
	var rec contentRecord
	if entry.scope != s || entry.operationID == "" || entry.accounting && purpose != "execution_intent" {
		return rec, api.E("forbidden", "original_execution_content_entry_required")
	}
	status, err := c.h.Store.Within(ctx, s, []string{Namespace}, func(tx runtime.Tx) error {
		var original admissionRecord
		if _, err := tx.Get(ctx, Namespace+".admissions", entry.operationID, &original); err != nil {
			return err
		}
		b := original.Bundle
		if b.Intent.OperationID != entry.operationID || !api.Equal(b.Principal, PrincipalOf(auth)) {
			return api.E("forbidden", "original_content_permission_mismatch")
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		// 来源签名核原固定内容；当前启动期限在普通输入和最终 VerifyStart 分别重核。
		if err = c.h.verifyBundle(b, now, false); err != nil {
			return err
		}
		digest, err := BundleDigest(b)
		if err != nil {
			return err
		}
		if digest != original.Digest {
			return api.E("forbidden", "original_admission_digest_mismatch")
		}
		permission, ok := permissionFor(b, ref)
		if !ok || !has(permission.Purposes, purpose) || purpose == "execution_intent" && !api.Equal(ref, b.IntentRef) || purpose == "execution_arguments" && !api.Equal(ref, b.Intent.ArgumentsRef) {
			return api.E("forbidden", "original_content_purpose_not_allowed")
		}
		if _, err = tx.Get(ctx, Namespace+".contents", contentKey(ref), &rec); err != nil {
			return err
		}
		if !rec.Complete || !api.Equal(rec.Permission.ContentRef, ref) || !api.Equal(rec.Permission.ProcessedSources, permission.ProcessedSources) || !api.Equal(rec.Permission.DisclosedSources, permission.DisclosedSources) {
			return api.E("forbidden", "original_content_permission_mismatch")
		}
		if err = before(now, earliest(rec.Permission.RetainUntil, permission.RetainUntil)); err != nil {
			return err
		}
		if entry.accounting {
			// 原账务只恢复已固定 intent 依据；不取参数/正文，不消费新 Use 或恢复已撤权行动。
			return nil
		}
		if err = before(now, b.StartBefore); err != nil {
			return err
		}
		if err = c.h.checkDenyTx(ctx, tx, b); err != nil {
			return err
		}
		policy, subjects := rec.Permission.SourcePolicy, rec.Permission.SubjectRefs
		if rec.Published {
			policy, subjects = rec.SourcePolicy, rec.SourceReaders
			if rec.SourceState != "published" {
				return api.E("forbidden", "source_closed")
			}
		}
		if permission.SourcePolicy != nil {
			if !api.Equal(policy, permission.SourcePolicy) || !api.Equal(subjects, permission.SubjectRefs) || policy.State != "active" {
				return api.E("forbidden", "original_cached_policy_changed")
			}
			if err = before(now, policy.Values.RetainUntil); err != nil {
				return err
			}
			for _, subject := range permission.SubjectRefs {
				if err = c.h.knownDenyTx(ctx, tx, subject); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if status == runtime.CommitUnknown {
		return rec, runtime.ErrCommitUnknown
	}
	return rec, err
}
