package development

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/executor"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type foreignLocationReadInput struct {
	ContentRef api.ContentRef `json:"content_ref"`
	Purpose    string         `json:"purpose"`
	Location   string         `json:"location"`
}

type foreignLocationReadOutput struct {
	Hash       string `json:"hash"`
	ByteLength uint64 `json:"byte_length"`
}

func registerForeignLocationRead(a *App) error {
	const method = "host.foreign_location_read"
	return a.Registry.Register(runtime.Method{Contract: api.Contract[foreignLocationReadInput, foreignLocationReadOutput](method, "content", "query", false, false), Query: func(ctx context.Context, _ runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query) (any, error) {
		var in foreignLocationReadInput
		if err := api.Decode(q.Payload, &in); err != nil {
			return nil, err
		}
		if q.TargetID != in.ContentRef.ContentID {
			return nil, api.E("invalid_request", "target_mismatch")
		}
		body, err := a.ReadContentBytes(ctx, scope, auth, in.ContentRef, in.Purpose, in.Location)
		if err != nil {
			return nil, err
		}
		return foreignLocationReadOutput{Hash: api.Hash(body), ByteLength: uint64(len(body))}, nil
	}})
}

func foreignLocationRead(ctx context.Context, a *App, ref api.ContentRef, location string) (foreignLocationReadOutput, error) {
	var got foreignLocationReadOutput
	raw, err := a.queryAs(ctx, a.UserAuth, "host.foreign_location_read", ref.ContentID, foreignLocationReadInput{ContentRef: ref, Purpose: "execution_result", Location: location})
	if err != nil {
		return got, err
	}
	err = api.Decode(raw, &got)
	if err == nil && (got.Hash != ref.Hash || got.ByteLength != ref.ByteLength) {
		err = fmt.Errorf("original foreign bytes changed at %s", location)
	}
	return got, err
}

// 由原公开 Cloud Task、实际 TLS 设备读取和原签名结果取得前态。每次
// Dispatcher 查询是独立入口，不能把上次 cloud 正面证明当作本次许可。
func TestForeignReadPreparesRequestedAndActualStorageLocations(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			observed := false
			observe := func(ctx context.Context, a *App, device *executor.Host, facts task.ContextFacts, _ int32) error {
				if observed || len(facts.Operations) != 1 || !facts.Operations[0].Fact.Closed {
					return nil
				}
				operation := facts.Operations[0]
				route, err := a.remoteExecutors.client(ctx, operation.Intent.ExecutorID)
				if err != nil {
					return err
				}
				actual, err := route.Client.Get(ctx, operation.Intent.OperationID)
				if err != nil {
					return err
				}
				if actual.Operation.ResultRef == nil || !actual.ActuallyStopped || !actual.NewAttemptsClosed || !actual.Operation.UsageFinal || len(actual.Attempts.Items) != 1 || actual.Attempts.Items[0].StartedAt == "" || device.Scope.OwnerID != actual.Operation.ResultRef.OwnerID || a.Memory.Location != "cloud" {
					return fmt.Errorf("original signed device result/storage premise missing")
				}
				ref := *actual.Operation.ResultRef
				if err = registerForeignLocationRead(a); err != nil {
					return err
				}
				for _, location := range []string{"cloud", "device"} {
					_, err := foreignLocationRead(ctx, a, ref, location)
					if err != nil {
						return fmt.Errorf("actual foreign read purpose=execution_result requested=%s storage=cloud: %w", location, err)
					}
					if location == "cloud" {
						t.Logf("FOREIGN_LOCATION_POLICY_PREMISE cloud_read=true task=%s operation=%s attempt=%s source=%s original_owner=%s hash=%s length=%d", facts.Task.TaskID, operation.Intent.OperationID, actual.Attempts.Items[0].AttemptID, ref.ContentID, ref.OwnerID, ref.Hash, ref.ByteLength)
					}
				}
				observed = true
				t.Logf("FOREIGN_DUAL_LOCATION_READ task=%s operation=%s attempt=%s source=%s original_owner=%s hash=%s length=%d requested=device storage=cloud", facts.Task.TaskID, operation.Intent.OperationID, actual.Attempts.Items[0].AttemptID, ref.ContentID, ref.OwnerID, ref.Hash, ref.ByteLength)
				return nil
			}
			runRemoteExecutorTask(t, driver, false, false, observe)
			if !observed {
				t.Fatal("actual independent device source was never read at both locations")
			}
		})
	}
}

