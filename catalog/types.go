// Package catalog exposes bounded, authorized capability discovery independently
// of its durable directory, index and execution bindings.
package catalog

import (
	"context"
	"lerna/execution"
	"time"
)

type Ref struct{ Namespace, Name, Version, Implementation, ImplementationVersion, Digest string }
type Source struct {
	Kind, Key string
	Revision  uint64
}
type Entry struct {
	Source                                                                                                        Source
	Ref                                                                                                           Ref
	Title, Category                                                                                               string
	Aliases                                                                                                       []string
	Purpose, Location, Resource, DiscoveryResource, ResourceType, Preconditions, Effects, Unsupported, Guarantees string
	Available                                                                                                     bool
	Capability                                                                                                    execution.Capability
}
type Summary struct {
	Source                                                                                                        Source
	Ref                                                                                                           Ref
	Title, Category                                                                                               string
	Aliases                                                                                                       []string
	Purpose, Location, Resource, DiscoveryResource, ResourceType, Preconditions, Effects, Unsupported, Guarantees string
	Available                                                                                                     bool
}

func (e Entry) Summary() Summary {
	return Summary{Source: e.Source, Ref: e.Ref, Title: e.Title, Category: e.Category, Aliases: append([]string(nil), e.Aliases...), Purpose: e.Purpose, Location: e.Location, Resource: e.Resource, DiscoveryResource: e.DiscoveryResource, ResourceType: e.ResourceType, Preconditions: e.Preconditions, Effects: e.Effects, Unsupported: e.Unsupported, Guarantees: e.Guarantees, Available: e.Available}
}

type QueryContext struct{ Namespace, Subject, Token, Purpose, Location, Action string }
type Policy struct {
	Resource, Purpose, Location string
	Source                      Source
}

func (s Summary) Policy() Policy {
	r := s.DiscoveryResource
	if r == "" {
		r = s.Resource
	}
	return Policy{Resource: r, Purpose: s.Purpose, Location: s.Location, Source: s.Source}
}

type Access struct {
	Namespace, Subject, Revision string
	Allowed                      []bool
}
type Authorization interface {
	View(context.Context, QueryContext, []Policy) (Access, error)
}
type Query struct {
	Text, Category, ResourceType, Purpose, Location, Cursor string
	Required                                                []string
	Limit, Budget                                           int
}
type Match struct {
	Ref                                                                          Ref
	Title, Category, ResourceType, Reason                                        string
	Purpose, Location, Resource, Preconditions, Effects, Unsupported, Guarantees string
}
type Page struct {
	Items         []Match
	Cursor        string
	Revision      uint64
	IndexRevision uint64
	Coverage      string
	Limitations   []string
	Scanned       int
	SchemaLoads   int
}
type Snapshot struct {
	Revision uint64
	Rows     []Summary
	After    string
	More     bool
}
type IndexResult struct {
	Revision uint64
	Terms    map[string]string
	Status   string
}
type Store interface {
	Snapshot(context.Context, string, int) (Snapshot, error)
	LookupSummary(context.Context, Ref) (Summary, uint64, error)
	Describe(context.Context, Ref) (Entry, uint64, error)
	Index(context.Context, []Ref, uint64) (IndexResult, error)
	CursorKey(context.Context) ([]byte, error)
	WithVersion(context.Context, Ref, func(Entry) error) error
}
type Config struct {
	MaxScan, MaxPage, MaxCandidates int
	Timeout                         time.Duration
}

// Ranking receives only authorized summaries and text. It must honor context
// cancellation; it cannot read hidden entries or invoke an unbounded model loop.
type Ranking interface {
	Score(context.Context, Query, Summary, string) (int, error)
}
type KeywordRanking struct{}

func (KeywordRanking) Score(ctx context.Context, q Query, s Summary, text string) (int, error) {
	if e := ctx.Err(); e != nil {
		return 0, e
	}
	return 4*Score(q.Text, s.Title) + Score(q.Text, text), nil
}
