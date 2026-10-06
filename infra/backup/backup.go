// Package backup 复制已停止写入的真实文件组；不打开 SQLite，也不证明外部写者已停止或备份最新。
package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ruipengliu/lerna/contracts/command"
)

const manifestName = "manifest.json"
const manifestVersion = "lerna.m1.stopped-backup.v1"
const maxManifestSize = 64 * 1024

var fileNames = [...]string{"state.db", "state.db-wal", "state.db-shm", "state.db-journal"}

type Environment struct {
	Platform                 string `json:"platform"`
	SQLiteVersion            string `json:"sqlite_version"`
	SQLiteSourceID           string `json:"sqlite_source_id"`
	SQLiteCompileOptionsHash string `json:"sqlite_compile_options_hash"`
	JournalMode              string `json:"journal_mode"`
	Synchronous              int    `json:"synchronous"`
	FullFSync                int    `json:"full_fsync"`
	DurabilityProfile        string `json:"durability_profile"`
	PowerLossQualified       bool   `json:"power_loss_qualified"`
}

type File struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
}

type Manifest struct {
	Version               string      `json:"version"`
	CreatedAtUnixMs       int64       `json:"created_at_unix_ms"`
	UserID                string      `json:"user_id"`
	DomainID              string      `json:"domain_id"`
	FormatVersion         uint32      `json:"format_version"`
	ContractVersion       uint32      `json:"contract_version"`
	FormatDigest          string      `json:"format_digest"`
	ImplementationProfile string      `json:"implementation_profile"`
	Environment           Environment `json:"environment"`
	Files                 []File      `json:"files"`
}

type sourceFile struct {
	file File
	info os.FileInfo
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(b)
}

// CreateStopped 的前置条件是全部写者已关闭且原路径此后不再恢复；只关闭一个数据库连接不足以证明此条件。
func CreateStopped(ctx context.Context, sourcePath, directory string, metadata Manifest) (*Manifest, error) {
	metadata.Version = manifestVersion
	metadata.CreatedAtUnixMs = time.Now().UnixMilli()
	metadata.Files = nil
	if e := validateMetadata(&metadata); e != nil {
		return nil, e
	}
	if e := requireEmptyDirectory(directory); e != nil {
		return nil, e
	}
	var sources []sourceFile
	for _, name := range fileNames {
		suffix := name[len("state.db"):]
		copied, e := copyFile(ctx, sourcePath+suffix, filepath.Join(directory, name), name)
		if e != nil {
			return nil, e
		}
		metadata.Files = append(metadata.Files, copied.file)
		sources = append(sources, copied)
	}
	if !metadata.Files[0].Present || metadata.Files[0].Size == 0 {
		return nil, command.Fail("INVALID_BACKUP")
	}
	for i, source := range sources {
		if e := verifyFile(ctx, sourcePath+fileNames[i][len("state.db"):], source.file, source.info); e != nil {
			return nil, e
		}
	}
	if e := writeManifest(directory, &metadata); e != nil {
		return nil, e
	}
	return Verify(ctx, directory, metadata.UserID, metadata.DomainID)
}

// Verify 在打开任何数据库之前核验固定文件集；清单不能检测合法旧备份或外部世界的回滚。
func Verify(ctx context.Context, directory, user, domain string) (*Manifest, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if e := requireDirectory(directory); e != nil {
		return nil, e
	}
	info, e := os.Lstat(filepath.Join(directory, manifestName))
	if e != nil || !info.Mode().IsRegular() || info.Size() > maxManifestSize {
		return nil, command.Fail("INVALID_BACKUP")
	}
	data, e := os.ReadFile(filepath.Join(directory, manifestName))
	if e != nil {
		return nil, e
	}
	if len(data) > maxManifestSize || uniqueJSONFields(json.NewDecoder(bytes.NewReader(data)), 0) != nil {
		return nil, command.Fail("INVALID_BACKUP")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if e = decoder.Decode(&manifest); e != nil {
		return nil, command.Fail("INVALID_BACKUP")
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		return nil, command.Fail("INVALID_BACKUP")
	}
	if e = validateMetadata(&manifest); e != nil || len(manifest.Files) != len(fileNames) {
		return nil, command.Fail("INVALID_BACKUP")
	}
	if manifest.UserID != user || manifest.DomainID != domain || user == "" || domain == "" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	allowed := map[string]bool{manifestName: true}
	for i, file := range manifest.Files {
		if file.Name != fileNames[i] || file.Size < 0 || file.Present && !validDigest(file.SHA256) || !file.Present && (file.Size != 0 || file.SHA256 != "") {
			return nil, command.Fail("INVALID_BACKUP")
		}
		if e = verifyFile(ctx, filepath.Join(directory, file.Name), file, nil); e != nil {
			return nil, e
		}
		if file.Present {
			allowed[file.Name] = true
		}
	}
	if !manifest.Files[0].Present || manifest.Files[0].Size == 0 {
		return nil, command.Fail("INVALID_BACKUP")
	}
	entries, e := os.ReadDir(directory)
	if e != nil {
		return nil, e
	}
	for _, entry := range entries {
		if !allowed[entry.Name()] {
			return nil, command.Fail("INVALID_BACKUP")
		}
	}
	return &manifest, nil
}

// Restore 仅复制到已存在的空目录，源与目标都验完后才向宿主返回可打开的路径。
func Restore(ctx context.Context, directory, destination, user, domain string) (string, *Manifest, error) {
	manifest, e := Verify(ctx, directory, user, domain)
	if e != nil {
		return "", nil, e
	}
	if e = requireEmptyDirectory(destination); e != nil {
		return "", nil, e
	}
	for _, file := range manifest.Files {
		copied, err := copyFile(ctx, filepath.Join(directory, file.Name), filepath.Join(destination, file.Name), file.Name)
		if err != nil {
			return "", nil, err
		}
		if copied.file != file {
			return "", nil, command.Fail("INVALID_BACKUP")
		}
	}
	if e = writeManifest(destination, manifest); e != nil {
		return "", nil, e
	}
	if _, e = Verify(ctx, directory, user, domain); e != nil {
		return "", nil, e
	}
	verified, e := Verify(ctx, destination, user, domain)
	if e != nil {
		return "", nil, e
	}
	return filepath.Join(destination, "state.db"), verified, nil
}

func copyFile(ctx context.Context, source, target, name string) (sourceFile, error) {
	result := sourceFile{file: File{Name: name}}
	info, e := os.Lstat(source)
	if errors.Is(e, os.ErrNotExist) {
		return result, nil
	}
	if e != nil || !info.Mode().IsRegular() {
		return result, command.Fail("INVALID_BACKUP")
	}
	input, e := os.Open(source)
	if e != nil {
		return result, e
	}
	output, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return result, errors.Join(e, input.Close())
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(output, hash), contextReader{ctx, input})
	e = errors.Join(copyErr, output.Sync(), output.Close(), input.Close())
	if e != nil {
		return result, e
	}
	result.info = info
	result.file.Present = true
	result.file.Size = size
	result.file.SHA256 = hex.EncodeToString(hash.Sum(nil))
	if size != info.Size() {
		return result, command.Fail("INVALID_BACKUP")
	}
	return result, nil
}

