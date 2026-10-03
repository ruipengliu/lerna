package executor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

type RemoteConfig struct {
	DatabaseID    string `json:"database_id"`
	Endpoint      string `json:"endpoint"`
	OwnerID       string `json:"owner_id"`
	InstanceID    string `json:"instance_id"`
	AuthorityID   string `json:"authority_id"`
	TenantID      string `json:"tenant_id"`
	TLSCAFile     string `json:"tls_ca_file"`
	PeerTokenFile string `json:"peer_token_file"`
	JournalRoot   string `json:"journal_root"`
}
type Client struct {
	Config    RemoteConfig
	SDK       *harness.Client
	transport *harness.GRPCTransport
	journal   *harness.FileJournal
}

// Contracts 是本版本设备的静态登记，构造不打开目标、数据库或网络。
func Contracts() ([]api.MethodContract, error) {
	r := runtime.NewRegistry()
	s, err := execution.New(execution.Config{OwnerID: "executor_00000000000000000000000000000000", Location: "device", Drivers: []execution.Driver{&target.FileDriver{}, &target.FileDriver{ReadOnly: true}}})
	if err != nil {
		return nil, err
	}
	if err = s.Register(r); err != nil {
		return nil, err
	}
	h := &Host{Registry: r}
	if err = h.register(); err != nil {
		return nil, err
	}
	return r.Contracts(), nil
}
func Dial(ctx context.Context, c RemoteConfig) (client *Client, err error) {
	if !api.ValidID(c.DatabaseID) || !api.ValidID(c.TenantID) || !api.ValidID(c.OwnerID) || !api.ValidID(c.AuthorityID) || !api.ValidID(c.InstanceID) || c.OwnerID == c.AuthorityID || !filepath.IsAbs(c.TLSCAFile) || !filepath.IsAbs(c.PeerTokenFile) || !filepath.IsAbs(c.JournalRoot) {
		return nil, api.E("unsupported", "remote_executor_unconfigured")
	}
	token, err := privateToken(c.PeerTokenFile)
	if err != nil {
		return nil, err
	}
	ca, err := os.ReadFile(c.TLSCAFile)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, api.E("forbidden", "device_tls_ca_not_paired")
	}
	methods, err := Contracts()
	if err != nil {
		return nil, err
	}
	digest, err := api.DigestLimit(methods, 1<<20)
	if err != nil {
		return nil, err
	}
	scope := c.TenantID + "/" + c.AuthorityID + "/" + c.OwnerID + "/" + c.InstanceID + "/" + c.DatabaseID
	discovery := harness.Discovery{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: c.OwnerID, SchemaDigest: api.CoreDigest(), Methods: methods, MethodsDigest: digest, IdentityScope: scope, Limits: harness.Limits{MaxDomainBytes: api.MaxJSONBytes, MaxFrameBytes: 1 << 20, MaxPending: 4096}}
	j, err := harness.OpenJournal(c.JournalRoot, scope)
	if err != nil {
		return nil, err
	}
	transport, err := harness.DialGRPC(ctx, c.Endpoint, token, discovery, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}, false)
	if err != nil {
		return nil, errors.Join(err, j.Close())
	}
	sdk, err := harness.NewClient(transport, j, discovery)
	if err != nil {
		return nil, errors.Join(err, transport.Close(), j.Close())
	}
	return &Client{Config: c, SDK: sdk, transport: transport, journal: j}, nil
}
func (c *Client) Close() error {
	var err error
	if c.transport != nil {
		err = errors.Join(err, c.transport.Close())
		c.transport = nil
	}
	if c.journal != nil {
		err = errors.Join(err, c.journal.Close())
		c.journal = nil
	}
	return err
}
func commandFor(owner, id, method, targetID, expires string, payload any) api.Command {
	return api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: owner, CommandID: id, Method: method, TargetID: targetID, ExpiresAt: expires, Payload: api.Raw(payload)}
}
func applied(receipt api.Receipt, err error) error {
	if err != nil {
		return err
	}
	if receipt.Error != nil {
		return receipt.Error
	}
	if receipt.Stage != "applied" {
		return api.E("dependency_unavailable", "device_original_command_pending")
	}
	return nil
}

