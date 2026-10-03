package runner_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/cmd/internal/bootstrap"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/internal/task"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

type processOutput struct {
	sync.Mutex
	strings.Builder
}

func (b *processOutput) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.Builder.Write(p)
}
func (b *processOutput) Text() string { b.Lock(); defer b.Unlock(); return b.Builder.String() }

func start(t *testing.T, executable string, env []string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(executable, args...)
	cmd.Env = env
	output := &processOutput{}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("process did not shut down cleanly: %v %s", err, output.Text())
			}
		case <-time.After(8 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Errorf("process did not actually exit after stop: %s", output.Text())
		}
	})
	return cmd
}

func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func processEnvironment(t *testing.T) (string, []string) {
	t.Helper()
	if os.Getenv("HARNESS_TEST_PROCESS_BACKEND") != "postgres" {
		return "sqlite", os.Environ()
	}
	dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("real PostgreSQL process tests require a private DSN environment reference")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal("PostgreSQL administrative test connection unavailable")
	}
	defer conn.Close(context.Background())
	name := api.NewID("harness_process")
	if _, err = conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal("isolated PostgreSQL test database creation failed")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Error("PostgreSQL test cleanup connection failed")
			return
		}
		defer conn.Close(context.Background())
		if _, err = conn.Exec(ctx, "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)"); err != nil {
			t.Error("isolated PostgreSQL test cleanup failed")
		}
	})
	u, err := url.Parse(dsn)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("test DSN must be a URL reference")
	}
	u.Path = "/" + name
	t.Setenv("HARNESS_DATABASE_DSN", u.String())
	return "postgres", os.Environ()
}

func diagnoseReport(t *testing.T, c bootstrap.Config, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	a, err := bootstrap.OpenAppForRole(ctx, c, false, "management")
	if err != nil {
		t.Logf("report public diagnostic unavailable: %v", err)
		return
	}
	defer func() {
		if err := a.Close(); err != nil {
			t.Logf("report diagnostic close: %v", err)
		}
	}()
	facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.ServiceAuth, id)
	if err != nil {
		t.Logf("report current facts unavailable: %v", err)
		return
	}
	t.Logf("report public facts: checks=%d artifacts=%d operations=%d", len(facts.Checks), len(facts.Artifacts), len(facts.Operations))
	for _, operation := range facts.Operations {
		q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: c.OwnerID, QueryID: api.NewID("query"), Method: "execution.get", TargetID: operation.Intent.OperationID, Payload: api.Raw(execution.OperationIDInput{OperationID: operation.Intent.OperationID})}
		view, err := a.Dispatcher.Query(ctx, a.ServiceAuth, api.Raw(q))
		t.Logf("report public operation=%s fact_effect=%s view=%s error=%v", operation.Intent.OperationID, operation.Fact.Effect, view, err)
	}
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: c.OwnerID, QueryID: api.NewID("query"), Method: "execution.control.get", TargetID: id, Payload: api.Raw(execution.ControlGetInput{TaskID: id})}
	view, err := a.Dispatcher.Query(ctx, a.ServiceAuth, api.Raw(q))
	var control execution.ControlView
	if err == nil {
		err = api.Decode(view, &control)
	}
	if err != nil {
		t.Logf("report public control unavailable: %v", err)
		return
	}
	for _, window := range control.Windows {
		t.Logf("report control window=%s issued_at=%s start_before=%s", window.WindowID, window.IssuedAt, window.StartBefore)
	}
}

