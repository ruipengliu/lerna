package harness_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

// 完整保留的历史墓碑必须计入容量；新增责任拒绝早于物理发送，原 ID 的收尾仍可继续。
func TestFullFileJournalRejectsNewResponsibilityBeforeSendAndRecoversOriginal(t *testing.T) {
	ctx := context.Background()
	owner := api.NewID("owner")
	discovery := fixtureDiscovery(owner)
	root := t.TempDir()
	var first harness.Entry
	// 模拟启动前已保存的真实目录历史，正文与原摘要按同版公开 Entry 合同写入。
	for index := range 4094 {
		command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: owner, CommandID: api.NewID("command"), Method: "fixture.save", TargetID: owner, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(fixtureInput{Value: "retained original"})}
		digest, err := api.Digest(command)
		if err != nil {
			t.Fatal(err)
		}
		receipt := &api.Receipt{CommandID: command.CommandID, RequestDigest: digest, Stage: "applied", DecidedAt: api.Time(time.Now()), Output: api.Raw(fixtureOutput{Saved: "retained"})}
		entry := harness.Entry{IdentityScope: discovery.IdentityScope, SchemaDigest: discovery.SchemaDigest, MethodSchemaDigest: discovery.Methods[0].SchemaDigest, Command: command, Digest: digest, Receipt: receipt}
		if index == 0 {
			first = entry
		}
		if err = os.WriteFile(filepath.Join(root, command.CommandID+".json"), api.Raw(entry), 0600); err != nil {
			t.Fatal(err)
		}
	}
	journal, err := harness.OpenJournal(root, discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if journal != nil {
			if err := journal.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	transport := &lostReply{}
	client, err := harness.NewClient(transport, journal, discovery)
	if err != nil {
		t.Fatal(err)
	}
	original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: owner, CommandID: api.NewID("command"), Method: "fixture.save", TargetID: owner, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(fixtureInput{Value: "original pending at capacity"})}
	if _, err = client.Send(ctx, original); err == nil || transport.calls != 1 {
		t.Fatalf("original lost reply %d %v", transport.calls, err)
	}
	newCommand := original
	newCommand.CommandID = api.NewID("command")
	if _, err = client.Send(ctx, newCommand); !api.IsCode(err, "overloaded") || transport.calls != 1 {
		t.Fatalf("full journal admitted and sent unrecoverable new identity: calls=%d err=%v", transport.calls, err)
	}
	if _, err = journal.Read(ctx, newCommand.CommandID); !os.IsNotExist(err) {
		t.Fatalf("rejected identity became a journal responsibility: %v", err)
	}
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal = nil
	journal, err = harness.OpenJournal(root, discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	client, err = harness.NewClient(transport, journal, discovery)
	if err != nil {
		t.Fatal(err)
	}
	receipts, partial, err := client.Recover(ctx)
	if err != nil || partial || len(receipts) != 1 || receipts[0].CommandID != original.CommandID || transport.calls != 1 {
		t.Fatalf("full journal lost original receipt recovery %+v %v %v calls=%d", receipts, partial, err, transport.calls)
	}
	// 满容量时的原 ID 仍先查询准确回执，不创造身份、摘要或期限。
	if _, err = client.Send(ctx, original); err != nil || transport.calls != 1 || !api.Equal(transport.command, original) {
		t.Fatalf("original receipt recovery drifted at capacity: %d %v", transport.calls, err)
	}
	retained, err := journal.Read(ctx, first.Command.CommandID)
	if err != nil || !api.Equal(retained, first) {
		t.Fatalf("capacity enforcement removed original tombstone: %v", err)
	}
	names, err := os.ReadDir(root)
	if err != nil || len(names) != 4096 {
		t.Fatalf("capacity or tombstone history changed: %d %v", len(names), err)
	}
}
