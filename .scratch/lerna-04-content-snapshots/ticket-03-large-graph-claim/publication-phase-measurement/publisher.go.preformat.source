package decision

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	engine "github.com/ruipengliu/lerna/components/decision_engine"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	c "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/domain/content"
	compiler "github.com/ruipengliu/lerna/domain/task/context"
	"strconv"
)

func (a *Adapter) publicationRef(key string, body []byte, p engine.Permission) v.ContentRef {
	permission, _ := json.Marshal(p)
	media := "text/plain"
	if _, err := v.ParseJSON(body); err == nil {
		media = "application/json"
	}
	return v.ContentRef{Owner: v.OwnerRef{TenantID: v.ID(a.config.Owner.TenantID), OwnerID: v.ID(a.config.Owner.OwnerID)}, ContentID: v.ID("publication-" + identity(string(permission)+"/"+key)), Version: "1", Hash: hash(body), MediaType: media, ByteLength: v.Revision(strconv.Itoa(len(body)))}
}
func (a *Adapter) PlanPublication(ctx context.Context, key string, body []byte, sources []v.ContentRef, p engine.Permission) (v.ContentRef, error) {
	if key == "" || len(key) > 1024 || len(body) > 262144 || len(sources) > 64 {
		return v.ContentRef{}, engine.ErrInputLimit
	}
	planCurrentStart := content.BeginPublicationPhase(ctx)
	_, planCurrentErr := a.config.Access.Current(ctx, p, "publish")
	content.EndPublicationPhase(ctx, "adapter.plan.current", planCurrentStart, planCurrentErr)
	if err := planCurrentErr; err != nil {
		return v.ContentRef{}, err
	}
	for _, source := range sources {
		if _, err := ToContent(source); err != nil {
			return v.ContentRef{}, err
		}
	}
	ref := a.publicationRef(key, body, p)
	_, err := v.Encode(ref)
	return ref, err
}
func (a *Adapter) Publish(ctx context.Context, key string, body []byte, sources []v.ContentRef, p engine.Permission) (v.ContentRef, error) {
	planStart := content.BeginPublicationPhase(ctx)
	ref, err := a.PlanPublication(ctx, key, body, sources, p)
	content.EndPublicationPhase(ctx, "adapter.publish.plan", planStart, err)
	if err != nil {
		return ref, err
	}
	currentStart := content.BeginPublicationPhase(ctx)
	binding, err := a.config.Access.Current(ctx, p, "publish")
	content.EndPublicationPhase(ctx, "adapter.publish.current", currentStart, err)
	if err != nil {
		return ref, err
	}
	converted, err := ToContent(ref)
	if err != nil {
		return ref, err
	}
	ancestors := make([]c.ContentRef, 0, len(sources))
	for _, source := range sources {
		r, e := ToContent(source)
		if e != nil {
			return ref, e
		}
		ancestors = append(ancestors, r)
	}
	if _, err = a.publishObject(ctx, binding.Input, compiler.Object{Ref: converted, Key: key, Bytes: body, Sources: ancestors}, a.config.Content); err != nil {
		return ref, err
	}
	return ref, nil
}
func (a *Adapter) publishObject(ctx context.Context, in compiler.Input, o compiler.Object, verification compiler.ProcessingContent) (c.ContentRef, error) {
	if verification == nil {
		return o.Ref, engine.ErrUnavailable
	}
	if in.Purpose != a.config.Purpose || o.Ref.Owner != a.config.Owner || len(o.Bytes) > 262144 || hash(o.Bytes) != o.Ref.Hash || string(o.Ref.ByteLength) != strconv.Itoa(len(o.Bytes)) {
		return o.Ref, engine.ErrPublicationConflict
	}
	deadline := c.Time(in.Request.Payload.Deadline)
	request := c.ContentPutRequest{ContractVersion: c.Version, Profile: "content", CommandID: c.ID("context-put-" + identity(o.Key)), Target: c.ContentTarget{TenantID: o.Ref.Owner.TenantID, OwnerID: o.Ref.Owner.OwnerID, Kind: "content", ID: o.Ref.ContentID}, Method: "content.put", AcceptBefore: deadline, Payload: c.ContentPutPayload{ContentRef: o.Ref, Sources: o.Sources, Purpose: c.Purpose(a.config.Purpose), RetainUntil: c.Time(in.RetainUntil), BytesBase64: base64.StdEncoding.EncodeToString(o.Bytes)}}
	// The owner journal saves this exact request before any Content send. Replay
	// returns the original bytes and cannot substitute a renewed absolute cutoff.
	stageStart := content.BeginPublicationPhase(ctx)
	raw, err := a.config.Access.StagePublication(ctx, in, request)
	content.EndPublicationPhase(ctx, "adapter.stage", stageStart, err)
	if err != nil {
		return o.Ref, err
	}
	prepareStart := content.BeginPublicationPhase(ctx)
	err = a.config.Access.PrepareContent(ctx, in, o.Ref)
	content.EndPublicationPhase(ctx, "adapter.prepare", prepareStart, err)
	if err != nil {
		return o.Ref, err
	}
	putStart := content.BeginPublicationPhase(ctx)
	response, err := a.config.Content.Put(ctx, raw, &a.config.Subject)
	content.EndPublicationPhase(ctx, "adapter.put", putStart, err)
	if err != nil {
		return o.Ref, err
	}
	received, ok := response.AsReceived()
	if !ok {
		return o.Ref, engine.ErrUnavailable
	}
	if _, ok = received.Receipt.AsAccepted(); !ok {
		return o.Ref, engine.ErrForbidden
	}
	command := c.CommandRef{Owner: a.config.Owner, CommandID: request.CommandID}
	query, err := c.Encode(c.CommandGetRequest{ContractVersion: c.Version, Profile: "command", CommandID: "context-publication-query", Target: c.CommandTarget{TenantID: a.config.Owner.TenantID, OwnerID: a.config.Owner.OwnerID, Kind: "command", ID: request.CommandID}, Method: "command.get", AcceptBefore: deadline, Payload: c.CommandGetPayload{CommandRef: command}})
	if err != nil {
		return o.Ref, err
	}
	for step := 0; step < 16; step++ {
		if err = ctx.Err(); err != nil {
			return o.Ref, err
		}
		getStart := content.BeginPublicationPhase(ctx)
		current, e := a.config.Content.GetCommand(ctx, query, &a.config.Subject)
		content.EndPublicationPhase(ctx, "adapter.get_command", getStart, e)
		if e != nil {
			return o.Ref, e
		}
		found, ok := current.AsFound()
		if !ok {
			return o.Ref, engine.ErrUnavailable
		}
		progress, ok := found.Progress.AsContent()
		if !ok || progress.ContentRef != o.Ref {
			return o.Ref, engine.ErrUnavailable
		}
		if progress.Publication == "published" {
			verificationStart := content.BeginPublicationPhase(ctx)
			read, e := verification.ReadForProcessing(ctx, &a.config.Subject, o.Ref, a.config.Purpose, int64(len(o.Bytes)))
			content.EndPublicationPhase(ctx, "adapter.verification", verificationStart, e)
			if e != nil {
				return o.Ref, mapContentError(e)
			}
			if !bytes.Equal(read.Bytes, o.Bytes) {
				return o.Ref, engine.ErrPublicationConflict
			}
			return o.Ref, nil
		}
		if progress.Publication != "preparing" {
			return o.Ref, engine.ErrForbidden
		}
		stepStart := content.BeginPublicationPhase(ctx)
		_, err = a.config.Content.Step(ctx)
		content.EndPublicationPhase(ctx, "adapter.step", stepStart, err)
		if err != nil {
			return o.Ref, err
		}
	}
	return o.Ref, engine.ErrUnavailable
}
func (a *Adapter) ReadPublished(ctx context.Context, ref v.ContentRef, p engine.Permission) ([]byte, error) {
	readCurrentStart := content.BeginPublicationPhase(ctx)
	_, readCurrentErr := a.config.Access.Current(ctx, p, "publish")
	content.EndPublicationPhase(ctx, "adapter.read.current", readCurrentStart, readCurrentErr)
	if err := readCurrentErr; err != nil {
		return nil, err
	}
	exact, err := ToContent(ref)
	if err != nil {
		return nil, err
	}
	readStart := content.BeginPublicationPhase(ctx)
	read, err := a.config.Content.ReadForProcessing(ctx, &a.config.Subject, exact, a.config.Purpose, 262144)
	content.EndPublicationPhase(ctx, "adapter.read.processing", readStart, err)
	return read.Bytes, mapContentError(err)
}
func mapContentError(err error) error {
	if errors.Is(err, content.ErrProcessingLimit) {
		return errors.Join(engine.ErrInputLimit, err)
	}
	if errors.Is(err, content.ErrProcessingForbidden) {
		return errors.Join(engine.ErrForbidden, err)
	}
	if err != nil {
		return errors.Join(engine.ErrUnavailable, err)
	}
	return nil
}