func TestIndependentApplicationGatewayAndWorkerCompleteReportAndActuallyStop(t *testing.T) {
	driver, env := processEnvironment(t)
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	migrate := binary(t, "migrate")
	cmd := exec.Command(migrate, "--config", configPath, "--development-init", "--data", root, "--driver", driver)
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("management setup failed: %v %s", err, out)
	}
	c, err := bootstrap.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	c.HTTPAddr, c.GRPCAddr, c.StaticDir = freeAddress(t), freeAddress(t), ""
	if path := os.Getenv("HARNESS_TEST_TZDB_ROOT"); path != "" {
		c.TZDBRoot = path
	}
	if err = bootstrap.SaveConfig(configPath, c); err != nil {
		t.Fatal(err)
	}
	worker, application, gateway, cliExecutable := binary(t, "worker"), binary(t, "application"), binary(t, "gateway"), binary(t, "cli")
	start(t, worker, env, "--config", configPath)
	start(t, application, env, "--config", configPath)
	start(t, gateway, env, "--config", configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	token, err := os.ReadFile(c.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	httpTransport := &harness.HTTPTransport{BaseURL: "http://" + c.HTTPAddr, Token: strings.TrimSpace(string(token)), AllowInsecureLoopback: true, HTTP: &http.Client{Timeout: 30 * time.Second}}
	var discovery harness.Discovery
	for {
		discovery, err = httpTransport.Discover(ctx)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("gateway did not become currently authenticated and ready")
		case <-time.After(100 * time.Millisecond):
		}
	}
	// 受信发布在管理装配中完成；没有写入私有 Task/Decision/Operation 或预置效果。
	a, err := bootstrap.OpenAppForRole(ctx, c, false, "management")
	if err != nil {
		t.Fatal(err)
	}
	goal := brain.GoalSpec{Kind: "report", Title: "Independent process report", Body: "Verified bytes from separate durable workers.", SavePath: "reports/process-proof.md"}
	ref, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", api.Raw(goal), []api.ContentRef{}, []api.ContentRef{})
	policy := a.TaskPolicy.PolicyRef
	if closeErr := a.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	id := api.NewID("task")
	original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: c.OwnerID, CommandID: api.NewID("command"), TargetID: id, Method: "task.submit", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: c.OwnerID, GoalRef: ref, PolicyRef: policy, Deadline: api.Time(time.Now().Add(2 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}, RequirementCandidates: []api.RequirementCandidate{}})}
	request := filepath.Join(root, "submit.json")
	if err = os.WriteFile(request, api.Raw(original), 0600); err != nil {
		t.Fatal(err)
	}
	cli := exec.CommandContext(ctx, cliExecutable, "--endpoint", "grpc://"+c.GRPCAddr, "--discovery", "http://"+c.HTTPAddr, "--development-loopback", "--token-envref", "HARNESS_CLI_CREDENTIAL", "--timeout", "30s", "--journal", filepath.Join(root, "cli-journal"), "--request", request, "command")
	cli.Env = append(env, "HARNESS_CLI_CREDENTIAL="+strings.TrimSpace(string(token)))
	out, err := cli.CombinedOutput()
	if err != nil {
		t.Fatalf("original command through application gRPC failed: %v %s", err, out)
	}
	var receipt api.Receipt
	if err = api.Decode(out, &receipt); err != nil || receipt.Stage != "applied" || receipt.CommandID != original.CommandID {
		t.Fatalf("application receipt: %s %v", out, err)
	}
	j, err := harness.OpenJournal(filepath.Join(root, "queries"), discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	client, err := harness.NewClient(httpTransport, j, discovery)
	if err != nil {
		t.Fatal(err)
	}
	query := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: c.OwnerID, QueryID: api.NewID("query"), Method: "task.read", TargetID: id, Payload: api.Raw(task.ReadInput{})}
	for {
		// 查询用新query_id代表新的读取；已发送原command_id始终保留。
		query.QueryID = api.NewID("query")
		out, err = client.Query(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		var fact api.Task
		if err = json.Unmarshal(out, &fact); err != nil {
			t.Fatal(err)
		}
		if fact.Status == "succeeded" {
			break
		}
		if fact.Status == "failed" || fact.Status == "cancelled" {
			diagnoseReport(t, c, id)
			t.Fatalf("separate worker ended task without verified result: %s", out)
		}
		select {
		case <-ctx.Done():
			diagnoseReport(t, c, id)
			t.Fatalf("separate worker did not complete report: %s", out)
		case <-time.After(100 * time.Millisecond):
		}
	}
	actual, err := os.ReadFile(filepath.Join(root, "files", goal.SavePath))
	if err != nil || string(actual) != "# Independent process report\n\nVerified bytes from separate durable workers.\n" {
		t.Fatalf("independent target bytes: %q %v", actual, err)
	}
}

