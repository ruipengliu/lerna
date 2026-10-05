//go:build integration

package component_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/contentfixture"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
)

type actualPutCall struct {
	Key, Attempt, Hash string
	Length             int64
	Body               string
}
type actualPutReplyLoss struct {
	content.Objects
	Calls         []actualPutCall
	RepliesToLose int
}

func (p *actualPutReplyLoss) Put(ctx context.Context, key, attempt, hash string, length int64, body []byte) error {
	p.Calls = append(p.Calls, actualPutCall{key, attempt, hash, length, string(body)})
	if err := p.Objects.Put(ctx, key, attempt, hash, length, body); err != nil {
		return err
	}
	if p.RepliesToLose > 0 {
		p.RepliesToLose--
		return errors.New("mechanical successful native Put reply lost")
	}
	return nil
}

func TestContentEveryOriginalAttemptPagePreservesUnknownSameKeyResidual(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.New(t, ctx)
	installContentPolicy(t, ctx, w, alphaRef)
	service := contentService(t, w)
	request := contentPut(t, alphaRef, "all-original-attempt-pages", "YWxwaGEK")
	receipt := putContentRequest(t, ctx, service, request)
	if _, ok := receipt.AsAccepted(); !ok {
		t.Fatal("normal original admission refused")
	}
	actual := &actualPutReplyLoss{Objects: w.Objects, RepliesToLose: 2}
	service = withPublication(t, w, w.Store(), actual, time.Minute, 5*time.Second)
	_, key, err := content.VersionIdentity(alphaRef)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if worked, err := service.Step(ctx); err != nil || !worked {
			t.Fatal("original finite retry did not progress", worked, err)
		}
		if len(actual.Calls) != i+1 {
			t.Fatal("real original Put call missing", actual.Calls)
		}
		call := actual.Calls[i]
		if call.Key != key || !strings.HasPrefix(call.Attempt, key+".") || call.Hash != alphaRef.Hash || call.Length != 6 || call.Body != "alpha\n" {
			t.Fatal("actual original full Put parameters differ", call)
		}
		bytes, err := os.ReadFile(filepath.Join(w.Directory, key))
		if err != nil || string(bytes) != "alpha\n" {
			t.Fatal("lost reply did not follow real native Put success", string(bytes), err)
		}
		if i < 2 {
			waitUntil(t, ctx, time.Now().Add(120*time.Millisecond))
		}
	}
	if actual.Calls[0].Attempt == actual.Calls[1].Attempt || actual.Calls[0].Attempt == actual.Calls[2].Attempt || actual.Calls[1].Attempt == actual.Calls[2].Attempt {
		t.Fatal("original retries reused an attempt identity", actual.Calls)
	}
	assertContentBody(t, ctx, service, alphaRef, nil, "alpha\n")
	v2 := alphaRef
	v2.Version = "2"
	v2.Hash = "sha256:f2c82decdd7181cf98945929a62598db7e6b477e11f6e0eb0ae97020eff151ad"
	v2.ByteLength = "5"
	installContentPolicy(t, ctx, w, v2)
	if _, ok := putContentRequest(t, ctx, contentService(t, w), contentPut(t, v2, "attempt-pages-independent-v2", "YmV0YQo=")).AsAccepted(); !ok {
		t.Fatal("independent V2 admission refused")
	}
	if worked, err := contentService(t, w).Step(ctx); err != nil || !worked {
		t.Fatal("independent V2 publication failed", worked, err)
	}
	assertContentBody(t, ctx, contentService(t, w), v2, nil, "beta\n")
	unknown := w.CreateUnregisteredAttempt(alphaRef, []byte("independent unknown responsibility\n"))
	l := bodyLifecycle(t, w)
	sealRequest := content.SealRequest{Ref: alphaRef, Purpose: "verification", SealID: "every-original-attempt", Deadline: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond)}
	original, err := l.Seal(ctx, &contentPrincipal, sealRequest)
	if err != nil {
		t.Fatal(err)
	}
	var primary content.BodyHolder
	partial := false
	for i := 0; i < 4; i++ {
		if worked, err := l.Step(ctx, &contentPrincipal); err != nil || !worked {
			t.Fatal("original attempt page did not progress", worked, err)
		}
		page, err := l.Observe(ctx, &contentPrincipal, alphaRef, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, holder := range page.Holders {
			if holder.Kind == "primary" {
				primary = holder
			}
		}
		if primary.AttemptCursor != "" {
			if primary.AttemptCursor != actual.Calls[1].Attempt || primary.State != "pending" || primary.Reason != "attempt_page_pending" || page.CleanupComplete || primary.Identity.SealID != original.Seal.ID || !primary.Deadline.Equal(original.Seal.Deadline) {
				t.Fatal("first finite attempt page falsely ACKed or lost original duty", page, actual.Calls)
			}
			partial = true
			break
		}
	}
	if !partial {
		t.Fatal("three actual original attempts did not traverse the page2 continuation")
	}
	w.Reopen(ctx)
	l = bodyLifecycle(t, w)
	if worked, err := l.Step(ctx, &contentPrincipal); err != nil || !worked {
		t.Fatal("last registered attempt page did not continue", worked, err)
	}
	residual, err := l.Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || residual.CleanupComplete || residual.Seal.ID != original.Seal.ID || !residual.Seal.Deadline.Equal(original.Seal.Deadline) {
		t.Fatal("unknown attempt became all-holder ACK or refreshed original seal", residual, err)
	}
	for _, holder := range residual.Holders {
		if holder.Kind == "primary" {
			primary = holder
		}
	}
	if primary.State != "residual" || primary.Reason != "holder_unconfirmed" || primary.Responsible != "primary" {
		t.Fatal("unknown same-key duty was discarded", primary)
	}
	path := filepath.Join(w.Directory, unknown.Name)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal("product guessed and deleted unknown attempt", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || uint64(stat.Dev) != unknown.Device || stat.Ino != unknown.Inode {
		t.Fatal("unknown original setup inode was replaced")
	}
	bytes, err := os.ReadFile(path)
	if err != nil || string(bytes) != "independent unknown responsibility\n" {
		t.Fatal("unknown original bytes changed", string(bytes), err)
	}
	physical, err := w.Objects.ObserveErasure(ctx, primary.Identity, "", 2)
	if err != nil || physical.Erased || !physical.Fenced || len(physical.Residual) != 1 || physical.Residual[0] != unknown.Name {
		t.Fatal("independent exact observation lost unknown residual", physical, err)
	}
	for _, call := range actual.Calls {
		if _, err = os.Lstat(filepath.Join(w.Directory, call.Attempt)); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("registered original attempt was not actually absent", call, err)
		}
	}
	if _, err = os.Lstat(filepath.Join(w.Directory, key)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("primary final bytes still present", err)
	}
	assertContentBody(t, ctx, contentService(t, w), v2, nil, "beta\n")
	// This is recovery by the separate, positively identified setup owner.
	// No product authority adopts the unknown effect or deletes it by guess.
	unknown.Remove()
	waitUntil(t, ctx, time.Now().Add(120*time.Millisecond))
	w.Reopen(ctx)
	l = bodyLifecycle(t, w)
	if worked, err := l.Step(ctx, &contentPrincipal); err != nil || !worked {
		t.Fatal("original confirmed deferred duty did not recover", worked, err)
	}
	complete, err := l.Observe(ctx, &contentPrincipal, alphaRef, "")
	if err != nil || !complete.CleanupComplete || complete.Seal.ID != original.Seal.ID || !complete.Seal.Deadline.Equal(original.Seal.Deadline) {
		t.Fatal("registered page recovery failed or changed original budget", complete, err)
	}
	assertContentBody(t, ctx, contentService(t, w), v2, nil, "beta\n")
	before, err := v.Encode(receipt)
	if err != nil {
		t.Fatal(err)
	}
	after, err := v.Encode(putContentRequest(t, ctx, contentService(t, w), request))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("all-attempt cleanup rewrote original receipt")
	}
}