// 先让原正常 Task/费用、foreign-flow 检查及实际 handlers 退出，再用
// 原配置和准确 DB/凭据重开做拒绝探针；不把新负例 Copy 责任当成已收束。
func TestForeignReadRetainsLocationPolicyAndCurrentSourceClosure(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			var config Config
			var deviceConfig executor.Config
			var originalScope, originalDeviceScope runtime.Scope
			var original task.ContextOperation
			var ref api.ContentRef
			var originalAttempt string
			observe := func(ctx context.Context, a *App, device *executor.Host, facts task.ContextFacts, _ int32) error {
				if ref.ContentID != "" || len(facts.Operations) != 1 || !facts.Operations[0].Fact.Closed {
					return nil
				}
				original = facts.Operations[0]
				route, err := a.remoteExecutors.client(ctx, original.Intent.ExecutorID)
				if err != nil {
					return err
				}
				actual, err := route.Client.Get(ctx, original.Intent.OperationID)
				if err != nil {
					return err
				}
				if actual.Operation.ResultRef == nil || !actual.ActuallyStopped || !actual.NewAttemptsClosed || !actual.Operation.UsageFinal || len(actual.Attempts.Items) != 1 || actual.Attempts.Items[0].StartedAt == "" {
					return fmt.Errorf("original foreign source guard premise missing")
				}
				ref, originalAttempt = *actual.Operation.ResultRef, actual.Attempts.Items[0].AttemptID
				config, deviceConfig, originalScope, originalDeviceScope = a.Config, device.Config, a.Scope, device.Scope
				return nil
			}
			runRemoteExecutorTask(t, driver, false, false, observe)
			if ref.ContentID == "" {
				t.Fatal("actual original source was never observed")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			paths := []string{config.TokenFile, config.KeyFile, deviceConfig.SigningKeyFile, deviceConfig.PeerTokenFile, deviceConfig.TLSCertificateFile, deviceConfig.TLSKeyFile}
			hashes := make([]string, len(paths))
			for i, path := range paths {
				body, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				hashes[i] = api.Hash(body)
			}
			a, err := OpenApp(ctx, config, false)
			if err != nil {
				t.Fatal("original App reopen", err)
			}
			defer func() {
				if err := a.Close(); err != nil {
					t.Error(err)
				}
			}()
			device, err := executor.Open(ctx, deviceConfig, false)
			if err != nil {
				t.Fatal("original device reopen", err)
			}
			defer func() {
				if err := device.Close(); err != nil {
					t.Error(err)
				}
			}()
			if a.Scope != originalScope || device.Scope != originalDeviceScope {
				t.Fatal("original owner/database scope changed")
			}
			runCtx, stop := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { done <- device.Run(runCtx) }()
			defer func() {
				stop()
				select {
				case err := <-done:
					if err != nil {
						t.Error("actual reopened device exit", err)
					}
				case <-time.After(10 * time.Second):
					t.Error("reopened device did not actually exit")
				}
			}()
			roots := x509.NewCertPool()
			ca, err := os.ReadFile(config.RemoteExecutors[0].TLSCAFile)
			if err != nil || !roots.AppendCertsFromPEM(ca) {
				t.Fatal("original paired CA unavailable", err)
			}
			for {
				conn, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", deviceConfig.GRPCAddr, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots})
				if err == nil {
					if err = conn.Close(); err != nil {
						t.Fatal(err)
					}
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("actual original TLS readiness", ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}
			route, err := a.remoteExecutors.client(ctx, original.Intent.ExecutorID)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := route.Client.Get(ctx, original.Intent.OperationID)
			current, taskErr := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, original.Intent.TaskRef.ObjectID)
			if err != nil || taskErr != nil || current.Status != "failed" || current.AccountingOpen || !actual.ActuallyStopped || !actual.NewAttemptsClosed || !actual.Operation.UsageFinal || actual.Operation.ResultRef == nil || *actual.Operation.ResultRef != ref || len(actual.Attempts.Items) != 1 || actual.Attempts.Items[0].AttemptID != originalAttempt || actual.Attempts.Items[0].StartedAt == "" {
				t.Fatalf("original Task/Attempt/known fees changed after reopen: %v %v", err, taskErr)
			}
			for _, budget := range current.Budget {
				if budget.Reserved != "0" {
					t.Fatal("original fee reservation remains open")
				}
			}
			if err = registerForeignLocationRead(a); err != nil {
				t.Fatal(err)
			}
			if _, err = foreignLocationRead(ctx, a, ref, "device"); err != nil {
				t.Fatal("accurate permitted device baseline", err)
			}
			if _, err = foreignLocationRead(ctx, a, ref, "local"); !api.IsCode(err, "forbidden") {
				t.Fatalf("undeclared local location was not forbidden: %v", err)
			}
			identity, err := api.Digest([]any{a.Scope, a.UserAuth.Ref(a.Scope.OwnerID), ref, "execution_result", "local"})
			if err != nil {
				t.Fatal(err)
			}
			copyID, registerID := stableID("copy", identity), stableID("command", identity+"/register")
			var held memory.ForeignHeldCopy
			if _, err = a.Store.Read(ctx, a.Scope, "content.held_copies", copyID, 0, &held); err != nil {
				t.Fatal("actual refused Copy responsibility", err)
			}
			registration, err := route.Client.SDK.Journal.Read(ctx, registerID)
			if err != nil || registration.Receipt == nil || registration.Receipt.Stage != "rejected" || registration.Receipt.Error == nil || registration.Receipt.Error.Code != "forbidden" || held.Reference.ContentRef != ref || held.Reference.Location != "local" || held.Phase != "reference_intent" || held.WriteState != "not_started" || held.ObjectLocation.Key != "" {
				t.Fatalf("original refused registration/Copy responsibility changed: %v", err)
			}
			body := []byte("current closed source with original foreign ancestry")
			derived, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "text/plain", body, []api.ContentRef{ref}, []api.ContentRef{})
			if err != nil {
				t.Fatal("actual ordinary derived publication", err)
			}
			if _, err = foreignLocationRead(ctx, a, derived, "device"); err != nil {
				t.Fatal("actual permitted derived source baseline", err)
			}
			one := uint64(1)
			command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: derived.ContentID, Method: "content.close", ExpectedRevision: &one, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.CloseInput{ContentRef: derived, Reason: "withdraw this exact ordinary source before another device read"})}
			receipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command))
			if err != nil || receipt.Stage != "applied" || receipt.Error != nil {
				t.Fatalf("original source closure was not applied: %v %+v", err, receipt)
			}
			if _, err = foreignLocationRead(ctx, a, derived, "device"); !api.IsCode(err, "forbidden") {
				t.Fatalf("current closed source was not forbidden: %v", err)
			}
			for i, path := range paths {
				body, err := os.ReadFile(path)
				if err != nil || api.Hash(body) != hashes[i] {
					t.Fatalf("original credentials/configuration changed after reopen: %v", err)
				}
			}
			t.Logf("FOREIGN_LOCATION_GUARDS task=%s operation=%s attempt=%s source=%s derived=%s close_command=%s copy=%s register_command=%s register_stage=%s copy_phase=%s copy_known_deny=%t mirror_write=%s original_task_accounting_closed=true original_cfg_credentials_reopened=true unconfigured_local_denied=true current_closed_source_denied=true additional_refused_copy_responsibility_retained=true", current.TaskID, original.Intent.OperationID, originalAttempt, ref.ContentID, derived.ContentID, command.CommandID, copyID, registerID, registration.Receipt.Stage, held.Phase, held.KnownDeny, held.WriteState)
		})
	}
}