func TestServiceRolesRefuseInitializationAndMissingConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-config.json")
	for _, role := range []string{"gateway", "application", "worker"} {
		b := binary(t, role)
		_, diagnostic, err := run(t, b, "--config", path)
		if err == nil || !strings.Contains(string(diagnostic), "dependency_unavailable") {
			t.Fatalf("%s silently initialized: %v %s", role, err, diagnostic)
		}
		_, diagnostic, err = run(t, b, "--config", path, "--development-init")
		if err == nil || !strings.Contains(string(diagnostic), "invalid_request") {
			t.Fatalf("%s accepted management flag: %v %s", role, err, diagnostic)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a service created configuration")
	}
}

func TestGatewayUnavailableApplicationCreatesNoDomainFactAndRecoversOriginalPrincipal(t *testing.T) {
	driver, env := processEnvironment(t)
	migrate, gateway, application, cli := binary(t, "migrate"), binary(t, "gateway"), binary(t, "application"), binary(t, "cli")
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	initialize := exec.Command(migrate, "--config", configPath, "--development-init", "--data", root, "--driver", driver)
	initialize.Env = env
	if out, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("management setup failed: %v %s", err, out)
	}
	c, err := bootstrap.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	c.HTTPAddr, c.GRPCAddr, c.StaticDir = freeAddress(t), freeAddress(t), ""
	if err = bootstrap.SaveConfig(configPath, c); err != nil {
		t.Fatal(err)
	}
	start(t, gateway, env, "--config", configPath)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	token, err := os.ReadFile(c.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	transport := &harness.HTTPTransport{BaseURL: "http://" + c.HTTPAddr, Token: strings.TrimSpace(string(token)), AllowInsecureLoopback: true, HTTP: &http.Client{Timeout: 30 * time.Second}}
	var discovery harness.Discovery
	for {
		discovery, err = transport.Discover(ctx)
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("gateway discovery did not become ready")
		case <-time.After(100 * time.Millisecond):
		}
	}
	session, branch := api.NewID("session"), api.NewID("branch")
	query := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: c.OwnerID, QueryID: api.NewID("query"), Method: "session.read", TargetID: session, Payload: api.Raw(interaction.ReadInput{})}
	if _, err = transport.Call(ctx, "query", api.Raw(query)); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("gateway served domain query without application: %v", err)
	}
	original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: c.OwnerID, CommandID: api.NewID("command"), Method: "session.create", TargetID: c.OwnerID, ExpiresAt: api.Time(time.Now().Add(2 * time.Minute)), Payload: api.Raw(interaction.CreateSessionInput{SessionID: session, DefaultBranchID: branch, ConfigRef: api.ComponentRef{ComponentID: api.NewID("component"), Version: "1.0.0", Digest: api.Hash([]byte("original gateway session configuration"))}})}
	request := filepath.Join(root, "original.json")
	if err = os.WriteFile(request, api.Raw(original), 0600); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(root, "original-journal")
	cliEnv := append(env, "HARNESS_CLI_CREDENTIAL="+strings.TrimSpace(string(token)))
	flags := []string{"--endpoint", "http://" + c.HTTPAddr, "--development-loopback", "--token-envref", "HARNESS_CLI_CREDENTIAL", "--timeout", "30s", "--journal", journalPath}
	runCLI := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, cli, append(append([]string{}, flags...), args...)...)
		cmd.Env = cliEnv
		return cmd.CombinedOutput()
	}
	if out, err := runCLI("--request", request, "command"); err == nil || !strings.Contains(string(out), "dependency_unavailable") {
		t.Fatalf("gateway applied original command without application: %v %s", err, out)
	}
	journal, err := harness.OpenJournal(journalPath, discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := journal.Read(ctx, original.CommandID)
	closeErr := journal.Close()
	if err != nil || closeErr != nil || pending.Receipt != nil || !api.Equal(pending.Command, original) {
		t.Fatalf("unavailable original was not durably retained: %v %v", err, closeErr)
	}
	// 查询负责方公开入口，确认gateway没有写Session或原命令决定。
	a, err := bootstrap.OpenAppForRole(ctx, c, false, "management")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err = a.Dispatcher.Lookup(ctx, a.UserAuth, original.CommandID); !api.IsCode(err, "not_found") {
		t.Fatalf("gateway stored a domain command decision while application was absent: %v", err)
	}
	query.QueryID = api.NewID("query")
	if _, err = a.Dispatcher.Query(ctx, a.UserAuth, api.Raw(query)); !api.IsCode(err, "not_found") {
		t.Fatalf("gateway created a Session while application was absent: %v", err)
	}
	start(t, application, env, "--config", configPath)
	// 等待真正应用可读取原owner；新query_id只表示新的读取，不改变原命令。
	for {
		query.QueryID = api.NewID("query")
		_, err = transport.Call(ctx, "query", api.Raw(query))
		if api.IsCode(err, "not_found") {
			break
		}
		if !api.IsCode(err, "dependency_unavailable") {
			t.Fatalf("application did not restore original route: %v", err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("application did not become ready")
		case <-time.After(100 * time.Millisecond):
		}
	}
	out, err := runCLI("recover")
	if err != nil {
		t.Fatalf("original command recovery failed: %v %s", err, out)
	}
	var recovered struct {
		Receipts []api.Receipt `json:"receipts"`
		Partial  bool          `json:"partial"`
	}
	if err = api.Decode(out, &recovered); err != nil || recovered.Partial || len(recovered.Receipts) != 1 || recovered.Receipts[0].CommandID != original.CommandID || recovered.Receipts[0].Stage != "applied" {
		t.Fatalf("recovery changed original decision: %v %s", err, out)
	}
	receipt, err := a.Dispatcher.Lookup(ctx, a.UserAuth, original.CommandID)
	if err != nil || !api.Equal(receipt, recovered.Receipts[0]) {
		t.Fatalf("application did not preserve user's original receipt: %v", err)
	}
	if _, err = a.Dispatcher.Lookup(ctx, a.ServiceAuth, original.CommandID); !api.IsCode(err, "forbidden") {
		t.Fatalf("application attributed user command to service principal: %v", err)
	}
	query.QueryID = api.NewID("query")
	out, err = transport.Call(ctx, "query", api.Raw(query))
	var view interaction.SessionView
	if err != nil || api.Decode(out, &view) != nil || view.Session.SessionID != session || view.Session.Revision != 1 || len(view.Branches) != 1 || view.Branches[0].BranchID != branch {
		t.Fatalf("original Session was not created exactly once: %v %s", err, out)
	}
	query.QueryID = api.NewID("query")
	if _, err = a.Dispatcher.Query(ctx, a.ServiceAuth, api.Raw(query)); !api.IsCode(err, "forbidden") {
		t.Fatalf("application Session belongs to service principal: %v", err)
	}
	journal, err = harness.OpenJournal(journalPath, discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	stored, err := journal.Read(ctx, original.CommandID)
	if err != nil || !api.Equal(stored.Command, original) || stored.Receipt == nil || !api.Equal(*stored.Receipt, receipt) {
		t.Fatalf("recovery refreshed original identity/deadline/body: %v", err)
	}
}