func verifyFile(ctx context.Context, path string, expected File, original os.FileInfo) error {
	info, e := os.Lstat(path)
	if errors.Is(e, os.ErrNotExist) && !expected.Present {
		return nil
	}
	if e != nil || !expected.Present || !info.Mode().IsRegular() || info.Size() != expected.Size || original != nil && !os.SameFile(info, original) {
		return command.Fail("INVALID_BACKUP")
	}
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	hash := sha256.New()
	_, readErr := io.Copy(hash, contextReader{ctx, f})
	e = errors.Join(readErr, f.Close())
	if e != nil {
		return e
	}
	after, e := os.Lstat(path)
	if e != nil || !os.SameFile(info, after) || info.Size() != after.Size() || expected.SHA256 != hex.EncodeToString(hash.Sum(nil)) {
		return command.Fail("INVALID_BACKUP")
	}
	return nil
}

func validDigest(value string) bool {
	data, e := hex.DecodeString(value)
	return e == nil && len(data) == sha256.Size && value == hex.EncodeToString(data)
}

func uniqueJSONFields(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return command.Fail("INVALID_BACKUP")
	}
	token, e := decoder.Token()
	if e != nil {
		return e
	}
	delimiter, structured := token.(json.Delim)
	if !structured {
		return nil
	}
	seen := map[string]bool{}
	for decoder.More() {
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, valid := key.(string)
			if !valid || seen[name] {
				return command.Fail("INVALID_BACKUP")
			}
			seen[name] = true
		}
		if e = uniqueJSONFields(decoder, depth+1); e != nil {
			return e
		}
	}
	_, e = decoder.Token()
	return e
}

func validateMetadata(m *Manifest) error {
	if m.Version != manifestVersion || m.CreatedAtUnixMs <= 0 || m.UserID == "" || m.DomainID == "" || m.FormatVersion != 1 || m.ContractVersion != 1 || m.ImplementationProfile != "lerna-m1-v1" || !validDigest(m.FormatDigest) || m.Environment.Platform == "" || m.Environment.SQLiteVersion == "" || m.Environment.SQLiteSourceID == "" || !validDigest(m.Environment.SQLiteCompileOptionsHash) || m.Environment.JournalMode != "wal" || m.Environment.Synchronous != 2 || m.Environment.DurabilityProfile != "LOCAL" || !m.Environment.PowerLossQualified {
		return command.Fail("INVALID_BACKUP")
	}
	return nil
}

func requireDirectory(path string) error {
	info, e := os.Lstat(path)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return command.Fail("INVALID_BACKUP")
	}
	return nil
}

func requireEmptyDirectory(path string) error {
	if e := requireDirectory(path); e != nil {
		return e
	}
	entries, e := os.ReadDir(path)
	if e != nil {
		return e
	}
	if len(entries) != 0 {
		return command.Fail("BACKUP_DESTINATION_NOT_EMPTY")
	}
	return nil
}

func writeManifest(directory string, manifest *Manifest) error {
	f, e := os.OpenFile(filepath.Join(directory, manifestName), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	e = errors.Join(encoder.Encode(manifest), f.Sync(), f.Close())
	if e != nil {
		return e
	}
	dir, e := os.Open(directory)
	if e != nil {
		return e
	}
	return errors.Join(dir.Sync(), dir.Close())
}
