// Package development 显式装配有界本机参考宿主、受信规则与领域端口。
package development

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Development                bool                      `json:"development"`
	TenantID                   string                    `json:"tenant_id"`
	OwnerID                    string                    `json:"owner_id"`
	SubjectID                  string                    `json:"subject_id"`
	UserRoles                  []string                  `json:"user_roles,omitempty"`
	Driver                     string                    `json:"driver"`
	DatabaseID                 string                    `json:"database_id"`
	DatabasePath               string                    `json:"database_path,omitempty"`
	DSNEnv                     string                    `json:"dsn_env,omitempty"`
	DevDatabaseEnvFile         string                    `json:"dev_database_env_file,omitempty"`
	DataRoot                   string                    `json:"data_root"`
	TokenFile                  string                    `json:"token_file"`
	KeyFile                    string                    `json:"key_file"`
	PolicyExpiresAt            string                    `json:"policy_expires_at"`
	HTTPAddr                   string                    `json:"http_addr"`
	GRPCAddr                   string                    `json:"grpc_addr"`
	Origins                    []string                  `json:"origins"`
	StaticDir                  string                    `json:"static_dir,omitempty"`
	TZDBRoot                   string                    `json:"tzdb_root"`
	TZDBVersion                string                    `json:"tzdb_version"`
	Model                      *ModelConfig              `json:"model,omitempty"`
	Governance                 *BuiltinGovernanceConfig  `json:"governance,omitempty"`
	ActionBindings             []ActionBindingConfig     `json:"action_bindings,omitempty"`
	Information                []InformationSourceConfig `json:"information,omitempty"`
	InformationReferenceAnswer bool                      `json:"information_reference_answer,omitempty"`
	Knowledge                  *KnowledgeConfig          `json:"knowledge,omitempty"`
	WorkerPool                 *ClassifiedWorkerConfig   `json:"worker_pool,omitempty"`
	EndpointChannels           *EndpointChannelConfig    `json:"endpoint_channels,omitempty"`
	WASI                       *WASIConfig               `json:"wasi,omitempty"`
	RemoteAgent                *RemoteAgentConfig        `json:"remote_agent,omitempty"`
	ForeignConsumers           []ForeignConsumerConfig   `json:"foreign_consumers,omitempty"`
	ForeignSourceTLS           *ForeignSourceTLSConfig   `json:"foreign_source_tls,omitempty"`
	RemoteExecutors            []RemoteExecutorConfig    `json:"remote_executors,omitempty"`
}

