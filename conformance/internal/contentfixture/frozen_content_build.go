package contentfixture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// The complete frozen production package closure is checked in as inert text.
// The supervisor driver is separate; no current product package participates.
//
//go:embed testdata/ticket05-frozen-1a7 testdata/ticket05-producer/main.go.txt
var frozenContentFiles embed.FS

type frozenContentEntry struct {
	Path     string `json:"path"`
	Artifact string `json:"artifact"`
	Bytes    int    `json:"bytes"`
	SHA256   string `json:"sha256"`
}
type frozenContentManifest struct {
	Format       string               `json:"format"`
	SourceCommit string               `json:"source_commit"`
	Files        []frozenContentEntry `json:"files"`
	FileCount    int                  `json:"file_count"`
	PayloadBytes int                  `json:"payload_bytes"`
}
type finiteCompilerOutput struct {
	Body     []byte
	Overflow bool
}

func (o *finiteCompilerOutput) Write(bytes []byte) (int, error) {
	n := len(bytes)
	if len(o.Body)+n > 32768 {
		o.Overflow = true
		return n, nil
	}
	o.Body = append(o.Body, bytes...)
	return n, nil
}

func (w *World) buildFrozenContent(ctx context.Context) (string, error) {
	base := "testdata/ticket05-frozen-1a7/"
	manifestRaw, err := frozenContentFiles.ReadFile(base + "provenance.json")
	if err != nil {
		return "", err
	}
	manifestSHA := sha256.Sum256(manifestRaw)
	if hex.EncodeToString(manifestSHA[:]) != "589a151af608daf9accc510c7905d7635528fd3fb87cac64a2cdf8f7070d9c77" {
		return "", errors.New("frozen production manifest changed")
	}
	var manifest frozenContentManifest
	if err = json.Unmarshal(manifestRaw, &manifest); err != nil {
		return "", err
	}
	if manifest.Format != "ticket05-frozen-content-source-1" || manifest.SourceCommit != stoppedWriterSHA || manifest.FileCount != 75 || len(manifest.Files) != manifest.FileCount || manifest.PayloadBytes != 466704 {
		return "", errors.New("frozen production scope incomplete")
	}
	directory, err := os.MkdirTemp("", "lerna-content-frozen-build-")
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return "", err
	}
	identity, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("frozen build directory identity unknown")
	}
	joined := false
	w.infrastructureClosers = append(w.infrastructureClosers, func() error {
		if !joined || w.setupCloseErr != nil {
			return errors.New("retain frozen build: child or setup Close unconfirmed")
		}
		current, err := os.Lstat(directory)
		if err != nil {
			return err
		}
		st, ok := current.Sys().(*syscall.Stat_t)
		if !ok || st.Dev != identity.Dev || st.Ino != identity.Ino {
			return errors.New("original frozen build root changed")
		}
		return os.RemoveAll(directory)
	})
	syncDirectory := func(path string) error {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		closeFile := w.ownSetupFile(file)
		return errors.Join(file.Sync(), closeFile())
	}
	if err = errors.Join(syncDirectory(directory), syncDirectory(filepath.Dir(directory))); err != nil {
		return "", err
	}
	if err = w.register(fmt.Sprintf("frozen_content_build_scope %s %d %d writer=%s manifest_sha256=%s", directory, identity.Dev, identity.Ino, stoppedWriterSHA, hex.EncodeToString(manifestSHA[:]))); err != nil {
		return "", err
	}
	directories := map[string]bool{directory: true}
	write := func(name string, body []byte) error {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		for parent := filepath.Dir(path); parent != directory; parent = filepath.Dir(parent) {
			directories[parent] = true
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		closeFile := w.ownSetupFile(file)
		n, writeErr := file.Write(body)
		if writeErr == nil && n != len(body) {
			writeErr = io.ErrShortWrite
		}
		if err = errors.Join(writeErr, file.Sync(), closeFile()); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(path))
	}
	seen := map[string]bool{}
	for _, entry := range manifest.Files {
		if !fs.ValidPath(entry.Path) || !fs.ValidPath(entry.Artifact) || entry.Artifact != entry.Path+".txt" || seen[entry.Path] {
			return "", errors.New("invalid exact frozen source path")
		}
		seen[entry.Path] = true
		body, err := frozenContentFiles.ReadFile(base + entry.Artifact)
		if err != nil {
			return "", err
		}
		digest := sha256.Sum256(body)
		if len(body) != entry.Bytes || hex.EncodeToString(digest[:]) != entry.SHA256 {
			return "", errors.New("frozen source entry changed")
		}
		if err = write(entry.Path, body); err != nil {
			return "", err
		}
	}
	driver, err := frozenContentFiles.ReadFile("testdata/ticket05-producer/main.go.txt")
	if err != nil {
		return "", err
	}
	driverSHA := sha256.Sum256(driver)
	if hex.EncodeToString(driverSHA[:]) != "3d683e3b66e662b21102a42e0f418d49c1776e52cd4f421bfb3f88da718ee8be" {
		return "", errors.New("independent frozen supervisor source changed")
	}
	if err = write(".ticket05-producer/main.go", driver); err != nil {
		return "", err
	}
	for path := range directories {
		if err = syncDirectory(path); err != nil {
			return "", err
		}
	}
	buildCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	binary := filepath.Join(directory, "legacy-producer")
	gateRead, gateWrite, err := os.Pipe()
	if err != nil {
		return "", err
	}
	closeRead, closeWrite := w.ownSetupFile(gateRead), w.ownSetupFile(gateWrite)
	cmd := exec.CommandContext(buildCtx, "bash", "-c", `IFS= read -r -n 1 gate <&3 || exit 125; exec 3<&- || exit 125; printf '%s\n' FROZEN_BUILD_GATE_CLOSE_ACK; exec go "$@"`, "frozen-compiler", "build", "-mod=readonly", "-o", binary, "./.ticket05-producer")
	cmd.Dir = directory
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.ExtraFiles = []*os.File{gateRead}
	cmd.WaitDelay = time.Second
	cmd.Cancel = func() error { return cmd.Process.Kill() }
	output := &finiteCompilerOutput{}
	// Identical writer pointer lets os/exec serialize stdout/stderr, retaining
	// only finite diagnostics and joining all managed pipe copy tasks in Wait.
	cmd.Stdout = output
	cmd.Stderr = output
	if err = w.register("frozen_content_compiler_prestart " + directory); err != nil {
		return "", err
	}
	w.retainedInvocation = true
	if err = cmd.Start(); err != nil {
		return "", err
	}
	waited := false
	defer func() {
		if !waited {
			cancel()
			waitErr := cmd.Wait()
			absent := errors.Is(syscall.Kill(-cmd.Process.Pid, 0), syscall.ESRCH)
			if e := w.register(fmt.Sprintf("frozen_content_compiler_unqualified pid=%d exit=%v group_absent=%t", cmd.Process.Pid, waitErr, absent)); e != nil {
				w.t.Error(e)
			}
		}
	}()
	pid := cmd.Process.Pid
	pgid, identityErr := syscall.Getpgid(pid)
	start, startErr := legacyActualStart(pid)
	if identityErr != nil || startErr != nil || pgid != pid {
		return "", errors.Join(identityErr, startErr, errors.New("compiler actual identity unknown"))
	}
	if err = closeRead(); err != nil {
		return "", err
	}
	if err = w.register(fmt.Sprintf("frozen_content_compiler_start_ack pid=%d pgid=%d starttime=%s source=%s", pid, pgid, start, stoppedWriterSHA)); err != nil {
		return "", err
	}
	if err = errors.Join(legacyPipeWrite(buildCtx, gateWrite), closeWrite()); err != nil {
		return "", err
	}
	waitErr := cmd.Wait()
	waited = true
	absent := errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
	known := cmd.ProcessState != nil
	exit := -1
	if known {
		exit = cmd.ProcessState.ExitCode()
	}
	controlClosed := bytes.HasPrefix(output.Body, []byte("FROZEN_BUILD_GATE_CLOSE_ACK\n"))
	if err = w.register(fmt.Sprintf("frozen_content_compiler_completion pid=%d pgid=%d starttime=%s exit=%d group_absent=%t state_known=%t control_close_ack=%t", pid, pgid, start, exit, absent, known, controlClosed)); err != nil {
		return "", err
	}
	if waitErr != nil || !absent || !known || !controlClosed || output.Overflow {
		return "", errors.Join(waitErr, errors.New("frozen compiler failed or unconfirmed: "+string(output.Body)))
	}
	artifact, err := os.Open(binary)
	if err != nil {
		return "", err
	}
	closeArtifact := w.ownSetupFile(artifact)
	artifactInfo, statErr := artifact.Stat()
	if statErr != nil || artifactInfo.Size() < 1 || artifactInfo.Size() > 64*1024*1024 {
		return "", errors.Join(statErr, closeArtifact(), errors.New("finite actual compiler artifact unavailable"))
	}
	digest := sha256.New()
	n, readErr := io.CopyN(digest, artifact, artifactInfo.Size())
	if readErr == nil && n != artifactInfo.Size() {
		readErr = io.ErrUnexpectedEOF
	}
	if err = errors.Join(readErr, artifact.Sync(), closeArtifact(), syncDirectory(directory)); err != nil {
		return "", err
	}
	if err = w.register("frozen_content_compiler_artifact " + binary + " sha256:" + hex.EncodeToString(digest.Sum(nil)) + " driver_sha256:" + hex.EncodeToString(driverSHA[:])); err != nil {
		return "", err
	}
	joined = true
	w.retainedInvocation = false
	return binary, nil
}
