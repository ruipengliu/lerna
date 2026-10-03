// Package executor 装配独立 SQLite 设备；它不持有云端 Task 事务或数据库。
package executor

import (
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

const Namespace = "executor"
const ChunkBytes = 96 << 10
const MaxContentBytes = 16 << 20
const MaterializeJob = "executor.materialize_content"

// Principal 是 authority 签名的原主体，不含凭据。传输 peer 无权自行填写角色。
type Principal struct {
	TenantID             string   `json:"tenant_id"`
	SubjectID            string   `json:"subject_id"`
	CredentialGeneration uint64   `json:"credential_generation"`
	Roles                []string `json:"roles"`
}

func PrincipalOf(a runtime.Auth) Principal {
	return Principal{a.TenantID, a.SubjectID, a.CredentialGeneration, append([]string{}, a.Roles...)}
}
func (p Principal) Auth() runtime.Auth {
	return runtime.Auth{TenantID: p.TenantID, SubjectID: p.SubjectID, CredentialGeneration: p.CredentialGeneration, Roles: append([]string{}, p.Roles...)}
}

// ContentPermission 保存原 owner 的准确来源；设备缓存不得改写 ContentRef。
type ContentPermission struct {
	ContentRef       api.ContentRef   `json:"content_ref"`
	Purposes         []string         `json:"purposes"`
	ProcessedSources []api.ContentRef `json:"processed_sources"`
	DisclosedSources []api.ContentRef `json:"disclosed_sources"`
	RetainUntil      string           `json:"retain_until"`
}
type AdmissionBundle struct {
	BundleID          string                    `json:"bundle_id"`
	Revision          uint64                    `json:"revision"`
	AuthorityID       string                    `json:"authority_id"`
	EndpointID        string                    `json:"endpoint_id"`
	DeviceDatabaseID  string                    `json:"device_database_id"`
	InstanceID        string                    `json:"instance_id"`
	OriginalCommandID string                    `json:"original_command_id"`
	AdmissionHash     string                    `json:"admission_hash"`
	ExecutionHash     string                    `json:"execution_hash"`
	IntentRef         api.ContentRef            `json:"intent_ref"`
	ReservationRef    api.ObjectRef             `json:"reservation_ref"`
	Intent            execution.ExecutionIntent `json:"intent"`
	Principal         Principal                 `json:"principal"`
	LeaseRef          api.ObjectRef             `json:"lease_ref"`
	UseRefs           []api.ObjectRef           `json:"use_refs"`
	Lease             governance.GrantLease     `json:"lease"`
	Contents          []ContentPermission       `json:"contents"`
	IssuedAt          string                    `json:"issued_at"`
	StartBefore       string                    `json:"start_before"`
	Proof             string                    `json:"proof"`
}
type Binding struct {
	CapabilityRef  api.ComponentRef `json:"capability_ref"`
	BindingRef     api.ObjectRef    `json:"binding_ref"`
	InstallLockRef api.ComponentRef `json:"install_lock_ref"`
	Resources      []string         `json:"resources"`
	Actions        []string         `json:"actions"`
}
type AdmissionOutput struct {
	BundleRef    api.ObjectRef `json:"bundle_ref"`
	OperationRef api.ObjectRef `json:"operation_ref"`
	StartBefore  string        `json:"start_before"`
}
type AdmissionID struct {
	BundleID string `json:"bundle_id"`
}
type AdmissionView struct {
	Bundle   AdmissionBundle  `json:"bundle"`
	Complete bool             `json:"complete"`
	Missing  []api.ContentRef `json:"missing"`
	Denied   bool             `json:"denied"`
}
type StageInput struct {
	BundleID   string         `json:"bundle_id"`
	ContentRef api.ContentRef `json:"content_ref"`
	ChunkIndex uint64         `json:"chunk_index"`
	ChunkCount uint64         `json:"chunk_count"`
	DataBase64 string         `json:"data_base64"`
}
type ContentID struct {
	ContentRef api.ContentRef `json:"content_ref"`
}
type ContentOutput struct {
	ContentRef   api.ContentRef `json:"content_ref"`
	Complete     bool           `json:"complete"`
	StagedChunks uint64         `json:"staged_chunks"`
}
type ContentGet struct {
	ContentRef api.ContentRef `json:"content_ref"`
	ChunkIndex uint64         `json:"chunk_index"`
}
type ContentChunk struct {
	Permission ContentPermission `json:"permission"`
	ChunkIndex uint64            `json:"chunk_index"`
	ChunkCount uint64            `json:"chunk_count"`
	DataBase64 string            `json:"data_base64"`
}
type ControlDelivery struct {
	Snapshot api.ControlSnapshot `json:"snapshot"`
	Compact  string              `json:"compact"`
}

// Revocation 用独立签名保存本机已知的原授权/主体撤权；不在设备裁决云端 Task。
type Revocation struct {
	AuthorityID string        `json:"authority_id"`
	EndpointID  string        `json:"endpoint_id"`
	ObjectRef   api.ObjectRef `json:"object_ref"`
	Kind        string        `json:"kind"`
	IssuedAt    string        `json:"issued_at"`
	StartBefore string        `json:"start_before"`
	Proof       string        `json:"proof"`
}
type RevocationOutput struct {
	Ref    api.ObjectRef `json:"ref"`
	Denied bool          `json:"denied"`
}

type LeaseID struct {
	LeaseID string `json:"lease_id"`
}
type LeaseUsageProof struct {
	LeaseRef         api.ObjectRef         `json:"lease_ref"`
	EndpointID       string                `json:"endpoint_id"`
	InstanceID       string                `json:"instance_id"`
	AllocationDigest string                `json:"allocation_digest"`
	LocalLease       governance.GrantLease `json:"local_lease"`
	OperationUsage   api.UsageSnapshot     `json:"operation_usage"`
}
type SignedLeaseReport struct {
	SourceDatabaseID string                 `json:"source_database_id"`
	Report           governance.LeaseReport `json:"report"`
	IssuedAt         string                 `json:"issued_at"`
	StartBefore      string                 `json:"start_before"`
	Proof            string                 `json:"proof"`
}

type admissionRecord struct {
	Bundle AdmissionBundle `json:"bundle"`
	Digest string          `json:"digest"`
}
type contentRecord struct {
	Permission ContentPermission `json:"permission"`
	BundleID   string            `json:"bundle_id"`
	Principal  Principal         `json:"principal"`
	Revision   uint64            `json:"revision"`
	ChunkCount uint64            `json:"chunk_count"`
	Complete   bool              `json:"complete"`
	Published  bool              `json:"published"`
	ObjectKey  string            `json:"object_key"`
}
type stagedChunk struct {
	Index      uint64 `json:"index"`
	Hash       string `json:"hash"`
	DataBase64 string `json:"data_base64"`
}
type denyRecord struct {
	Revocation Revocation `json:"revocation"`
	Digest     string     `json:"digest"`
}
