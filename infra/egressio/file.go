//go:build darwin

package egressio

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type FileContent interface {
	BindFileRoot(context.Context, *v1.Caller, *v1.BindFileRootCommand) (*v1.CommandReceipt, error)
	CheckUsable(context.Context, *v1.Caller, *v1.Ref) error
	QueryFileResources(context.Context, *v1.Caller, *v1.Ref) (*v1.FileResources, error)
}

type FileLedger interface {
	QueryFilePublication(context.Context, *v1.Caller, *v1.FileCommit) (*v1.RawObservation, error)
	CheckFileCleanup(context.Context, *v1.Caller, *v1.FileResources) error
}

// Files 持有部署固定的根映射；只接受闸门已经固定的发送和原生发布复查。
type Files struct {
	roots   map[string]string
	ledger  FileLedger
	content FileContent
}

func NewFiles(roots map[string]string, ledger FileLedger, content FileContent) *Files {
	copy := make(map[string]string, len(roots))
	for name, path := range roots {
		copy[name] = path
	}
	return &Files{roots: copy, ledger: ledger, content: content}
}
func (f *Files) Perform(context.Context, *v1.PhysicalIORequest) (*v1.PhysicalIOResult, error) {
	return nil, command.Fail("FILE_USE_GUARD_REQUIRED")
}
func (f *Files) PerformChecked(ctx context.Context, c *v1.PhysicalIORequest, check func(context.Context) error) (*v1.PhysicalIOResult, error) {
	d := c.GetCallDescriptor()
	if d == nil || d.Protocol != "FILE" || c.Send == nil || c.Send.Phase != "DISPATCH_POSSIBLE" || c.Attempt == nil || c.OperationId == nil || c.FileParameters == nil || check == nil {
		return nil, command.Fail("INVALID_IO_INTENT")
	}
	u, e := url.Parse(d.Target)
	if e != nil || u.Scheme != "managed" || u.Host == "" || u.User != nil || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" || !fileName(u.Host) || !fileName(strings.TrimPrefix(u.Path, "/")) {
		return nil, command.Fail("TARGET_SCOPE_MISMATCH")
	}
	rootPath, ok := f.roots[u.Host]
	if !ok {
		return nil, command.Fail("FILE_ROOT_UNAVAILABLE")
	}
	ev := &v1.FileEvidence{Rule: "managed-file-v1", Stage: "ADMITTED", Terminal: true, BillingRule: "managed-file-zero-v1"}
	raw := &v1.RawObservation{Ref: c.Send.ObservationRef, UserId: c.OperationId.UserId, TaskId: c.TaskId, OperationId: c.OperationId, AttemptId: c.Attempt.Ref.Name, SendRef: c.Send.Ref, SendSeq: c.Send.SendSeq, Target: d.Target, ExecutorEndpointId: c.ExecutorEndpointId, Protocol: "FILE", StartedAtUnixMs: time.Now().UnixMilli(), ExternalKey: c.Attempt.ExternalKey, Source: "TRUSTED_IO", QuerySubject: d.QuerySubject, FileEvidence: ev}
	result := &v1.PhysicalIOResult{Observation: raw}
	finish := func(err error) (*v1.PhysicalIOResult, error) {
		raw.FinishedAtUnixMs = time.Now().UnixMilli()
		if err != nil {
			ev.ErrorCode = fileError(err)
			ev.NegativeProof = !ev.Published && (d.Method == "CREATE" || d.Method == "REPLACE")
		} else {
			raw.StatusCode = 200
		}
		return result, nil
	}
	if e = check(ctx); e != nil {
		return finish(e)
	}
	root, e := openNativeRoot(ctx, rootPath)
	if e != nil {
		return finish(e)
	}
	defer root.close()
	ev.Platform = root.platform
	raw.ActualAddress = root.identity
	if f.content == nil {
		return finish(command.Fail("DEPENDENCY_UNAVAILABLE"))
	}
	actor := &v1.Caller{UserId: c.OperationId.UserId, IssuerId: "egress-io"}
	header := &v1.CommandHeader{Identity: &v1.CommandIdentity{UserId: actor.UserId, IssuerId: actor.IssuerId, TargetDomainId: d.ParametersRef.Name.AuthorityDomainId, CommandId: "file-root:" + c.Send.Ref.Name.LocalId}, ContractVersion: 1, FingerprintVersion: 1, SchemaId: "lerna.v1.AdmissionCommands"}
	bound, e := f.content.BindFileRoot(ctx, actor, &v1.BindFileRootCommand{Header: header, OperationId: c.OperationId, SendRef: c.Send.Ref, RootId: u.Host, NativeIdentity: root.identity, Platform: root.platform})
	if e != nil {
		return finish(e)
	}
	if bound == nil || bound.Decision != v1.Decision_DECISION_ACCEPTED {
		if bound != nil && bound.Error != nil {
			return finish(&command.Failure{Detail: bound.Error})
		}
		return finish(command.Fail("FILE_ROOT_UNAVAILABLE"))
	}
	target := strings.TrimPrefix(u.Path, "/")
	release, e := root.lock(target)
	if e != nil {
		return finish(e)
	}
	defer release()
	if e = check(ctx); e != nil {
		return finish(e)
	}
	current, e := readFileCommit(root, target)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return finish(e)
	}
	if current != nil && !fileCommitScope(current, d.Target, c.OperationId.UserId) {
		return finish(command.Fail("FILE_COMMIT_INVALID"))
	}
	if current != nil {
		if e = f.checkCommit(ctx, c.OperationId.UserId, current); e != nil {
			return finish(e)
		}
	}
	switch d.Method {
	case "READ":
		ev.ReadTerminal = true
		if current == nil {
			return finish(command.Fail("FILE_NOT_FOUND"))
		}
		if c.FileParameters.ExpectedVersion != "" && c.FileParameters.ExpectedVersion != current.Version {
			return finish(command.Fail("FILE_VERSION_CONFLICT"))
		}
		result.Body, e = readFileObject(root, current, "read.object")
		if e != nil {
			return finish(e)
		}
		ev.Commit = current
		ev.ReadbackVerified = true
		return finish(nil)
	case "QUERY":
		return f.queryFile(ctx, c, root, current, result, finish)
	case "CLEANUP":
		resources := c.FileResources
		if resources == nil || resources.Target != d.Target || !proto.Equal(resources.Ref, c.FileParameters.CleanupResourcesRef) || !fileName(resources.ObjectName) || !fileName(resources.PointerName) {
			return finish(command.Fail("INVALID_FILE_RESOURCES"))
		}
		ev.ResourcesRef = resources.Ref
		if current != nil && (proto.Equal(current.ResourcesRef, resources.Ref) || current.ObjectName == resources.ObjectName) {
			return finish(command.Fail("FILE_RESOURCE_IN_USE"))
		}
		if f.ledger == nil {
			return finish(command.Fail("DEPENDENCY_UNAVAILABLE"))
		}
		if e = f.ledger.CheckFileCleanup(ctx, &v1.Caller{UserId: c.OperationId.UserId, IssuerId: "egress-io"}, resources); e != nil {
			return finish(e)
		}
		if e = check(ctx); e != nil {
			return finish(e)
		}
		if e = root.remove("objects", resources.ObjectName, resources.ObjectIdentity, check); e != nil {
			return finish(e)
		}
		if e = root.remove("commits", resources.PointerName, resources.PointerIdentity, check); e != nil {
			return finish(e)
		}
		if e = root.sync(root.dirs["objects"], "objects", "", "cleanup.objects.sync", true); e != nil {
			return finish(e)
		}
		if e = root.sync(root.dirs["commits"], "commits", "", "cleanup.commits.sync", true); e != nil {
			return finish(e)
		}
		if e = root.absent("objects", resources.ObjectName); e != nil {
			return finish(e)
		}
		if e = root.absent("commits", resources.PointerName); e != nil {
			return finish(e)
		}
		ev.Stage = "CLEANED"
		ev.DurabilityConfirmed = true
		ev.ReadbackVerified = true
		return finish(nil)
	case "CREATE", "REPLACE":
	default:
		return finish(command.Fail("UNSUPPORTED_CAPABILITY"))
	}
	resources := c.FileResources
	if resources == nil || !proto.Equal(resources.SendRef, c.Send.Ref) || !proto.Equal(resources.OperationId, c.OperationId) || !proto.Equal(resources.AttemptId, c.Attempt.Ref.Name) || resources.Target != d.Target || resources.Digest != fmt.Sprintf("%x", sha256.Sum256(c.Body)) || resources.ByteSize != uint64(len(c.Body)) || !fileName(resources.ObjectName) || !fileName(resources.PointerName) {
		return finish(command.Fail("INVALID_FILE_RESOURCES"))
	}
	ev.ResourcesRef = resources.Ref
	if d.Method == "CREATE" && current != nil || d.Method == "REPLACE" && (current == nil || current.Version != c.FileParameters.ExpectedVersion) {
		return finish(command.Fail("FILE_VERSION_CONFLICT"))
	}
	if current != nil {
		if f.ledger == nil {
			return finish(command.Fail("FILE_PREDECESSOR_UNACCEPTED"))
		}
		accepted, e := f.ledger.QueryFilePublication(ctx, &v1.Caller{UserId: c.OperationId.UserId, IssuerId: "egress-io"}, current)
		if e != nil {
			return finish(e)
		}
		if accepted == nil {
			return finish(command.Fail("FILE_PREDECESSOR_UNACCEPTED"))
		}
	}
	commit := &v1.FileCommit{Version: strings.TrimSuffix(resources.ObjectName, ".data"), Target: d.Target, OperationId: c.OperationId, AttemptId: c.Attempt.Ref.Name, SendRef: c.Send.Ref, ExternalKey: c.Attempt.ExternalKey, ObjectName: resources.ObjectName, Digest: resources.Digest, ByteSize: resources.ByteSize, ContentRef: resources.ContentRef, ResourcesRef: resources.Ref, PreviousVersion: c.FileParameters.ExpectedVersion, RootIdentity: root.identity}
	ev.Commit = commit
	object, e := root.create("objects", resources.ObjectName, "object.create")
	if e != nil {
		return finish(e)
	}
	defer object.Close()
	ev.ObjectIdentity, e = fileIdentity(object)
	if e != nil {
		return finish(e)
	}
	ev.Stage = "OBJECT_CREATED"
	if e = root.write(object, "objects", resources.ObjectName, "object.write", c.Body); e != nil {
		return finish(e)
	}
	ev.Stage = "OBJECT_WRITTEN"
	if e = root.sync(object, "objects", resources.ObjectName, "object.sync", false); e != nil {
		return finish(e)
	}
	if e = root.sync(root.dirs["objects"], "objects", "", "objects.sync", true); e != nil {
		return finish(e)
	}
	ev.Stage = "OBJECT_DURABLE"
	pointerBody, e := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(commit)
	if e != nil {
		return finish(e)
	}
	pointer, e := root.create("commits", resources.PointerName, "pointer.create")
	if e != nil {
		return finish(e)
	}
	defer pointer.Close()
	ev.PointerIdentity, e = fileIdentity(pointer)
	if e != nil {
		return finish(e)
	}
	if e = root.write(pointer, "commits", resources.PointerName, "pointer.write", pointerBody); e != nil {
		return finish(e)
	}
	if e = root.sync(pointer, "commits", resources.PointerName, "pointer.sync", false); e != nil {
		return finish(e)
	}
	ev.Stage = "POINTER_PREPARED"
	if e = nativeFileBefore(ctx, "publish.check"); e != nil {
		return finish(e)
	}
	if e = check(ctx); e != nil {
		return finish(e)
	}
	if e = root.rename(resources.PointerName, target, check); e != nil {
		return finish(e)
	}
	ev.Published = true
	ev.Stage = "PUBLISHED"
	if e = root.sync(root.dirs["commits"], "commits", "", "commits.sync", true); e != nil {
		return finish(e)
	}
	ev.DurabilityConfirmed = true
	ev.Stage = "DURABLE"
	observed, e := readFileCommit(root, target)
	if e != nil {
		return finish(e)
	}
	if !proto.Equal(observed, commit) {
		return finish(command.Fail("FILE_READBACK_MISMATCH"))
	}
	result.Body, e = readFileObject(root, commit, "readback.object")
	if e != nil {
		return finish(e)
	}
	ev.ReadbackVerified = true
	ev.Stage = "VERIFIED"
	return finish(nil)
}
func fileName(s string) bool {
	if s == "" || s == "." || s == ".." || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' && c != '.' {
			return false
		}
	}
	return true
}
func fileError(err error) string {
	var e *command.Failure
	if errors.As(err, &e) {
		return e.Detail.Code
	}
	switch {
	case errors.Is(err, syscall.ENOSPC):
		return "FILE_NO_SPACE"
	case errors.Is(err, syscall.EXDEV):
		return "FILE_CROSS_DEVICE"
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return "FILE_ACCESS_DENIED"
	case errors.Is(err, syscall.ENOENT):
		return "FILE_NOT_FOUND"
	case errors.Is(err, syscall.EEXIST):
		return "FILE_EXISTS"
	case errors.Is(err, syscall.ELOOP):
		return "FILE_SYMLINK_REJECTED"
	default:
		return "FILE_NATIVE_IO_FAILED"
	}
}
func readFileCommit(root *nativeRoot, target string) (*v1.FileCommit, error) {
	data, e := root.read("commits", target, "read.pointer")
	if e != nil {
		return nil, e
	}
	commit := new(v1.FileCommit)
	if protojson.Unmarshal(data, commit) != nil || commit.Version == "" || commit.OperationId == nil || commit.AttemptId == nil || commit.SendRef == nil || commit.ContentRef == nil || commit.ResourcesRef == nil || commit.ExternalKey == "" || !fileName(commit.ObjectName) || len(commit.Digest) != 64 || commit.RootIdentity != root.identity {
		return nil, command.Fail("FILE_COMMIT_INVALID")
	}
	return commit, nil
}
func readFileObject(root *nativeRoot, c *v1.FileCommit, stage string) ([]byte, error) {
	data, e := root.read("objects", c.ObjectName, stage)
	if e != nil {
		return nil, e
	}
	if uint64(len(data)) != c.ByteSize || fmt.Sprintf("%x", sha256.Sum256(data)) != c.Digest {
		return nil, command.Fail("FILE_READBACK_MISMATCH")
	}
	return data, nil
}
func (f *Files) queryFile(ctx context.Context, c *v1.PhysicalIORequest, root *nativeRoot, current *v1.FileCommit, result *v1.PhysicalIOResult, finish func(error) (*v1.PhysicalIOResult, error)) (*v1.PhysicalIOResult, error) {
	subject := c.CallDescriptor.QuerySubject
	ev := result.Observation.FileEvidence
	if subject == nil || subject.TargetScope != c.CallDescriptor.Target || subject.ExecutorEndpointId != c.ExecutorEndpointId {
		return finish(command.Fail("INVALID_QUERY_SUBJECT"))
	}
	ev.ReadTerminal = true
	if current != nil && proto.Equal(current.OperationId, subject.OperationId) && proto.Equal(current.AttemptId, subject.AttemptId) && current.ExternalKey == subject.ExternalKey && current.Target == subject.TargetScope {
		data, e := readFileObject(root, current, "query.object")
		if e != nil {
			return finish(e)
		}
		result.Body = data
		ev.Commit = current
		ev.Published = true
		ev.ReadbackVerified = true
		ev.Stage = "OBSERVED_CURRENT"
		if f.ledger != nil {
			accepted, e := f.ledger.QueryFilePublication(ctx, &v1.Caller{UserId: c.OperationId.UserId, IssuerId: "egress-io"}, current)
			if e != nil {
				return finish(e)
			}
			if accepted != nil {
				ev.DurabilityConfirmed = accepted.FileEvidence.DurabilityConfirmed
				ev.AcceptedObservationRef = accepted.Ref
			}
		}
		return finish(nil)
	}
	// 当前目标不是原提交时，只向事实负责方查既有证据，绝不根据孤立对象判断发布。
	if f.ledger != nil {
		original := &v1.FileCommit{OperationId: subject.OperationId, AttemptId: subject.AttemptId, ExternalKey: subject.ExternalKey, Target: subject.TargetScope}
		accepted, e := f.ledger.QueryFilePublication(ctx, &v1.Caller{UserId: c.OperationId.UserId, IssuerId: "egress-io"}, original)
		if e != nil {
			return finish(e)
		}
		if accepted != nil {
			previous := accepted.FileEvidence
			ev.Commit = previous.Commit
			ev.Published = previous.Published
			ev.DurabilityConfirmed = previous.DurabilityConfirmed
			ev.ReadbackVerified = previous.ReadbackVerified
			ev.AcceptedObservationRef = accepted.Ref
			ev.Stage = "ACCEPTED_HISTORY"
			if !fileCommitScope(ev.Commit, c.CallDescriptor.Target, c.OperationId.UserId) || ev.Commit.RootIdentity != root.identity {
				return finish(command.Fail("FILE_COMMIT_INVALID"))
			}
			if e = f.checkCommit(ctx, c.OperationId.UserId, ev.Commit); e != nil {
				return finish(e)
			}
			if !ev.ReadbackVerified {
				result.Body, e = readFileObject(root, ev.Commit, "query.history.object")
				if e != nil {
					return finish(e)
				}
				ev.ReadbackVerified = true
			}

		}
	}
	return finish(nil)
}

