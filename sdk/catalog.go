package sdk

import (
	"context"
	"encoding/json"
	"lerna/authorization"
	"lerna/catalog"
	wire "lerna/gen/harness/v1"
	"lerna/internal/catalogwire"
)

func (c *CapabilityClient) List(ctx context.Context, q catalog.Query) (catalog.Page, error) {
	return c.catalogQuery(ctx, q, false)
}
func (c *CapabilityClient) Search(ctx context.Context, q catalog.Query) (catalog.Page, error) {
	return c.catalogQuery(ctx, q, true)
}
func (c *CapabilityClient) catalogQuery(ctx context.Context, q catalog.Query, search bool) (catalog.Page, error) {
	if q.Limit < 1 || q.Limit > 64 || q.Budget < 1 || q.Budget > 4096 {
		return catalog.Page{}, &authorization.Error{Code: authorization.Invalid}
	}
	in := &wire.CapabilityRequest{Body: &wire.CapabilityRequest_List{List: catalogwire.Query(q)}}
	if search {
		in.Body = &wire.CapabilityRequest_Search{Search: catalogwire.Query(q)}
	}
	out, e := c.exchange(ctx, in)
	if e != nil {
		return catalog.Page{}, e
	}
	p := out.GetCatalogPage()
	if p == nil || len(p.Items) > q.Limit || p.Scanned > uint32(q.Budget) || p.SchemaLoads != 0 || len(p.Cursor) > 4096 || (p.Coverage != "COMPLETE" && p.Coverage != "PARTIAL") || len(p.Limitations) > 8 {
		return catalog.Page{}, &authorization.Error{Code: authorization.Invalid}
	}
	for _, m := range p.Items {
		if m.Ref == nil || len(m.Ref.Digest) != 64 || m.Ref.Name == "" || m.Ref.Version == "" || m.Ref.Namespace == "" {
			return catalog.Page{}, &authorization.Error{Code: authorization.Invalid}
		}
	}
	return catalogwire.DecodePage(p), nil
}
func (c *CapabilityClient) Describe(ctx context.Context, ref catalog.Ref) (catalog.Entry, error) {
	out, e := c.exchange(ctx, &wire.CapabilityRequest{Body: &wire.CapabilityRequest_Describe{Describe: catalogwire.Ref(ref)}})
	if e != nil {
		return catalog.Entry{}, e
	}
	d := out.GetCatalogDeclaration()
	var entry catalog.Entry
	if d == nil || len(d.DeclarationJson) > 65536 || json.Unmarshal(d.DeclarationJson, &entry) != nil || entry.Ref != ref || catalogwire.DecodeRef(d.Ref) != ref || entry.Capability.Digest() != ref.Digest || entry.Capability.Name != ref.Name || entry.Capability.Version != ref.Version || entry.Capability.Implementation != ref.Implementation || entry.Capability.ImplementationVersion != ref.ImplementationVersion {
		return catalog.Entry{}, &authorization.Error{Code: authorization.Invalid}
	}
	return entry, nil
}