func LoadConfig(path string) (Config, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return Config{}, e
	}
	var c Config
	e = api.Decode(b, &c)
	if e != nil {
		return c, e
	}
	if e = validateEndpointChannels(c); e != nil {
		return c, e
	}
	if e = validateRemoteAgent(c); e != nil {
		return c, e
	}
	if e = validateForeignConsumers(c); e != nil {
		return c, e
	}
	if !api.ValidID(c.TenantID) || !api.ValidID(c.OwnerID) || !api.ValidID(c.SubjectID) || !filepath.IsAbs(c.DataRoot) || c.DatabaseID == "" {
		return c, api.E("invalid_request", "invalid_process_configuration")
	}
	if !c.Development {
		return c, api.E("unsupported", "company_identity_adapter_not_configured")
	}
	return c, nil
}
func SaveConfig(path string, c Config) error {
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	return privateFile(path, append(b, '\n'))
}
func privateFile(path string, b []byte) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".harness-private-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = os.Rename(f.Name(), path)
	}
	if e != nil {
		return e
	}
	dir, e := os.Open(filepath.Dir(path))
	if e != nil {
		return e
	}
	e = dir.Sync()
	ce = dir.Close()
	if e == nil {
		e = ce
	}
	return e
}
func DevelopmentConfig(root, driver string) Config {
	tzdbRoot := os.Getenv("HARNESS_TEST_TZDB_ROOT")
	if tzdbRoot == "" {
		tzdbRoot = "/usr/share/zoneinfo"
	}
	return Config{Development: true, UserRoles: []string{"trusted_renderer", "grant_authority", "content_admin", "memory_admin", "maintainer", "evidence_consumer", "evaluation_authority", "release_approver", "rollout_observer"}, TenantID: platform.StableDevelopmentID("tenant", "tenant"), OwnerID: platform.StableDevelopmentID("owner", "service"), SubjectID: platform.StableDevelopmentID("subject", "user"), Driver: driver, DatabasePath: filepath.Join(root, "device.sqlite"), DSNEnv: "HARNESS_DATABASE_DSN", DevDatabaseEnvFile: "/workspace/harness-dev-environment/.postgres.env", DataRoot: root, TokenFile: filepath.Join(root, ".identity-token"), KeyFile: filepath.Join(root, ".owner-key.pem"), PolicyExpiresAt: api.Time(time.Now().Add(365 * 24 * time.Hour)), HTTPAddr: "127.0.0.1:8080", GRPCAddr: "127.0.0.1:8081", Origins: []string{"http://127.0.0.1:5173", "http://localhost:5173"}, StaticDir: "/workspace/lerna/apps/web/dist", TZDBRoot: tzdbRoot, TZDBVersion: "2026b"}
}
func DSN(c Config) (string, error) {
	if value := os.Getenv(c.DSNEnv); value != "" {
		return value, nil
	}
	if !c.Development || c.DevDatabaseEnvFile == "" {
		return "", api.E("invalid_state", "database_credentials_unavailable")
	}
	b, e := os.ReadFile(c.DevDatabaseEnvFile)
	if e != nil {
		return "", e
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok {
			values[k] = strings.Trim(v, "\"'")
		}
	}
	if values["POSTGRES_PASSWORD"] == "" {
		return "", api.E("invalid_state", "database_credentials_unavailable")
	}
	return (&url.URL{Scheme: "postgres", User: url.UserPassword(values["POSTGRES_USER"], values["POSTGRES_PASSWORD"]), Host: "127.0.0.1:55432", Path: "/" + values["POSTGRES_DB"], RawQuery: "sslmode=disable"}).String(), nil
}
func OpenStore(ctx context.Context, c Config, migration bool) (runtime.Store, error) {
	switch c.Driver {
	case "postgres":
		dsn, e := DSN(c)
		if e != nil {
			return nil, e
		}
		options := []postgres.Option{postgres.WithMaxConnections(32)}
		if c.DatabaseID != "" {
			options = append(options, postgres.WithExpectedDatabaseID(c.DatabaseID))
		}
		s, e := postgres.Open(ctx, dsn, options...)
		if e != nil {
			return nil, e
		}
		if migration {
			if e = s.Migrate(ctx); e != nil {
				s.Close()
				return nil, e
			}
		}
		return s, nil
	case "sqlite":
		options := []sqlite.Option{}
		if c.DatabaseID != "" {
			options = append(options, sqlite.WithExpectedDatabaseID(c.DatabaseID))
		}
		s, e := sqlite.Open(c.DatabasePath, options...)
		if e != nil {
			return nil, e
		}
		if migration {
			if e = s.Migrate(ctx); e != nil {
				s.Close()
				return nil, e
			}
		}
		return s, nil
	default:
		return nil, api.E("unsupported", "database_driver_not_supported")
	}
}
func InitializeConfig(ctx context.Context, path, root, driver string) (Config, error) {
	if _, e := os.Stat(path); e == nil {
		c, err := LoadConfig(path)
		if err != nil {
			return c, err
		}
		store, err := OpenStore(ctx, c, true)
		if err != nil {
			return c, err
		}
		return c, store.Close()
	} else if !os.IsNotExist(e) {
		return Config{}, e
	}
	c := DevelopmentConfig(root, driver)
	if e := os.MkdirAll(root, 0700); e != nil {
		return c, e
	}
	token := make([]byte, 32)
	if _, e := rand.Read(token); e != nil {
		return c, e
	}
	if e := privateFile(c.TokenFile, []byte(base64.RawURLEncoding.EncodeToString(token))); e != nil {
		return c, e
	}
	s, e := OpenStore(ctx, c, true)
	if e != nil {
		return c, e
	}
	c.DatabaseID = s.ID()
	if e = s.Close(); e != nil {
		return c, e
	}
	return c, SaveConfig(path, c)
}
