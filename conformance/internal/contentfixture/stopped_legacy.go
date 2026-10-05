package contentfixture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	v "github.com/ruipengliu/lerna/contract/v1_2"
	d "github.com/ruipengliu/lerna/domain/content"
)

const stoppedWriterSHA = "1a7d910238eb74cddc712d92b0ba4014a72ff507"

type LegacyPutEffect struct {
	Key         string `json:"key"`
	Attempt     string `json:"attempt"`
	Hash        string `json:"hash"`
	Length      int64  `json:"length"`
	InputSHA256 string `json:"input_sha256"`
}
type StoppedLegacyObservation struct {
	ScopeID           string                       `json:"scope_id"`
	Schema            string                       `json:"schema"`
	WriterSHA         string                       `json:"writer_sha"`
	Binding           string                       `json:"binding"`
	Requests          []v.ContentPutRequest        `json:"requests"`
	Receipts          []v.CommandReceipt           `json:"receipts"`
	States            []string                     `json:"states"`
	Attempts          map[string][]LegacyPutEffect `json:"attempts"`
	ObjectFDs         []int                        `json:"object_fds"`
	MigrationChecksum string                       `json:"migration_checksum"`
}
type stoppedLegacyFrame struct {
	Phase             string                   `json:"phase"`
	Observation       StoppedLegacyObservation `json:"observation"`
	ObjectCloseACK    bool                     `json:"object_close_ack"`
	StoreCloseACK     bool                     `json:"store_close_ack"`
	WitnessCloseACK   bool                     `json:"witness_close_ack"`
	StartGateCloseACK bool                     `json:"start_gate_close_ack"`
	StopGateCloseACK  bool                     `json:"stop_gate_close_ack"`
	ObjectFDsClosed   bool                     `json:"object_fds_closed"`
	Failed            bool                     `json:"failed"`
}

func legacyPipeWrite(ctx context.Context, file *os.File) error {
	fd := int(file.Fd())
	if err := syscall.SetNonblock(fd, true); err != nil {
		return err
	}
	for ctx.Err() == nil {
		n, err := syscall.Write(fd, []byte{1})
		if n == 1 && err == nil {
			return nil
		}
		if err != syscall.EAGAIN && err != syscall.EWOULDBLOCK && err != syscall.EINTR {
			return errors.Join(err, errors.New("effect gate write incomplete"))
		}
		time.Sleep(time.Millisecond)
	}
	return ctx.Err()
}
func legacyPipeFrame(ctx context.Context, file *os.File) (stoppedLegacyFrame, error) {
	var result stoppedLegacyFrame
	fd := int(file.Fd())
	if err := syscall.SetNonblock(fd, true); err != nil {
		return result, err
	}
	var body []byte
	var next [1]byte
	for ctx.Err() == nil && len(body) < 16384 {
		n, err := syscall.Read(fd, next[:])
		if n == 1 {
			if next[0] == '\n' {
				return result, json.Unmarshal(body, &result)
			}
			body = append(body, next[0])
			continue
		}
		if err == nil {
			return result, errors.New("producer frame EOF")
		}
		if err != syscall.EAGAIN && err != syscall.EWOULDBLOCK && err != syscall.EINTR {
			return result, err
		}
		time.Sleep(time.Millisecond)
	}
	return result, errors.Join(ctx.Err(), errors.New("finite producer frame incomplete"))
}
func legacyActualStart(pid int) (string, error) {
	file, err := os.Open(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return "", err
	}
	var bytes [4096]byte
	n, readErr := file.Read(bytes[:])
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || n == len(bytes) {
		return "", errors.Join(readErr, closeErr, errors.New("finite child identity incomplete"))
	}
	text := string(bytes[:n])
	delimiter := strings.LastIndex(text, ")")
	if delimiter < 0 {
		return "", errors.New("child identity malformed")
	}
	fields := strings.Fields(text[delimiter+1:])
	if len(fields) < 20 {
		return "", errors.New("child identity missing starttime")
	}
	return fields[19], nil
}

