package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/ruipengliu/lerna/contracts/command"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

const formatDDL = `CREATE TABLE database_format (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1), format_version INTEGER NOT NULL,
 contract_version INTEGER NOT NULL, implementation_profile TEXT NOT NULL,
 user_id TEXT NOT NULL, domain_id TEXT NOT NULL, durability_profile TEXT NOT NULL,
 compiled_digest TEXT NOT NULL, actual_schema_digest TEXT NOT NULL
);`
const formatApplicationID = 0x4c524e41
const formatPragmas = "PRAGMA application_id=0x4c524e41; PRAGMA user_version=1;"
const implementationProfile = "lerna-m1-v1"

// CheckFormatCompatibility 供恢复工具在原数据库 Open 前核验清单声明的受支持格式。
func CheckFormatCompatibility(version, contractVersion uint32, implementation, digest string) error {
	if version != 1 || contractVersion != 1 || implementation != implementationProfile || digest != compiledFormatDigest() {
		return command.Fail("UNSUPPORTED_DATABASE_FORMAT")
	}
	return nil
}

func initialSchema() string {
	return migration + admissionMigration + sessionInputMigration + grantsMigration + egressMigration + completionMigration + budgetMigration + reconciliationMigration + contentGovernanceMigration + resendMigration + modelMigration + traceMigration + taskClosingMigration + reasonerDriverMigration + fileResourcesMigration + cancellationMigration
}

// compiledFormatDigest 同时固定受审表定义和公共契约；不以版本号掩盖未迁移的结构变化。
func compiledFormatDigest() string {
	var descriptors []string
	protoregistry.GlobalFiles.RangeFiles(func(f protoreflect.FileDescriptor) bool {
		if strings.HasPrefix(f.Path(), "lerna/v1/") {
			b, _ := proto.MarshalOptions{Deterministic: true}.Marshal(protodesc.ToFileDescriptorProto(f))
			descriptors = append(descriptors, f.Path()+":"+fmt.Sprintf("%x", sha256.Sum256(b)))
		}
		return true
	})
	sort.Strings(descriptors)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(formatPragmas+formatDDL+initialSchema()+strings.Join(descriptors, "\n"))))
}

// schemaDigest 只观察 SQLite 格式元数据，拒绝后不自动修补业务表或索引。
func schemaDigest(ctx context.Context, q querier) (string, int, error) {
	rows, e := q.QueryContext(ctx, "SELECT type,name,tbl_name,coalesce(sql,'') FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY type,name")
	if e != nil {
		return "", 0, e
	}
	defer rows.Close()
	var entries [][4]string
	for rows.Next() {
		var entry [4]string
		if e = rows.Scan(&entry[0], &entry[1], &entry[2], &entry[3]); e != nil {
			return "", 0, e
		}
		entries = append(entries, entry)
	}
	if e = rows.Err(); e != nil {
		return "", 0, e
	}
	b, _ := json.Marshal(entries)
	return fmt.Sprintf("%x", sha256.Sum256(b)), len(entries), nil
}

func (s *Store) checkFormat(ctx context.Context, q querier) (bool, error) {
	digest, count, e := schemaDigest(ctx, q)
	if e != nil {
		return false, e
	}
	var applicationID, userVersion, schemaVersion int64
	for _, value := range []struct {
		query  string
		target *int64
	}{{"PRAGMA application_id", &applicationID}, {"PRAGMA user_version", &userVersion}, {"PRAGMA schema_version", &schemaVersion}} {
		if e = q.QueryRowContext(ctx, value.query).Scan(value.target); e != nil {
			return false, e
		}
	}
	if count == 0 {
		if applicationID != 0 || userVersion != 0 || schemaVersion != 0 {
			return false, command.Fail("UNSUPPORTED_DATABASE_FORMAT")
		}
		var allObjects int
		if e = q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema").Scan(&allObjects); e != nil {
			return false, e
		}
		if allObjects != 0 {
			return false, command.Fail("UNSUPPORTED_DATABASE_FORMAT")
		}
		return true, nil
	}
	if applicationID != formatApplicationID || userVersion != 1 {
		return false, command.Fail("UNSUPPORTED_DATABASE_FORMAT")
	}

	var marker int
	if e = q.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name='database_format'").Scan(&marker); e != nil {
		return false, e
	}
	if marker != 1 {
		return false, command.Fail("UNSUPPORTED_DATABASE_FORMAT")
	}
	var version, contractVersion uint32
	var user, domain, profile, implementation, compiled, actual string
	e = q.QueryRowContext(ctx, "SELECT format_version,contract_version,implementation_profile,user_id,domain_id,durability_profile,compiled_digest,actual_schema_digest FROM database_format WHERE singleton=1").Scan(&version, &contractVersion, &implementation, &user, &domain, &profile, &compiled, &actual)
	if e != nil {
		return false, command.Fail("UNSUPPORTED_DATABASE_FORMAT")
	}
	if user != s.user || domain != s.domain {
		return false, command.Fail("PERMISSION_DENIED")
	}
	if version != 1 || contractVersion != 1 || profile != "LOCAL" || implementation != implementationProfile || compiled != compiledFormatDigest() || actual != digest {
		return false, command.Fail("UNSUPPORTED_DATABASE_FORMAT")
	}
	var configuredUser, configuredDomain, configuredProfile string
	e = q.QueryRowContext(ctx, "SELECT user_id,domain_id,durability_profile FROM domain_config WHERE singleton=1").Scan(&configuredUser, &configuredDomain, &configuredProfile)
	if e != nil || configuredUser != user || configuredDomain != domain || configuredProfile != profile {
		return false, command.Fail("UNSUPPORTED_DATABASE_FORMAT")
	}
	s.settings.FormatVersion, s.settings.ContractVersion = version, contractVersion
	s.settings.FormatDigest, s.settings.ImplementationProfile = compiled, implementation
	s.settings.DurabilityProfile = profile
	return false, nil
}

func (s *Store) initializeFormat(ctx context.Context) error {
	return s.transact(ctx, "bootstrap", "storage.bootstrap", func(txctx context.Context) error {
		tx, e := s.writer(txctx, "bootstrap")
		if e != nil {
			return e
		}
		empty, e := s.checkFormat(txctx, tx)
		if e != nil || !empty {
			return e
		}
		if _, e = tx.ExecContext(txctx, initialSchema()+formatDDL+formatPragmas); e != nil {
			return e
		}
		if _, e = tx.ExecContext(txctx, "INSERT INTO domain_config VALUES(1,?,?,?)", s.user, s.domain, "LOCAL"); e != nil {
			return e
		}
		digest, _, e := schemaDigest(txctx, tx)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(txctx, "INSERT INTO database_format VALUES(1,1,1,?,?,?,?,?,?)", implementationProfile, s.user, s.domain, "LOCAL", compiledFormatDigest(), digest)
		if e != nil {
			return e
		}
		_, e = s.checkFormat(txctx, tx)
		return e
	})
}
