package context

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"lerna/brain"
	"lerna/contextassembly"
	"lerna/tasks"
	"lerna/websearch"
	"strings"
	"time"
)

type SearchEvidence interface {
	Read(context.Context, string) (websearch.Result, error)
}
type SearchContext struct {
	boundContext
	evidence  SearchEvidence
	refs      []string
	summaries bool
}

var _ brain.Context = (*SearchContext)(nil)

func NewSearch(e SearchEvidence, t tasks.Task, location string, refs []string) (*SearchContext, error) {
	if e == nil {
		return nil, contextassembly.Invalid
	}
	bound, err := bindContext(t, location, refs)
	if err != nil {
		return nil, err
	}
	return &SearchContext{boundContext: bound, evidence: e, refs: append([]string(nil), refs...)}, nil
}

// NewSearchForAnswer explicitly admits the retained provider summaries as secondary evidence.
// It never represents candidate pages as independently acquired.
func NewSearchForAnswer(e SearchEvidence, t tasks.Task, location string, refs []string) (*SearchContext, error) {
	c, err := NewSearch(e, t, location, refs)
	if err == nil {
		c.summaries = true
	}
	return c, err
}
func (c *SearchContext) Validate(ctx context.Context, t tasks.Task, location string) error {
	if err := c.check(t, location); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, ref := range c.refs {
		if _, err := c.evidence.Read(ctx, ref); err != nil {
			return err
		}
	}
	return nil
}
func (c *SearchContext) Assemble(ctx context.Context, t tasks.Task, location string, limit int) (brain.Input, error) {
	if err := c.check(t, location); err != nil {
		return brain.Input{}, err
	}
	if limit < 1 || limit > brain.MaxInputBytes {
		return brain.Input{}, contextassembly.BudgetExceeded
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out := taskInput(t)
	for _, ref := range c.refs {
		discovery, err := c.evidence.Read(ctx, ref)
		if err != nil {
			return brain.Input{}, err
		}
		acquired := discovery.Acquisition
		raw, err := json.Marshal(struct {
			Status                          string
			Candidates                      []websearch.Candidate
			SearchedAt                      time.Time
			SearchURL, ResponseSHA256, Mode string
			Requests                        int
		}{"discovered", discovery.Candidates, acquired.FetchedAt, acquired.FinalURL, acquired.SHA256, acquired.Mode, acquired.Requests})
		if err != nil {
			return brain.Input{}, contextassembly.Invalid
		}
		projected := 0
		if c.summaries {
			for i, candidate := range discovery.Candidates {
				if strings.TrimSpace(candidate.Summary) == "" {
					continue
				}
				sum := sha256.Sum256([]byte(candidate.Summary))
				material, e := json.Marshal(map[string]any{"Status": "acquired", "Body": candidate.Summary, "SHA256": fmt.Sprintf("%x", sum), "FetchedAt": acquired.FetchedAt, "EvidenceKind": "search-summary", "SearchURL": acquired.FinalURL, "ResponseRef": ref, "ResponseSHA256": acquired.SHA256, "SourceURL": candidate.URL, "Title": candidate.Title, "CandidateIndex": i, "Mode": acquired.Mode})
				if e != nil {
					return brain.Input{}, e
				}
				// A stable view of the original response, not a new Content object.
				// All reads and lineage checks continue to use ResponseRef.
				viewRef := fmt.Sprintf("%s#summary-%d", ref, i)
				out.Blocks = append(out.Blocks, brain.Block{Ref: viewRef, Text: string(material), Subject: t.Subject, Role: "external-search-evidence"})
				projected++
			}
		}
		if projected == 0 {
			out.Blocks = append(out.Blocks, brain.Block{Ref: ref, Text: string(raw), Subject: t.Subject, Role: "search-candidates"})
		}

		if err := checkSize(out, limit); err != nil {
			return brain.Input{}, err
		}
	}
	return finishAssembly(ctx, c, t, location, out, limit, len(c.refs))
}
