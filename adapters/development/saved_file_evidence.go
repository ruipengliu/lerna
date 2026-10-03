package development

import (
	"context"
	"encoding/base64"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 设备保存条件只采用原准确 write/read 的独立目标依据，不读取云端同名路径。
// 普通 Content 当前门禁仍逐份核验；此记录不代替 DataUse 或原设备准入。
type savedDeviceBasis struct {
	ExecutorID    string         `json:"executor_id"`
	DatabaseID    string         `json:"database_id"`
	InstanceID    string         `json:"instance_id"`
	RouteDigest   string         `json:"route_digest"`
	WriteRef      api.ObjectRef  `json:"write_ref"`
	ReadRef       api.ObjectRef  `json:"read_ref"`
	WriteAttempt  api.ObjectRef  `json:"write_attempt"`
	ReadAttempt   api.ObjectRef  `json:"read_attempt"`
	WriteResult   api.ContentRef `json:"write_result"`
	ReadResult    api.ContentRef `json:"read_result"`
	Path          string         `json:"path"`
	Version       string         `json:"version"`
	ReadObserved  string         `json:"read_observed"`
	WriteObserved string         `json:"write_observed"`
}

type savedDeviceObservation struct {
	Handled    bool
	Valid      bool
	ObservedAt time.Time
	Evidence   []api.ContentRef
	Basis      *savedDeviceBasis
}

func (a *App) savedDeviceEvidence(ctx context.Context, scope runtime.Scope, t api.Task, params brain.RuleParameters, facts task.ContextFacts) (savedDeviceObservation, error) {
	var out savedDeviceObservation
	var write, read *task.ContextOperation
	ambiguous := false
	for i := range facts.Operations {
		op := &facts.Operations[i]
		if op.Intent.GoalRevision != t.GoalRevision || op.Intent.ControlRevision != t.ControlRevision {
			continue
		}
		switch op.Intent.LogicalStepKey {
		case t.TaskID + "/save_report":
			ambiguous = ambiguous || write != nil
			write = op
		case t.TaskID + "/verify_file":
			ambiguous = ambiguous || read != nil
			read = op
		default:
			continue
		}
		out.Handled = out.Handled || op.Intent.ExecutorID != scope.OwnerID
	}
	if !out.Handled || ambiguous || write == nil || read == nil {
		return out, nil
	}
	if facts.Task.TaskID != t.TaskID || facts.Task.GoalRevision != t.GoalRevision || facts.Task.ControlRevision != t.ControlRevision || write.Intent.ExecutorID == scope.OwnerID || write.Intent.ExecutorID != read.Intent.ExecutorID || write.Intent.OperationID == read.Intent.OperationID || !api.Equal(write.Intent.CapabilityRef, target.FileWriteCapability().Ref) || !api.Equal(read.Intent.CapabilityRef, target.FileReadCapability().Ref) {
		return out, nil
	}
	views := make([]execution.OperationView, 0, 2)
	refs := []api.ContentRef{}
	var originalRoute *remoteExecutor
	for n, original := range []*task.ContextOperation{write, read} {
		i := original.Intent
		// 原Task资源键由可信registry以设备owner限定；物理参数仍逐份核准确路径。
		if i.TaskRef.TenantID != scope.TenantID || i.TaskRef.OwnerID != scope.OwnerID || i.TaskRef.ObjectID != t.TaskID || i.BindingRef.OwnerID != i.ExecutorID || i.TaskRef.Revision > t.Revision || !api.Equal(i.ResourceKeys, []string{"executor:" + i.ExecutorID + "/file:" + params.SavePath}) || !original.Fact.Closed || original.Fact.MayApplyLater || original.Fact.Effect == "unknown" {
			return out, nil
		}
		route, err := a.remoteExecutors.binding(i.BindingRef, i.CapabilityRef, i.InstallLockRef)
		if err != nil {
			return out, err
		}
		var admission actionAdmission
		if _, err = a.Store.Read(ctx, scope, "platform.action_admissions", i.OperationID, 1, &admission); err != nil {
			return out, err
		}
		action := "file.read"
		if n == 0 {
			action = "file.write"
		}
		if admission.Scope != scope || !api.Equal(admission.Prepared, i.PreparedAction) || admission.Descriptor.RemoteConfigHash != route.Digest || admission.Descriptor.ExecutorID != i.ExecutorID || admission.Descriptor.Recipient != i.ExecutorID || admission.Descriptor.Location != "device" || !api.Equal(admission.Resources, []string{"managed-files"}) || !api.Equal(admission.Actions, []string{action}) {
			return out, api.E("forbidden", "original_saved_device_admission_changed")
		}
		if originalRoute != nil && (originalRoute.Digest != route.Digest || originalRoute.Config.DatabaseID != route.Config.DatabaseID || originalRoute.Config.InstanceID != route.Config.InstanceID) {
			return out, nil
		}
		route, err = a.remoteExecutors.client(ctx, i.ExecutorID)
		if err != nil {
			return out, err
		}
		if err = (executionBridge{a}).remoteAdmissionReady(ctx, scope, i, route); err != nil {
			return out, err
		}
		view, err := route.Client.Get(ctx, i.OperationID)
		if err != nil {
			return out, err
		}
		if view.Operation.OwnerID != i.ExecutorID || view.Operation.OperationID != i.OperationID || !api.Equal(view.Operation.TaskRef, i.TaskRef) || view.Operation.ResultRef == nil || view.Operation.ResultRef.TenantID != scope.TenantID || view.Operation.ResultRef.OwnerID != i.ExecutorID || !view.NewAttemptsClosed || !view.ActuallyStopped || !view.Operation.UsageFinal || view.EffectDisputed || len(view.Attempts.Items) != 1 || !view.Attempts.Exhausted || view.Attempts.Partial || len(view.Attempts.Gaps) != 0 {
			return out, nil
		}
		attempt := view.Attempts.Items[0]
		if attempt.OperationID != i.OperationID || attempt.StartedAt == "" || !attempt.ActuallyStopped || !attempt.UsageFinal || attempt.ResultRef == nil || !api.Equal(*attempt.ResultRef, *view.Operation.ResultRef) || n == 0 && (view.Operation.Effect != "applied" || attempt.Effect != "applied") || n == 1 && (view.Operation.Effect == "not_started" || view.Operation.Effect == "unknown" || attempt.Effect == "not_started" || attempt.Effect == "unknown") {
			return out, nil
		}
		views = append(views, view)
		refs = append(refs, i.ArgumentsRef, *view.Operation.ResultRef)
		originalRoute = route
	}
	// 准确新 Job 的当前证明只覆盖这两份原参数/结果，不能复用旧 carrier。
	ctx, err := a.prepareForeignSources(ctx, scope, a.ServiceAuth, refs, "task.context", "cloud")
	if err != nil {
		return out, err
	}
	bodies := make([][]byte, 0, len(refs))
	for _, ref := range refs {
		bytes, err := a.ReadContentBytes(ctx, scope, a.ServiceAuth, ref, "task.context", "cloud")
		if err != nil {
			return out, err
		}
		bodies = append(bodies, bytes)
	}
	var writeArgs target.FileWriteArguments
	var writeResult target.FileWriteResult
	var readArgs target.FileReadArguments
	var readResult target.FileReadResult
	for n, into := range []any{&writeArgs, &writeResult, &readArgs, &readResult} {
		if err = api.Decode(bodies[n], into); err != nil {
			return out, err
		}
	}
	wa, ra := views[0].Attempts.Items[0], views[1].Attempts.Items[0]
	writeObserved, err := api.ParseTime(wa.ObservedAt)
	if err != nil {
		return out, err
	}
	readStarted, err := api.ParseTime(ra.StartedAt)
	if err != nil {
		return out, err
	}
	readObserved, err := api.ParseTime(readResult.ObservedAt)
	if err != nil {
		return out, err
	}
	bytes, err := base64.StdEncoding.Strict().DecodeString(readResult.DataBase64)
	if err != nil {
		return out, err
	}
	includesWrite := false
	for _, ref := range read.Intent.ProcessedSourceRefs {
		includesWrite = includesWrite || api.Equal(ref, *views[0].Operation.ResultRef)
	}
	out.Evidence = uniqueSources(refs)
	out.ObservedAt = readObserved
	out.Valid = writeArgs.Path == params.SavePath && readArgs.Path == params.SavePath && writeResult.Path == params.SavePath && readResult.Path == params.SavePath && writeArgs.ContentRef.Hash == params.ExpectedHash && writeArgs.ContentRef.ByteLength == params.ExpectedLength && writeResult.DirectorySynced && writeResult.JournalID == wa.AttemptID && writeResult.Version == params.ExpectedHash && readResult.Version == writeResult.Version && wa.AttemptID != ra.AttemptID && !readStarted.Before(writeObserved) && !readObserved.Before(readStarted) && includesWrite && api.Hash(bytes) == params.ExpectedHash && uint64(len(bytes)) == params.ExpectedLength
	out.Basis = &savedDeviceBasis{ExecutorID: write.Intent.ExecutorID, DatabaseID: originalRoute.Config.DatabaseID, InstanceID: originalRoute.Config.InstanceID, RouteDigest: originalRoute.Digest, WriteRef: write.Fact.Ref, ReadRef: read.Fact.Ref, WriteAttempt: api.ObjectRef{TenantID: scope.TenantID, OwnerID: write.Intent.ExecutorID, ObjectID: wa.AttemptID, Revision: wa.Revision}, ReadAttempt: api.ObjectRef{TenantID: scope.TenantID, OwnerID: read.Intent.ExecutorID, ObjectID: ra.AttemptID, Revision: ra.Revision}, WriteResult: *views[0].Operation.ResultRef, ReadResult: *views[1].Operation.ResultRef, Path: params.SavePath, Version: readResult.Version, ReadObserved: readResult.ObservedAt, WriteObserved: wa.ObservedAt}
	return out, nil
}
