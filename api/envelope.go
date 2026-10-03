// Package api 定义同版线协议；不依赖服务端或数据库实现。
package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"
)

const Protocol = "harness/1"
const Profile = "architecture-2026-10-data1"
const MaxJSONBytes = 256 << 10
const MaxSafeInteger = uint64(9007199254740991)

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*_[0-9a-f]{32}$`)

func ValidID(id string) bool { return idPattern.MatchString(id) }
func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
func Hash(b []byte) string    { h := sha256.Sum256(b); return "sha256:" + hex.EncodeToString(h[:]) }
func Time(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func ParseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || t.Location() != time.UTC {
		return time.Time{}, fmt.Errorf("invalid UTC time")
	}
	return t, nil
}

type Command struct {
	Protocol         string          `json:"protocol"`
	Profile          string          `json:"profile"`
	LogicalServiceID string          `json:"logical_service_id"`
	CommandID        string          `json:"command_id"`
	Method           string          `json:"method"`
	TargetID         string          `json:"target_id"`
	ExpiresAt        string          `json:"expires_at"`
	ExpectedRevision *uint64         `json:"expected_revision,omitempty"`
	Payload          json.RawMessage `json:"payload"`
}
type Query struct {
	Protocol         string          `json:"protocol"`
	Profile          string          `json:"profile"`
	LogicalServiceID string          `json:"logical_service_id"`
	QueryID          string          `json:"query_id"`
	Method           string          `json:"method"`
	TargetID         string          `json:"target_id"`
	Payload          json.RawMessage `json:"payload"`
}
type Receipt struct {
	CommandID     string          `json:"command_id"`
	RequestDigest string          `json:"request_digest"`
	Stage         string          `json:"stage"`
	AcceptedAt    string          `json:"accepted_at,omitempty"`
	DecidedAt     string          `json:"decided_at,omitempty"`
	Output        json.RawMessage `json:"output,omitempty"`
	Error         *Error          `json:"error,omitempty"`
}
type ReceiptLookup struct {
	LogicalServiceID string `json:"logical_service_id"`
	CommandID        string `json:"command_id"`
}
type Error struct {
	Code   string `json:"code"`
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
	Retry  string `json:"retry"`
	Detail string `json:"detail,omitempty"`
	Cause  error  `json:"-"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Reason }
func (e *Error) Unwrap() error { return e.Cause }
func E(code, reason string) *Error {
	retry := "none"
	switch code {
	case "forbidden", "revision_conflict", "invalid_state", "cursor_expired", "snapshot_required":
		retry = "after_change"
	case "overloaded", "dependency_unavailable":
		retry = "backoff"
	case "effect_unknown", "accounting_unknown":
		retry = "query_original"
	}
	return &Error{Code: code, Scope: "request", Reason: reason, Retry: retry}
}
func IsCode(err error, code string) bool { var e *Error; return errors.As(err, &e) && e.Code == code }

type Page[T any] struct {
	Items              []T      `json:"items"`
	CollectionRevision uint64   `json:"collection_revision"`
	NextCursor         string   `json:"next_cursor,omitempty"`
	Exhausted          bool     `json:"exhausted"`
	Partial            bool     `json:"partial"`
	Gaps               []string `json:"gaps"`
}
type ListInput struct {
	Limit  uint64 `json:"limit"`
	Cursor string `json:"cursor,omitempty"`
}
