package development

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 通过宿主公开命令与实际 timer/交付 Job 验证安装锁，不调用私有 gate。
func TestScheduleUsesInstallLockAndKeepsOriginalOccurrence(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL fixture required")
			}
			runScheduleInstallLock(t, driver)
		})
	}
}

func runScheduleInstallLock(t *testing.T, driver string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	root := t.TempDir()
	cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	cfg.TZDBRoot, err = filepath.Abs("../../conformance/testdata/tzdb/2026b")
	if err != nil {
		t.Fatal(err)
	}
	utc, err := os.ReadFile(filepath.Join(cfg.TZDBRoot, "Etc/UTC"))
	if err != nil || api.Hash(utc) != "sha256:8b85846791ab2c8a5463c83a5be3c043e2570d7448434d41398969ed47e3e6f2" {
		t.Fatal("schedule fixture must retain the pinned 2026b TZif")
	}
	app, err := OpenApp(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if api.Equal(app.InstallLock, app.Profile.Ref) {
		t.Fatal("installation lock and model profile must be independently identified")
	}
	template, err := app.Publish(ctx, app.Scope, app.UserAuth, api.NewID("content"), "application/json", []byte(`{"kind":"answer","body":"Original scheduled answer."}`), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	send := func(method, id string, revision *uint64, input interaction.ScheduleInput) api.Receipt {
		t.Helper()
		command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: app.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: id, Method: method, ExpectedRevision: revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(input)}
		receipt, err := app.Dispatcher.Command(ctx, app.UserAuth, api.Raw(command))
		if err != nil {
			t.Fatal(err)
		}
		return receipt
	}
	read := func(id string) interaction.Schedule {
		t.Helper()
		body, err := clarificationQuery(ctx, app, "schedule.read", id, interaction.ReadInput{})
		var schedule interaction.Schedule
		if err != nil || api.Decode(body, &schedule) != nil {
			t.Fatalf("public original schedule %v", err)
		}
		return schedule
	}
	wrongID := api.NewID("schedule")
	wrong := interaction.ScheduleInput{Spec: interaction.ScheduleSpec{Type: "once_at", At: api.Time(time.Now().Add(2 * time.Second).Truncate(time.Second))}, Timezone: "UTC", TZDBVersion: "2026b", TemplateRef: template, PolicyRef: app.TaskPolicy.PolicyRef, InstallLockRef: app.Profile.Ref, TaskTimeoutSeconds: 120, Budget: []api.Amount{{Unit: "USD", Value: "20"}}}
	receipt := send("schedule.create", wrongID, nil, wrong)
	if receipt.Stage != "rejected" || !api.IsCode(receipt.Error, "unsupported") {
		t.Fatalf("model profile is not an installation lock: %+v", receipt)
	}
	if _, err = clarificationQuery(ctx, app, "schedule.read", wrongID, interaction.ReadInput{}); !api.IsCode(err, "not_found") {
		t.Fatalf("wrong installation lock created a Schedule: %v", err)
	}
	assertNoScheduleTrigger(t, ctx, app, wrong.Spec.At)
	id := api.NewID("schedule")
	original := wrong
	original.InstallLockRef = app.InstallLock
	original.Spec.At = api.Time(time.Now().Add(3 * time.Second).Truncate(time.Second))
	receipt = send("schedule.create", id, nil, original)
	if receipt.Stage != "applied" || receipt.Error != nil {
		t.Fatalf("registered installation lock must create schedule: %+v", receipt)
	}
	awaitScheduleTime(t, ctx, original.Spec.At)
	stepScheduleKind(t, ctx, app, interaction.JobTrigger)
	current := read(id)
	if current.ActiveOccurrenceID == "" {
		t.Fatal("actual future timer must record an occurrence")
	}
	frozen, err := app.Interaction.ReadOccurrence(ctx, app.Store, app.Scope, app.UserAuth, current.ActiveOccurrenceID)
	if err != nil || frozen.Phase != "recorded" || !api.Equal(frozen.Frozen, original) {
		t.Fatalf("trigger changed original installation lock or rule %+v %v", frozen, err)
	}
	updated := original
	updated.Spec.At = api.Time(time.Now().Add(time.Minute).Truncate(time.Second))
	updated.TaskTimeoutSeconds = 300
	revision := current.Revision
	receipt = send("schedule.update", id, &revision, updated)
	if receipt.Stage != "applied" || receipt.Error != nil {
		t.Fatalf("registered installation lock must update future rule: %+v", receipt)
	}
	current = read(id)
	if current.RuleRevision != 2 || !api.Equal(current.ScheduleInput, updated) {
		t.Fatal("accurate updated schedule was not applied")
	}
	// 合法更新复用已到期的原 trigger；先沿原身份处理它，再观察拒绝是否新增责任。
	recomputed := stepScheduleKind(t, ctx, app, interaction.JobTrigger)
	if recomputed.Job.ResponsibilityKey != id || recomputed.Job.SourceRef != app.Scope.Ref(id, current.Revision) || recomputed.Job.WorkRevision != 2 {
		t.Fatalf("future edit replaced the original trigger responsibility: %+v", recomputed.Job)
	}
	current = read(id)
	wrong = updated
	wrong.InstallLockRef = app.Profile.Ref
	wrong.Spec.At = api.Time(time.Now().Add(2 * time.Second).Truncate(time.Second))
	revision = current.Revision
	receipt = send("schedule.update", id, &revision, wrong)
	if receipt.Stage != "rejected" || !api.IsCode(receipt.Error, "unsupported") || !api.Equal(read(id), current) {
		t.Fatalf("model profile must not edit the original schedule: %+v", receipt)
	}
	assertNoScheduleTrigger(t, ctx, app, wrong.Spec.At)
	stepScheduleKind(t, ctx, app, interaction.JobOccurrence)
	delivered, err := app.Interaction.ReadOccurrence(ctx, app.Store, app.Scope, app.UserAuth, frozen.OccurrenceID)
	if err != nil || delivered.Phase != "accepted" || delivered.Command == nil || delivered.TaskRef == nil || delivered.Receipt == nil || !api.Equal(delivered.Frozen, original) || delivered.RuleRevision != 1 {
		t.Fatalf("original frozen lock must pass the actual delivery gate %+v %v", delivered, err)
	}
	originalReceipt, err := app.Dispatcher.Lookup(ctx, app.UserAuth, delivered.Command.CommandID)
	if err != nil || !api.Equal(originalReceipt, *delivered.Receipt) || originalReceipt.Stage != "applied" {
		t.Fatalf("original occurrence command lacks its real Task receipt %+v %v", originalReceipt, err)
	}
	var submitted task.SubmitInput
	if api.Decode(delivered.Command.Payload, &submitted) != nil || submitted.GoalRef != template || submitted.PolicyRef != original.PolicyRef || !api.Equal(submitted.Budget, original.Budget) || submitted.Deadline != frozen.TaskDeadline {
		t.Fatal("future edit drifted the original occurrence command")
	}
}

func awaitScheduleTime(t *testing.T, ctx context.Context, at string) {
	t.Helper()
	due, err := api.ParseTime(at)
	if err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(max(time.Until(due)+20*time.Millisecond, 0))
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func assertNoScheduleTrigger(t *testing.T, ctx context.Context, app *App, at string) {
	t.Helper()
	awaitScheduleTime(t, ctx, at)
	work, status, err := app.Store.Claim(ctx, app.Scope, api.NewID("worker"), []string{interaction.JobTrigger}, 1, 30*time.Second)
	if err != nil || status != runtime.Committed || len(work) != 0 {
		t.Fatalf("rejected installation lock raised a trigger %+v %s %v", work, status, err)
	}
}

func stepScheduleKind(t *testing.T, ctx context.Context, app *App, kind string) runtime.Work {
	t.Helper()
	work, status, err := app.Store.Claim(ctx, app.Scope, api.NewID("worker"), []string{kind}, 1, 30*time.Second)
	if err != nil || status != runtime.Committed || len(work) != 1 {
		t.Fatalf("original schedule responsibility %s %s %v", kind, status, err)
	}
	handler, ok := app.Registry.Job(kind)
	if !ok {
		t.Fatal("original schedule handler missing")
	}
	if err = handler(ctx, app.Store, app.Scope, work[0]); err != nil {
		t.Fatal(err)
	}
	return work[0]
}
