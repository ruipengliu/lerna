package catalogwire

import (
	"lerna/catalog"
	wire "lerna/gen/harness/v1"
)

func Ref(r catalog.Ref) *wire.CatalogRef {
	return &wire.CatalogRef{Namespace: r.Namespace, Name: r.Name, Version: r.Version, Implementation: r.Implementation, ImplementationVersion: r.ImplementationVersion, Digest: r.Digest}
}
func DecodeRef(r *wire.CatalogRef) catalog.Ref {
	return catalog.Ref{Namespace: r.GetNamespace(), Name: r.GetName(), Version: r.GetVersion(), Implementation: r.GetImplementation(), ImplementationVersion: r.GetImplementationVersion(), Digest: r.GetDigest()}
}
func Query(q catalog.Query) *wire.CatalogQuery {
	return &wire.CatalogQuery{Text: q.Text, Category: q.Category, ResourceType: q.ResourceType, Purpose: q.Purpose, Location: q.Location, Cursor: q.Cursor, Required: q.Required, Limit: uint32(q.Limit), Budget: uint32(q.Budget)}
}
func DecodeQuery(q *wire.CatalogQuery) catalog.Query {
	return catalog.Query{Text: q.GetText(), Category: q.GetCategory(), ResourceType: q.GetResourceType(), Purpose: q.GetPurpose(), Location: q.GetLocation(), Cursor: q.GetCursor(), Required: q.GetRequired(), Limit: int(q.GetLimit()), Budget: int(q.GetBudget())}
}
func Page(p catalog.Page) *wire.CatalogPage {
	out := &wire.CatalogPage{Cursor: p.Cursor, Revision: p.Revision, IndexRevision: p.IndexRevision, Coverage: p.Coverage, Limitations: p.Limitations, Scanned: uint32(p.Scanned), SchemaLoads: uint32(p.SchemaLoads)}
	for _, m := range p.Items {
		out.Items = append(out.Items, &wire.CatalogMatch{Ref: Ref(m.Ref), Title: m.Title, Category: m.Category, ResourceType: m.ResourceType, Reason: m.Reason, Purpose: m.Purpose, Location: m.Location, Resource: m.Resource, Preconditions: m.Preconditions, Effects: m.Effects, Unsupported: m.Unsupported, Guarantees: m.Guarantees})
	}
	return out
}
func DecodePage(p *wire.CatalogPage) catalog.Page {
	out := catalog.Page{Cursor: p.GetCursor(), Revision: p.GetRevision(), IndexRevision: p.GetIndexRevision(), Coverage: p.GetCoverage(), Limitations: p.GetLimitations(), Scanned: int(p.GetScanned()), SchemaLoads: int(p.GetSchemaLoads()), Items: []catalog.Match{}}
	for _, m := range p.GetItems() {
		out.Items = append(out.Items, catalog.Match{Ref: DecodeRef(m.Ref), Title: m.Title, Category: m.Category, ResourceType: m.ResourceType, Reason: m.Reason, Purpose: m.Purpose, Location: m.Location, Resource: m.Resource, Preconditions: m.Preconditions, Effects: m.Effects, Unsupported: m.Unsupported, Guarantees: m.Guarantees})
	}
	return out
}
