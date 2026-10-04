package decision_engine

import (
 "context"
 "time"

 "github.com/ruipengliu/lerna/contract"
 v "github.com/ruipengliu/lerna/contract/v1_1"
 "github.com/ruipengliu/lerna/runtime"
)

type commandReader struct{s *Service}
func(r commandReader)ReadCommand(ctx context.Context,ref v.CommandRef)(v.CommandGetResponse,error){
 var result v.CommandGetResponse
 err:=r.s.config.Store.Within(ctx,oldOwner(ref.Owner),func(ctx context.Context,tx runtime.Tx)error{
  record,err:=r.s.config.Store.ReadCommandRecord(ctx,tx,ref);if err!=nil{return err}
  if record==nil{result=v.NewCommandGetResponseNotFound(v.CommandGetResponseNotFound{CommandRef:ref});return nil}
  result=v.NewCommandGetResponseFound(v.CommandGetResponseFound{CommandRef:ref,Receipt:record.Receipt,Progress:v.NewCommandProgressNone(v.CommandProgressNone{})});return nil
 });return result,err
}
func(s *Service)AuthorizeCommandRead(ctx context.Context,subject v.SubjectBinding,ref v.CommandRef)(bool,error){
 permission,err:=s.authorize(ctx,&subject,v.DecisionRef{TenantID:ref.Owner.TenantID,OwnerID:ref.Owner.OwnerID,Kind:"decision",ID:ref.CommandID},"command.get",nil)
 if err!=nil{return false,err};if err=permissionCurrent(permission,time.Now().UTC());err!=nil{return false,err};return true,nil
}
func(s *Service)ResolveCommandOwner(ctx context.Context,owner v.OwnerRef)(v.ResolvedCommandOwner,error){if err:=finite(ctx);err!=nil{return v.ResolvedCommandOwner{},err};if owner!=s.config.Owner{return v.ResolvedCommandOwner{},ErrForbidden};return v.ResolvedCommandOwner{Owner:owner,Reader:commandReader{s}},nil}
func(s *Service)GetCommand(ctx context.Context,data []byte,trusted *v.SubjectBinding)(v.CommandGetResponse,error){return v.GetCommand(ctx,data,trusted,s,s,time.Now)}

// LegacyReader projects only receipts that the frozen 1.0 codec can represent.
// The old ReadCommandFacts boundary maps any loss to its existing unavailable.
type LegacyReader struct{Service *Service}
func(r LegacyReader)ReadCommand(ctx context.Context,ref contract.CommandRef)(contract.CommandGetResponse,error){
 if r.Service==nil{return contract.CommandGetResponse{},ErrUnavailable}
 current,err:=(commandReader{r.Service}).ReadCommand(ctx,v.CommandRef{Owner:v.OwnerRef{TenantID:v.ID(ref.Owner.TenantID),OwnerID:v.ID(ref.Owner.OwnerID)},CommandID:v.ID(ref.CommandID)})
 if err!=nil{return contract.CommandGetResponse{},err}
 data,err:=v.Encode(current);if err!=nil{return contract.CommandGetResponse{},err}
 return contract.DecodeCommandResponse(data,ref)
}
