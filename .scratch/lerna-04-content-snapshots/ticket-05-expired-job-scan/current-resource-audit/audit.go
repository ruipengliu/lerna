package main

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "os"
 "path/filepath"
 "strconv"
 "strings"
 "syscall"
 "time"

 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgconn"
)

type pathInput struct { Path string `json:"path"`; Dev uint64 `json:"dev"`; Ino uint64 `json:"ino"`; IdentityRegistered bool `json:"identity_registered"`; StickyUnknown bool `json:"sticky_unknown"`; Kinds []string `json:"kinds"` }
type plan struct { Commit string `json:"commit"`; Worktree string `json:"worktree"`; Inputs string `json:"inputs"`; InputsSHA string `json:"inputs_sha256"`; Ledger string `json:"ledger"`; First int `json:"ledger_first_line"`; Last int `json:"ledger_last_line"`; IntervalSHA string `json:"ledger_interval_sha256"`; Schemas []string `json:"schemas"`; BackendPIDs []int32 `json:"backend_pids"`; PGIDs []int `json:"owned_pgids"`; Paths []pathInput `json:"paths"`; Pins map[string]struct{ SHA string `json:"sha256"` } `json:"pins"` }
func digest(b []byte) string { h:=sha256.Sum256(b);return hex.EncodeToString(h[:]) }
func errorFact(stage string,err error) map[string]any { f:=map[string]any{"stage":stage,"type":fmt.Sprintf("%T",err),"deadline":errors.Is(err,context.DeadlineExceeded)};var pg *pgconn.PgError;if errors.As(err,&pg){f["sqlstate"]=pg.Code};return f }
func run(out map[string]any) (retErr error) {
 b,err:=os.ReadFile("/tmp/lerna-05-own-resource-audit-static/prelaunch.json");if err!=nil{return err};var p plan;if err=json.Unmarshal(b,&p);err!=nil{return err}
 out["commit"]=p.Commit;out["prelaunch_sha256"]=digest(b);out["logical_close_inferred"]=false;out["cleanup_performed"]=false
 b,err=os.ReadFile(p.Inputs);if err!=nil{return err};if digest(b)!=p.InputsSHA{return errors.New("audit input pin changed")}
 b,err=os.ReadFile(p.Ledger);if err!=nil{return err};lines:=strings.Split(strings.TrimSuffix(string(b),"\n"),"\n");if p.First<1||p.Last>len(lines)||digest([]byte(strings.Join(lines[p.First-1:p.Last],"\n")+"\n"))!=p.IntervalSHA{return errors.New("original ledger interval changed")}
 for name,pin:=range p.Pins { b,err=os.ReadFile(filepath.Join(p.Worktree,name));if err!=nil{return err};if digest(b)!=pin.SHA{return errors.New("qualified source changed")} };out["all_15_pins_unchanged"]=true
 ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
 fs:=[]map[string]any{};for _,x:=range p.Paths { if err=ctx.Err();err!=nil{return err};f:=map[string]any{"path":x.Path,"registered_dev":x.Dev,"registered_ino":x.Ino,"identity_registered":x.IdentityRegistered,"sticky_unknown":x.StickyUnknown,"kinds":x.Kinds,"cleanup_allowed":false};st,e:=os.Lstat(x.Path);switch {case errors.Is(e,os.ErrNotExist):f["observation"]="absent";case e!=nil:f["observation"]="read_error";f["error"]=errorFact("lstat",e);default:actual,ok:=st.Sys().(*syscall.Stat_t);if !ok{return errors.New("missing actual stat identity")};f["actual_dev"]=actual.Dev;f["actual_ino"]=actual.Ino;f["symlink"]=st.Mode()&os.ModeSymlink!=0;f["observation"]="present_unregistered_identity";if x.IdentityRegistered {f["observation"]="identity_mismatch";if uint64(actual.Dev)==x.Dev&&actual.Ino==x.Ino {f["observation"]="original_identity_present"}}};fs=append(fs,f) };out["filesystem"]=fs
 ids:=map[int]bool{};for _,id:=range p.PGIDs{ids[id]=true};entries,e:=os.ReadDir("/proc");if e!=nil{return e};if len(entries)>32768{return errors.New("proc census bound exceeded")};members:=[]map[string]any{};for _,entry:=range entries{if err=ctx.Err();err!=nil{return err};pid,e:=strconv.Atoi(entry.Name());if e!=nil{continue};b,e=os.ReadFile(filepath.Join("/proc",entry.Name(),"stat"));if errors.Is(e,os.ErrNotExist){continue};if e!=nil{return e};i:=strings.LastIndex(string(b),")");if i<0{return errors.New("proc stat shape")};f:=strings.Fields(string(b)[i+1:]);if len(f)<20{return errors.New("proc stat fields")};group,e:=strconv.Atoi(f[2]);if e!=nil{return e};if ids[group]{members=append(members,map[string]any{"pid":pid,"pgid":group,"state":f[0],"ppid":f[1],"starttime":f[19]})}};out["owned_group_members"]=members
 conn,err:=pgx.Connect(ctx,os.Getenv("LERNA_TEST_POSTGRES_DSN"));if err!=nil{return err}
 defer func(){e:=conn.Close(ctx);out["first_connection_close_returned_nil"]=e==nil;if e!=nil {out["first_connection_close_error"]=errorFact("connection_close",e);retErr=errors.Join(retErr,e)}}()
 out["audit_connection_backend_pid"]=conn.PgConn().PID()
 registration,err:=json.Marshal(map[string]any{"event":"audit_pg_connection_registered","backend_pid":conn.PgConn().PID(),"qualification_commit":p.Commit,"prelaunch_sha256":out["prelaunch_sha256"]});if err!=nil{return err}
 ledger,err:=os.OpenFile(p.Ledger,os.O_WRONLY|os.O_APPEND,0600);if err!=nil{return err}
 _,writeErr:=ledger.Write(append(registration,'\n'));err=errors.Join(writeErr,ledger.Sync(),ledger.Close());if err!=nil{return err};out["audit_connection_registration_fsynced"]=true
 txctx,txcancel:=context.WithTimeout(ctx,3*time.Second);defer txcancel()
 tx,err:=conn.BeginTx(txctx,pgx.TxOptions{AccessMode:pgx.ReadOnly});if err!=nil{return err}
 defer func(){e:=tx.Rollback(ctx);out["transaction_rollback_returned_nil_or_already_closed"]=e==nil||errors.Is(e,pgx.ErrTxClosed);if e!=nil&&!errors.Is(e,pgx.ErrTxClosed){out["rollback_error"]=errorFact("rollback",e);retErr=errors.Join(retErr,e)}}()
 if _,err=tx.Exec(txctx,"SET LOCAL statement_timeout = '2s'; SET LOCAL lock_timeout = '1s'");err!=nil{return err}
 var schemasJSON,backendsJSON string
 if err=tx.QueryRow(txctx,"SELECT COALESCE(json_agg(nspname ORDER BY nspname), '[]'::json)::text FROM pg_namespace WHERE nspname = ANY($1::text[])",p.Schemas).Scan(&schemasJSON);err!=nil{return err}
 if err=tx.QueryRow(txctx,"SELECT COALESCE(json_agg(json_build_object('pid',pid,'state',state) ORDER BY pid), '[]'::json)::text FROM pg_stat_activity WHERE pid = ANY($1::int[])",p.BackendPIDs).Scan(&backendsJSON);err!=nil{return err}
 out["registered_schema_count"]=len(p.Schemas);out["registered_backend_count"]=len(p.BackendPIDs);out["present_owned_schemas"]=json.RawMessage(schemasJSON);out["present_registered_backends"]=json.RawMessage(backendsJSON)
 return nil
}
func main(){out:=map[string]any{"kind":"owner-only bounded readonly resource observation"};err:=run(out);out["completed"]=err==nil;if err!=nil{out["error"]=errorFact("audit",err)};_ = json.NewEncoder(os.Stdout).Encode(out);if err!=nil{os.Exit(1)} }
