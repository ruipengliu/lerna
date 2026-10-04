// Package decision_engine stores Decision-owner facts in an independent schema.
package decision_engine

import (
 "context"
 "crypto/sha256"
 "database/sql"
 _ "embed"
 "encoding/hex"
 "encoding/json"
 "errors"
 "time"

 "github.com/ruipengliu/lerna/adapters/postgres"
 "github.com/ruipengliu/lerna/adapters/postgres/internal/pgstore"
 decision "github.com/ruipengliu/lerna/components/decision_engine"
 "github.com/ruipengliu/lerna/contract"
 v "github.com/ruipengliu/lerna/contract/v1_1"
 "github.com/ruipengliu/lerna/runtime"
 "github.com/ruipengliu/lerna/runtime/workpool"
)
type Store struct{core *pgstore.Core;schema string}
func Open(ctx context.Context,cfg postgres.Config)(*Store,error){core,err:=pgstore.Open(ctx,cfg);if err!=nil{return nil,err};return &Store{core:core,schema:cfg.Schema},nil}
func(s *Store)Close()error{return s.core.Close()}
func(s *Store)CreateSchema(ctx context.Context)error{return s.core.CreateSchema(ctx)}
func(s *Store)DropTestSchema(ctx context.Context)error{return s.core.DropTestSchema(ctx)}
func(s *Store)Within(ctx context.Context,owner contract.OwnerRef,fn func(context.Context,runtime.Tx)error)error{return s.core.Within(ctx,owner,fn)}
func(s *Store)Now(ctx context.Context,tx runtime.Tx)(time.Time,error){return s.core.Now(ctx,tx)}
func(s *Store)Settings(ctx context.Context,tx runtime.Tx)(pgstore.Settings,error){return s.core.Settings(ctx,tx)}
func(s *Store)table(name string)string{return s.core.Table(name)}
func owner(ref v.DecisionRef)contract.OwnerRef{return contract.OwnerRef{TenantID:contract.ID(ref.TenantID),OwnerID:contract.ID(ref.OwnerID)}}
func commandOwner(ref v.CommandRef)contract.OwnerRef{return contract.OwnerRef{TenantID:contract.ID(ref.Owner.TenantID),OwnerID:contract.ID(ref.Owner.OwnerID)}}
func(s *Store)key(kind string,tenant,owned,id v.ID)string{data,_:=json.Marshal([]string{s.schema,kind,string(tenant),string(owned),string(id)});return string(data)}
func(s *Store)LockDecision(ctx context.Context,token runtime.Tx,ref v.DecisionRef)(*decision.Record,error){
 tx,err:=s.core.SQL(ctx,token,owner(ref));if err!=nil{return nil,err}
 if _,err=tx.ExecContext(ctx,`SELECT pg_advisory_xact_lock(2,hashtext($1))`,s.key("decision",ref.TenantID,ref.OwnerID,ref.ID));err!=nil{return nil,err}
 return s.ReadDecision(ctx,token,ref)
}
func(s *Store)ReadDecision(ctx context.Context,token runtime.Tx,ref v.DecisionRef)(*decision.Record,error){
 if _,err:=v.Encode(ref);err!=nil{return nil,err}
 tx,err:=s.core.SQL(ctx,token,owner(ref));if err!=nil{return nil,err};var data []byte
 err=tx.QueryRowContext(ctx,`SELECT body FROM `+s.table("decisions")+` WHERE tenant_id=$1 AND owner_id=$2 AND decision_id=$3`,ref.TenantID,ref.OwnerID,ref.ID).Scan(&data)
 if errors.Is(err,sql.ErrNoRows){return nil,nil};if err!=nil{return nil,err}
 var record decision.Record;if err=json.Unmarshal(data,&record);err!=nil{return nil,err};if record.Ref!=ref{return nil,runtime.ErrScope}
 public,err:=record.Public();if err!=nil{return nil,err};if _,err=v.Encode(public);err!=nil{return nil,err};return &record,nil
}
func(s *Store)SaveDecision(ctx context.Context,token runtime.Tx,record decision.Record)error{
 tx,err:=s.core.SQL(ctx,token,owner(record.Ref));if err!=nil{return err}
 public,err:=record.Public();if err!=nil{return err};if _,err=v.Encode(public);err!=nil{return err}
 data,err:=json.Marshal(record);if err!=nil{return err};var deadline *time.Time
 if record.Input!=nil{value,err:=time.Parse("2006-01-02T15:04:05.000000Z",string(record.Input.Deadline));if err!=nil{return err};deadline=&value}
 result,err:=tx.ExecContext(ctx,`INSERT INTO `+s.table("decisions")+`(tenant_id,owner_id,decision_id,input_digest,revision,status,deadline,body)VALUES($1,$2,$3,$4,$5,$6,$7,$8)ON CONFLICT(tenant_id,owner_id,decision_id)DO UPDATE SET revision=excluded.revision,status=excluded.status,deadline=excluded.deadline,body=excluded.body WHERE decisions.input_digest=excluded.input_digest AND decisions.revision<excluded.revision AND decisions.status NOT IN('completed','failed','cancelled')`,record.Ref.TenantID,record.Ref.OwnerID,record.Ref.ID,record.InputDigest,record.Revision,record.Status,deadline,data)
 if err!=nil{return err};n,err:=result.RowsAffected();if err==nil&&n!=1{return runtime.ErrClaim};return err
}
func(s *Store)LockCommand(ctx context.Context,token runtime.Tx,ref v.CommandRef)(*decision.CommandRecord,error){
 tx,err:=s.core.SQL(ctx,token,commandOwner(ref));if err!=nil{return nil,err}
 if _,err=tx.ExecContext(ctx,`SELECT pg_advisory_xact_lock(1,hashtext($1))`,s.key("command",ref.Owner.TenantID,ref.Owner.OwnerID,ref.CommandID));err!=nil{return nil,err}
 return s.ReadCommandRecord(ctx,token,ref)
}
func(s *Store)ReadCommandRecord(ctx context.Context,token runtime.Tx,ref v.CommandRef)(*decision.CommandRecord,error){
 tx,err:=s.core.SQL(ctx,token,commandOwner(ref));if err!=nil{return nil,err};var metadata,data []byte;var digest string
 err=tx.QueryRowContext(ctx,`SELECT digest,metadata,receipt FROM `+s.table("command_receipts")+` WHERE tenant_id=$1 AND owner_id=$2 AND command_id=$3`,ref.Owner.TenantID,ref.Owner.OwnerID,ref.CommandID).Scan(&digest,&metadata,&data)
 if errors.Is(err,sql.ErrNoRows){return nil,nil};if err!=nil{return nil,err};var record decision.CommandRecord
 if err=json.Unmarshal(metadata,&record);err!=nil{return nil,err};record.Digest=digest;record.Receipt,err=v.Decode[v.CommandReceipt](data)
 if err!=nil{return nil,err};response:=v.NewCommandGetResponseFound(v.CommandGetResponseFound{CommandRef:ref,Receipt:record.Receipt,Progress:v.NewCommandProgressNone(v.CommandProgressNone{})});if _,err=v.EncodeCommandResponse(response,ref);err!=nil{return nil,err};return &record,nil
}
func(s *Store)SaveCommand(ctx context.Context,token runtime.Tx,ref v.CommandRef,record decision.CommandRecord)error{
 tx,err:=s.core.SQL(ctx,token,commandOwner(ref));if err!=nil{return err};data,err:=v.Encode(record.Receipt);if err!=nil{return err}
 response:=v.NewCommandGetResponseFound(v.CommandGetResponseFound{CommandRef:ref,Receipt:record.Receipt,Progress:v.NewCommandProgressNone(v.CommandProgressNone{})});if _,err=v.EncodeCommandResponse(response,ref);err!=nil{return err}
 metadata,err:=json.Marshal(record);if err!=nil{return err}
 _,err=tx.ExecContext(ctx,`INSERT INTO `+s.table("command_receipts")+`(tenant_id,owner_id,command_id,contract_version,digest,metadata,receipt)VALUES($1,$2,$3,'1.1.0',$4,$5,$6)`,ref.Owner.TenantID,ref.Owner.OwnerID,ref.CommandID,record.Digest,metadata,data);return err
}
//go:embed migrations/0001_decisions.sql
var migration string
func MigrationChecksum()string{digest:=sha256.Sum256([]byte(migration));return "sha256:"+hex.EncodeToString(digest[:])}
type MigrationVersion struct{Version int64;Checksum string}
func(s *Store)Migrate(ctx context.Context)error{return s.Within(ctx,contract.OwnerRef{TenantID:"migration",OwnerID:"decision_engine"},func(ctx context.Context,token runtime.Tx)error{
 tx,err:=s.core.LocalSQL(ctx,token);if err!=nil{return err};if _,err=tx.ExecContext(ctx,`SELECT pg_advisory_xact_lock(0,hashtext($1))`,s.schema+":decision-migration");err!=nil{return err}
 if _,err=tx.ExecContext(ctx,`SELECT set_config('search_path',$1,true)`,`"`+s.schema+`"`);err!=nil{return err}
 if _,err=tx.ExecContext(ctx,`CREATE TABLE IF NOT EXISTS decision_schema_migrations(version bigint PRIMARY KEY,checksum text NOT NULL)`);err!=nil{return err}
 var checksum string;err=tx.QueryRowContext(ctx,`SELECT checksum FROM decision_schema_migrations WHERE version=1`).Scan(&checksum)
 if err==nil{if checksum!=MigrationChecksum(){return errors.New("Decision migration checksum mismatch")};return nil};if !errors.Is(err,sql.ErrNoRows){return err}
 if _,err=tx.ExecContext(ctx,migration);err!=nil{return err};_,err=tx.ExecContext(ctx,`INSERT INTO decision_schema_migrations(version,checksum)VALUES(1,$1)`,MigrationChecksum());return err
})}
func(s *Store)MigrationVersions(ctx context.Context)([]MigrationVersion,error){var out []MigrationVersion;err:=s.Within(ctx,contract.OwnerRef{TenantID:"migration",OwnerID:"decision_engine"},func(ctx context.Context,token runtime.Tx)error{tx,err:=s.core.LocalSQL(ctx,token);if err!=nil{return err};var version MigrationVersion;err=tx.QueryRowContext(ctx,`SELECT version,checksum FROM `+s.table("decision_schema_migrations")+` WHERE version=1`).Scan(&version.Version,&version.Checksum);if err==nil{out=append(out,version)};return err});return out,err}
