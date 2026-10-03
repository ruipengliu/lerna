package execution

import (
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"sync"

	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

type FileWriteArguments struct {
	Path            string         `json:"path"`
	ExpectedVersion string         `json:"expected_version"`
	ContentRef      api.ContentRef `json:"content_ref"`
}
type FileReadArguments struct {
	Path string `json:"path"`
}
type FileWriteResult struct {
	Path            string `json:"path"`
	Version         string `json:"version"`
	DirectorySynced bool   `json:"directory_synced"`
	JournalID       string `json:"journal_id"`
}
type FileReadResult struct {
	Path       string `json:"path"`
	Version    string `json:"version"`
	DataBase64 string `json:"data_base64"`
	ObservedAt string `json:"observed_at"`
}
type fileEncoded struct {
	Path            string `json:"path"`
	ExpectedVersion string `json:"expected_version,omitempty"`
	DataBase64      string `json:"data_base64,omitempty"`
}
type FileDriver struct {
	Files    *ManagedFiles
	Content  domain.ContentPort
	ReadOnly bool
	Location string
	mu       sync.Mutex
	targetMu sync.Mutex
	reads    map[string]domain.Fact
}

func FileWriteCapability() domain.Capability { return fileCapability(false) }
func FileReadCapability() domain.Capability  { return fileCapability(true) }
func fileCapability(read bool) domain.Capability {
	name := "file.write"
	effect := "no_idempotency_guarantee"
	input := api.SchemaFor[FileWriteArguments]()
	output := api.SchemaFor[FileWriteResult]()
	if read {
		name = "file.read"
		effect = "read_only"
		input = api.SchemaFor[FileReadArguments]()
		output = api.SchemaFor[FileReadResult]()
		output["properties"].(map[string]any)["data_base64"] = api.Schema{"type": "string", "maxLength": 174768}
	}
	digest, _ := api.Digest([]any{name, "1", input, output, effect})
	return domain.Capability{Ref: api.ComponentRef{ComponentID: domain.BuiltinComponentID(name), Version: "1", Digest: digest}, EffectClass: effect, MaxAttempts: 1, InputSchema: input, OutputSchema: output}
}
func (d *FileDriver) Capability() domain.Capability { return fileCapability(d.ReadOnly) }
func (d *FileDriver) Prepare(ctx context.Context, sc rt.Scope, a rt.Auth, p domain.InvokeInput, i domain.ExecutionIntent, args []byte) (domain.PreparedRequest, error) {
	var encoded fileEncoded
	if d.ReadOnly {
		var q FileReadArguments
		if err := api.Decode(args, &q); err != nil {
			return domain.PreparedRequest{}, err
		}
		clean, err := normalizeFile(q.Path)
		if err != nil {
			return domain.PreparedRequest{}, err
		}
		encoded.Path = clean
	} else {
		var q FileWriteArguments
		if err := api.Decode(args, &q); err != nil {
			return domain.PreparedRequest{}, err
		}
		clean, err := normalizeFile(q.Path)
		if err != nil {
			return domain.PreparedRequest{}, err
		}
		if d.Content == nil {
			return domain.PreparedRequest{}, api.E("dependency_unavailable", "content_not_configured")
		}
		found := false
		for _, ref := range i.ProcessedSourceRefs {
			found = found || api.Equal(ref, q.ContentRef)
		}
		if !found {
			return domain.PreparedRequest{}, api.E("forbidden", "file_source_not_in_intent")
		}
		b, err := d.Content.ReadBytes(ctx, sc, a, q.ContentRef, "managed_file_write", d.Location)
		if err != nil {
			return domain.PreparedRequest{}, err
		}
		if len(b) > 128<<10 {
			return domain.PreparedRequest{}, api.E("overloaded", "file_payload_too_large")
		}
		encoded = fileEncoded{Path: clean, ExpectedVersion: q.ExpectedVersion, DataBase64: base64.StdEncoding.EncodeToString(b)}
	}
	raw := api.Raw(encoded)
	return domain.PreparedRequest{Encoded: raw, Digest: api.Hash(raw)}, nil
}
func (d *FileDriver) Start(ctx context.Context, q domain.AttemptRequest, barrier func(context.Context) error) (domain.Fact, error) {
	d.targetMu.Lock()
	defer d.targetMu.Unlock()
	var encoded fileEncoded
	if err := api.Decode(q.Attempt.Prepared.Encoded, &encoded); err != nil {
		return domain.Fact{}, err
	}
	if err := barrier(ctx); err != nil {
		return domain.Fact{}, err
	}
	if d.ReadOnly {
		observed, err := d.Files.Read(ctx, encoded.Path)
		if err == nil && len(observed.Data) > 128<<10 {
			err = api.E("overloaded", "file_read_limit")
		}
		if err != nil {
			return domain.Fact{}, err
		}
		f := domain.Fact{Revision: 1, Effect: "not_applied", MayApplyLater: false, Output: api.Raw(FileReadResult{Path: observed.Path, Version: observed.Version, DataBase64: base64.StdEncoding.EncodeToString(observed.Data), ObservedAt: observed.ObservedAt}), MediaType: "application/json", Evidence: []api.ContentRef{}, Usage: []api.Amount{}, UsageFinal: true}
		d.mu.Lock()
		if d.reads == nil {
			d.reads = map[string]domain.Fact{}
		}
		d.reads[q.Attempt.AttemptID] = f
		d.mu.Unlock()
		return f, nil
	}
	data, err := base64.StdEncoding.DecodeString(encoded.DataBase64)
	if err != nil {
		return domain.Fact{}, err
	}
	r, err := d.Files.Write(ctx, FileWrite{OperationID: q.Invoke.OperationID, AttemptID: q.Attempt.AttemptID, Path: encoded.Path, ExpectedVersion: encoded.ExpectedVersion, Data: data})
	if err != nil {
		return domain.Fact{}, err
	}
	return fileFact(encoded.Path, r, q.Attempt.FactRevision), nil
}
func fileFact(p string, r FileReceipt, prior uint64) domain.Fact {
	revision := uint64(1)
	if prior > 0 {
		revision = prior + 1
	}
	return domain.Fact{Revision: revision, Effect: r.Effect, MayApplyLater: r.MayApplyLater, Output: api.Raw(FileWriteResult{Path: p, Version: r.Version, DirectorySynced: r.DirectorySynced, JournalID: r.JournalID}), MediaType: "application/json", Evidence: []api.ContentRef{}, Usage: []api.Amount{}, UsageFinal: !r.MayApplyLater}
}
func (d *FileDriver) Reconcile(ctx context.Context, q domain.AttemptRequest) (domain.Fact, error) {
	d.targetMu.Lock()
	defer d.targetMu.Unlock()
	if d.ReadOnly {
		if q.Attempt.ResultRef != nil && d.Content != nil {
			raw, err := d.Content.ReadBytes(ctx, q.Scope, q.Auth, *q.Attempt.ResultRef, "execution_result", d.Location)
			if err != nil {
				return domain.Fact{}, err
			}
			return domain.Fact{Revision: q.Attempt.FactRevision + 1, Effect: "not_applied", MayApplyLater: false, Output: raw, MediaType: "application/json", Evidence: []api.ContentRef{}, Usage: []api.Amount{}, UsageFinal: true}, nil
		}
		d.mu.Lock()
		f, ok := d.reads[q.Attempt.AttemptID]
		d.mu.Unlock()
		if ok {
			return f, nil
		}
		return domain.Fact{Revision: q.Attempt.FactRevision + 1, Effect: "unknown", MayApplyLater: false, Evidence: []api.ContentRef{}, Usage: []api.Amount{}, UsageFinal: true}, nil
	}
	r, err := d.Files.Recover(ctx, q.Attempt.AttemptID)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.Fact{Revision: q.Attempt.FactRevision + 1, Effect: "not_started", MayApplyLater: false, Evidence: []api.ContentRef{}, Usage: []api.Amount{}, UsageFinal: true}, nil
	}
	if err != nil {
		return domain.Fact{}, err
	}
	var encoded fileEncoded
	if err = api.Decode(q.Attempt.Prepared.Encoded, &encoded); err != nil {
		return domain.Fact{}, err
	}
	return fileFact(encoded.Path, r, q.Attempt.FactRevision), nil
}
func (d *FileDriver) Stop(ctx context.Context, q domain.AttemptRequest) (domain.StopFact, error) {
	d.targetMu.Lock()
	defer d.targetMu.Unlock()
	if d.ReadOnly {
		return domain.StopFact{ActuallyStopped: true, MayApplyLater: false}, nil
	}
	r, err := d.Files.Recover(ctx, q.Attempt.AttemptID)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.StopFact{ActuallyStopped: true, MayApplyLater: false}, nil
	}
	if err != nil {
		return domain.StopFact{}, err
	}
	return domain.StopFact{ActuallyStopped: !r.MayApplyLater, MayApplyLater: r.MayApplyLater}, nil
}

var _ domain.Driver = (*FileDriver)(nil)