type ReadContent func(context.Context, ContentPermission) ([]byte, error)

// Prepare 在原五秒控制窗签发之前完成；每个原cache命令先由GoSDK fsync journal。
func (c *Client) Prepare(ctx context.Context, b AdmissionBundle, read ReadContent) error {
	if b.DeviceDatabaseID != c.Config.DatabaseID || b.EndpointID != c.Config.OwnerID || b.InstanceID != c.Config.InstanceID || b.AuthorityID != c.Config.AuthorityID || b.Principal.TenantID != c.Config.TenantID || read == nil {
		return api.E("forbidden", "remote_admission_binding_mismatch")
	}
	if err := validateBundle(b); err != nil {
		return err
	}
	command := commandFor(b.EndpointID, apiID("command", b.BundleID+"/install"), "executor.admission.install", b.Intent.OperationID, b.StartBefore, b)
	if err := applied(c.SDK.Send(ctx, command)); err != nil {
		return err
	}
	for _, permission := range b.Contents {
		var status ContentOutput
		if err := c.query(ctx, "executor.content.status", permission.ContentRef.ContentID, ContentID{permission.ContentRef}, &status); err != nil {
			return err
		}
		if status.Complete {
			continue
		}
		raw, err := read(ctx, permission)
		if err != nil {
			return err
		}
		if uint64(len(raw)) != permission.ContentRef.ByteLength || api.Hash(raw) != permission.ContentRef.Hash {
			return api.E("forbidden", "original_remote_content_bytes_mismatch")
		}
		for index := uint64(0); index < chunkCount(permission.ContentRef.ByteLength); index++ {
			start := index * ChunkBytes
			end := start + ChunkBytes
			if end > uint64(len(raw)) {
				end = uint64(len(raw))
			}
			in := StageInput{BundleID: b.BundleID, ContentRef: permission.ContentRef, ChunkIndex: index, ChunkCount: chunkCount(permission.ContentRef.ByteLength), DataBase64: base64.StdEncoding.EncodeToString(raw[start:end])}
			command = commandFor(b.EndpointID, apiID("command", b.BundleID+"/"+contentKey(permission.ContentRef)+"/"+apiString(index)), "executor.content.stage", permission.ContentRef.ContentID, b.StartBefore, in)
			if err = applied(c.SDK.Send(ctx, command)); err != nil {
				return err
			}
		}
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var view AdmissionView
		if err := c.query(ctx, "executor.admission.get", b.Intent.OperationID, AdmissionID{b.BundleID}, &view); err != nil {
			return err
		}
		if view.Denied {
			return api.E("forbidden", "remote_admission_known_revoked")
		}
		if view.Complete {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (c *Client) Dispatch(ctx context.Context, original api.Command, delivery ControlDelivery) (api.Receipt, error) {
	if original.LogicalServiceID != c.Config.OwnerID || original.Method != "execution.invoke" || delivery.Snapshot.OrchestratorID != c.Config.AuthorityID {
		return api.Receipt{}, api.E("forbidden", "original_remote_dispatch_mismatch")
	}
	control := commandFor(original.LogicalServiceID, apiID("command", delivery.Snapshot.WindowID+"/control-install"), "executor.control.install", delivery.Snapshot.TaskID, original.ExpiresAt, delivery)
	if err := applied(c.SDK.Send(ctx, control)); err != nil {
		return api.Receipt{}, err
	}
	return c.SDK.Send(ctx, original)
}

// Send 沿已冻结的原 command 发送 cancel/reconcile；不补默认值或刷新过期时间。
func (c *Client) Send(ctx context.Context, original api.Command) (api.Receipt, error) {
	return c.SDK.Send(ctx, original)
}
func (c *Client) Control(ctx context.Context, original api.Command, delivery ControlDelivery) (api.Receipt, error) {
	if original.Method != "execution.control" || original.TargetID != delivery.Snapshot.TaskID {
		return api.Receipt{}, api.E("forbidden", "original_remote_control_mismatch")
	}
	install := commandFor(c.Config.OwnerID, apiID("command", delivery.Snapshot.WindowID+"/control-install"), "executor.control.install", delivery.Snapshot.TaskID, original.ExpiresAt, delivery)
	if err := applied(c.SDK.Send(ctx, install)); err != nil {
		return api.Receipt{}, err
	}
	return c.SDK.Send(ctx, original)
}
func (c *Client) query(ctx context.Context, method, targetID string, payload any, out any) error {
	query := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: c.Config.OwnerID, QueryID: api.NewID("query"), Method: method, TargetID: targetID, Payload: api.Raw(payload)}
	raw, err := c.SDK.Query(ctx, query)
	if err != nil {
		return err
	}
	return api.Decode(raw, out)
}
func (c *Client) Get(ctx context.Context, operationID string) (execution.OperationView, error) {
	var out execution.OperationView
	err := c.query(ctx, "execution.get", operationID, execution.OperationIDInput{OperationID: operationID}, &out)
	return out, err
}
func (c *Client) Usage(ctx context.Context, operationID string) (api.UsageSnapshot, error) {
	var out api.UsageSnapshot
	err := c.query(ctx, "execution.usage.get", operationID, execution.OperationIDInput{OperationID: operationID}, &out)
	return out, err
}
func (c *Client) LeaseUsage(ctx context.Context, leaseID string) (SignedLeaseReport, error) {
	var out SignedLeaseReport
	err := c.query(ctx, "executor.lease.usage.get", leaseID, LeaseID{leaseID}, &out)
	if err == nil && (out.SourceDatabaseID != c.Config.DatabaseID || out.Report.EndpointID != c.Config.OwnerID || out.Report.InstanceID != c.Config.InstanceID || out.Report.LeaseRef.OwnerID != c.Config.AuthorityID || out.Report.LeaseRef.ObjectID != leaseID) {
		err = api.E("forbidden", "original_remote_lease_source_mismatch")
	}
	return out, err
}

// ReadBytes 核各块元数据和独立全量hash；调用方必须再走原source注册/当前gate。
func (c *Client) ReadBytes(ctx context.Context, ref api.ContentRef) ([]byte, ContentPermission, error) {
	if ref.TenantID != c.Config.TenantID || ref.OwnerID != c.Config.OwnerID || ref.ByteLength > MaxContentBytes {
		return nil, ContentPermission{}, api.E("forbidden", "remote_output_scope_mismatch")
	}
	raw := make([]byte, 0, ref.ByteLength)
	var permission ContentPermission
	for index := uint64(0); index < chunkCount(ref.ByteLength); index++ {
		var chunk ContentChunk
		if err := c.query(ctx, "executor.content.get", ref.ContentID, ContentGet{ContentRef: ref, ChunkIndex: index}, &chunk); err != nil {
			return nil, permission, err
		}
		if !api.Equal(chunk.Permission.ContentRef, ref) || chunk.ChunkIndex != index || chunk.ChunkCount != chunkCount(ref.ByteLength) {
			return nil, permission, api.E("forbidden", "remote_content_chunk_binding_mismatch")
		}
		if index == 0 {
			permission = chunk.Permission
		} else if !api.Equal(permission, chunk.Permission) {
			return nil, permission, api.E("forbidden", "remote_source_metadata_changed")
		}
		part, err := base64.StdEncoding.Strict().DecodeString(chunk.DataBase64)
		if err != nil || len(part) > ChunkBytes {
			return nil, permission, api.E("invalid_request", "remote_content_chunk_invalid")
		}
		raw = append(raw, part...)
	}
	if uint64(len(raw)) != ref.ByteLength || api.Hash(raw) != ref.Hash {
		return nil, permission, api.E("forbidden", "remote_original_bytes_changed")
	}
	return raw, permission, nil
}