// NewStoppedLegacy uses a fresh original scope, never an imported body copy.
// The callback runs after old normal publication, while the original writer
// is still held at its stop gate. Current migrations start only after the
// explicit old logical Close ACKs and independent actual Wait/group absence.
func NewStoppedLegacy(t *testing.T, ctx context.Context, whileOldWriterHeld func(*World, StoppedLegacyObservation)) (*World, StoppedLegacyObservation, d.LegacyPrimaryQualification) {
	t.Helper()
	var observation StoppedLegacyObservation
	var qualification d.LegacyPrimaryQualification
	w := newWorld(t, ctx, func(w *World) {
		binary, err := w.buildFrozenContent(ctx)
		if err != nil {
			t.Fatal(err)
		}
		childCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		binding := fmt.Sprintf("linux-directory:%d:%d", w.device, w.inode)
		scopeID := "ticket05-stopped-" + w.Config.Schema
		owner := v.OwnerRef{TenantID: "content-tenant", OwnerID: "content-owner"}
		subject := v.SubjectBinding{TenantID: owner.TenantID, SubjectID: "verified-subject", DelegationChain: []v.DelegatedSubject{}}
		refs := []v.ContentRef{
			{Owner: owner, ContentID: "stopped-legacy", Version: "1", Hash: "sha256:b6a98d9ce9a2d9149288fa3df42d377c3e42737afdcdaf714e33c0a100b51060", MediaType: "text/plain", ByteLength: "6"},
			{Owner: owner, ContentID: "stopped-legacy", Version: "2", Hash: "sha256:f2c82decdd7181cf98945929a62598db7e6b477e11f6e0eb0ae97020eff151ad", MediaType: "text/plain", ByteLength: "5"},
		}
		versions := []d.LegacyPrimaryVersion{}
		for _, ref := range refs {
			_, key, err := d.VersionIdentity(ref)
			if err != nil {
				t.Fatal(err)
			}
			versions = append(versions, d.LegacyPrimaryVersion{Ref: ref, Subject: subject, Purpose: "verification", ObjectKey: key})
		}
		ledger := func(value any) error {
			bytes, err := json.Marshal(value)
			if err != nil {
				return err
			}
			return w.register(string(bytes))
		}
		if err := ledger(map[string]any{"event": "legacy_original_prestart_duty", "scope_id": scopeID, "namespace": w.Config.Schema, "writer_sha": stoppedWriterSHA, "binding": binding, "root": w.Directory, "versions": versions}); err != nil {
			t.Fatal(err)
		}
		startRead, startWrite, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		closeStartRead, closeStartWrite := w.ownSetupFile(startRead), w.ownSetupFile(startWrite)
		stopRead, stopWrite, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		closeStopRead, closeStopWrite := w.ownSetupFile(stopRead), w.ownSetupFile(stopWrite)
		eventRead, eventWrite, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		closeEventRead, closeEventWrite := w.ownSetupFile(eventRead), w.ownSetupFile(eventWrite)
		cmd := exec.CommandContext(childCtx, binary)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.WaitDelay = time.Second
		cmd.Stdout = eventWrite
		cmd.Stderr = os.Stderr
		cmd.ExtraFiles = []*os.File{startRead, stopRead}
		cmd.Env = append(os.Environ(), "CONTENT_LEGACY_SCOPE_ID="+scopeID, "CONTENT_LEGACY_SCHEMA="+w.Config.Schema, "CONTENT_LEGACY_ROOT="+w.Directory, "CONTENT_LEGACY_BINDING="+binding)
		w.retainedInvocation = true
		if err = cmd.Start(); err != nil {
			t.Fatal(err)
		}
		joined := false
		defer func() {
			if !joined {
				cancel()
				waitErr := cmd.Wait()
				absent := errors.Is(syscall.Kill(-cmd.Process.Pid, 0), syscall.ESRCH)
				if e := ledger(map[string]any{"event": "legacy_unqualified_completion", "scope_id": scopeID, "pid": cmd.Process.Pid, "wait_error": fmt.Sprint(waitErr), "group_absent": absent, "logical_close_inferred": false, "cleanup_allowed": false}); e != nil {
					t.Error(e)
				}
			}
		}()
		pid := cmd.Process.Pid
		pgid, err := syscall.Getpgid(pid)
		if err != nil || pgid != pid {
			t.Fatal("original child process group unknown", err)
		}
		start, err := legacyActualStart(pid)
		if err != nil {
			t.Fatal(err)
		}
		if err = errors.Join(closeStartRead(), closeStopRead(), closeEventWrite()); err != nil {
			t.Fatal(err)
		}
		if err = ledger(map[string]any{"event": "legacy_original_start_ack", "scope_id": scopeID, "pid": pid, "pgid": pgid, "starttime": start, "binding": binding, "effect_released": false}); err != nil {
			t.Fatal(err)
		}
		if err = errors.Join(legacyPipeWrite(childCtx, startWrite), closeStartWrite()); err != nil {
			t.Fatal(err)
		}
		published, err := legacyPipeFrame(childCtx, eventRead)
		if err != nil || published.Phase != "published" || published.Failed {
			t.Fatal("frozen normal producer did not reach published gate", err, published)
		}
		observation = published.Observation
		if observation.ScopeID != scopeID || observation.Schema != w.Config.Schema || observation.Binding != binding || observation.WriterSHA != stoppedWriterSHA || observation.MigrationChecksum != "sha256:00363b79dafb6eb1f373be9ef08e915fe23cc3db0fa55345e8ae6c051ce0c1ed" || len(observation.Requests) != 2 || len(observation.Receipts) != 2 || len(observation.States) != 2 || len(observation.Attempts) != 2 {
			t.Fatal("original responsibility did not match prestart scope", observation)
		}
		for i, expected := range versions {
			request := observation.Requests[i]
			if request.Payload.ContentRef != expected.Ref || string(request.Payload.Purpose) != expected.Purpose || observation.States[i] != "published" {
				t.Fatal("old normal original version differs", request)
			}
			effects := observation.Attempts[expected.ObjectKey]
			if len(effects) != 1 || effects[0].Key != expected.ObjectKey || effects[0].Hash != string(expected.Ref.Hash) || effects[0].InputSHA256 != strings.TrimPrefix(string(expected.Ref.Hash), "sha256:") || fmt.Sprint(effects[0].Length) != string(expected.Ref.ByteLength) || effects[0].Attempt == "" {
				t.Fatal("actual original full Put responsibility incomplete", effects)
			}
			versions[i].AttemptKeys = []string{effects[0].Attempt}
		}
		if err = ledger(map[string]any{"event": "legacy_original_published", "scope_id": scopeID, "pid": pid, "pgid": pgid, "starttime": start, "observation": observation}); err != nil {
			t.Fatal(err)
		}
		if whileOldWriterHeld != nil {
			whileOldWriterHeld(w, observation)
		}
		if err = errors.Join(legacyPipeWrite(childCtx, stopWrite), closeStopWrite()); err != nil {
			t.Fatal(err)
		}
		stopped, frameErr := legacyPipeFrame(childCtx, eventRead)
		waitErr := cmd.Wait()
		joined = true
		absent := errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
		known := cmd.ProcessState != nil
		exit := -1
		if known {
			exit = cmd.ProcessState.ExitCode()
		}
		closeErr := closeEventRead()
		if err = ledger(map[string]any{"event": "legacy_original_completion", "scope_id": scopeID, "pid": pid, "pgid": pgid, "starttime": start, "exit": exit, "state_known": known, "group_absent": absent, "close_frame": stopped, "frame_error": fmt.Sprint(frameErr), "wait_error": fmt.Sprint(waitErr), "parent_pipe_close_error": fmt.Sprint(closeErr)}); err != nil {
			t.Fatal(err)
		}
		if frameErr != nil || waitErr != nil || closeErr != nil || !absent || !known || stopped.Phase != "stopped" || stopped.Failed || !stopped.ObjectCloseACK || !stopped.StoreCloseACK || !stopped.WitnessCloseACK || !stopped.StartGateCloseACK || !stopped.StopGateCloseACK || !stopped.ObjectFDsClosed {
			t.Fatal("original writer logical Close or kernel completion unconfirmed", frameErr, waitErr, closeErr, stopped)
		}
		first, err := json.Marshal(observation)
		if err != nil {
			t.Fatal(err)
		}
		second, err := json.Marshal(stopped.Observation)
		if err != nil || string(first) != string(second) {
			t.Fatal("original handoff evidence changed", err)
		}
		info, err := os.Lstat(w.Directory)
		if err != nil {
			t.Fatal(err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || uint64(stat.Dev) != w.device || stat.Ino != w.inode {
			t.Fatal("original owned root replaced during handoff")
		}
		evidence, err := json.Marshal(struct {
			PID, PGID          int
			Start              string
			Published, Stopped stoppedLegacyFrame
		}{pid, pgid, start, published, stopped})
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(evidence)
		qualification = d.LegacyPrimaryQualification{ID: scopeID, Namespace: w.Config.Schema, Owner: owner, WriterSHA: stoppedWriterSHA, Binding: binding, EvidenceDigest: "sha256:" + hex.EncodeToString(digest[:]), ValidUntil: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond), Versions: versions}
		if err = ledger(map[string]any{"event": "legacy_exact_scope_qualified", "qualification": qualification, "old_writer_restart_authorized": false}); err != nil {
			t.Fatal(err)
		}
		w.retainedInvocation = false
	})
	return w, observation, qualification
}