func fileCommitScope(c *v1.FileCommit, target, user string) bool {
	if c.Target != target || c.Version+".data" != c.ObjectName || c.OperationId.GetUserId() != user || c.AttemptId.GetUserId() != user {
		return false
	}
	for _, ref := range []*v1.Ref{c.SendRef, c.ContentRef, c.ResourcesRef} {
		if ref.GetName().GetUserId() != user || ref.Revision == 0 {
			return false
		}
	}
	return c.OperationId.ObjectKind == "operation" && c.AttemptId.ObjectKind == "attempt" && c.SendRef.Name.ObjectKind == "send" && c.ContentRef.Name.ObjectKind == "content" && c.ResourcesRef.Name.ObjectKind == "file-resources"
}

func (f *Files) checkCommit(ctx context.Context, user string, c *v1.FileCommit) error {
	if f.content == nil {
		return command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	actor := &v1.Caller{UserId: user, IssuerId: "egress-io"}
	r, e := f.content.QueryFileResources(ctx, actor, c.ResourcesRef)
	if e != nil {
		return e
	}
	if r == nil || r.Target != c.Target || r.ObjectName != c.ObjectName || r.Digest != c.Digest || r.ByteSize != c.ByteSize || !proto.Equal(r.ContentRef, c.ContentRef) || !proto.Equal(r.OperationId, c.OperationId) || !proto.Equal(r.AttemptId, c.AttemptId) || !proto.Equal(r.SendRef, c.SendRef) {
		return command.Fail("FILE_COMMIT_INVALID")
	}
	return f.content.CheckUsable(ctx, actor, c.ContentRef)
}
