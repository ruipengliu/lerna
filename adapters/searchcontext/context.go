// Package searchcontext projects retained discovery responses as candidates,
// never as acquired page evidence. It is a fact-content adapter for the existing
// taskcontext host, which still governs decision qualification and retention.
package searchcontext

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"lerna/answers"
	"lerna/brain"
	"lerna/contextassembly"
	"lerna/tasks"
	"lerna/websearch"
	"strings"
	"time"
)

type Evidence interface {
	Read(context.Context, string) (websearch.Result, error)
}
type Context struct {
	evidence  Evidence
	refs      []string
	facts     [32]byte
	location  string
	summaries bool
}

var _ brain.Context = (*Context)(nil)

func fingerprint(t tasks.Task) [32]byte {
	raw, _ := json.Marshal(struct {
		Ref                                           tasks.Ref
		Subject, Resource, Goal, GoalRef, GuidanceRef string
		Constraints                                   tasks.Constraints
		Inputs                                        []string
		Facts                                         []tasks.InputFact
	}{t.Ref, t.Subject, t.Resource, t.Goal, t.GoalRef, t.GuidanceRef, t.Constraints, t.InputRefs, t.InputFacts})
	return sha256.Sum256(raw)
}
func New(e Evidence, t tasks.Task, location string, refs []string) (*Context, error) {
	if e == nil || t.Ref.Namespace == "" || t.Ref.TaskID == "" || t.Subject == "" || location == "" || len(location) > 256 || len(refs) < 1 || len(refs) > 8 {
		return nil, contextassembly.Invalid
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		id, err := answers.ParseReference(ref)
		if err != nil || id.Namespace != t.Ref.Namespace || seen[ref] {
			return nil, contextassembly.Invalid
		}
		seen[ref] = true
	}
	return &Context{e, append([]string(nil), refs...), fingerprint(t), location, false}, nil
}

// NewForAnswer explicitly admits the retained provider summaries as secondary evidence.
// It never represents candidate pages as independently acquired.
func NewForAnswer(e Evidence, t tasks.Task, location string, refs []string) (*Context, error) {
	c, err := New(e, t, location, refs)
	if err == nil {
		c.summaries = true
	}
	return c, err
}
func (c *Context) check(t tasks.Task, location string) error {
	if location != c.location || fingerprint(t) != c.facts {
		return contextassembly.Invalidated
	}
	return nil
}
func (c *Context) Validate(ctx context.Context, t tasks.Task, location string) error {
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
func (c *Context) Assemble(ctx context.Context, t tasks.Task, location string, limit int) (brain.Input, error) {
	if err := c.check(t, location); err != nil {
		return brain.Input{}, err
	}
	if limit < 1 || limit > brain.MaxInputBytes {
		return brain.Input{}, contextassembly.BudgetExceeded
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	constraints, _ := json.Marshal(t.Constraints)
	out := brain.Input{Goal: t.Goal, Constraints: string(constraints)}
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

		encoded, err := json.Marshal(out)
		if err != nil || len(encoded) > limit {
			return brain.Input{}, contextassembly.BudgetExceeded
		}
	}
	// A sole evidence read is already the final I/O boundary; projection and
	// size checks are local. With multiple references, later reads may span a
	// revocation of an earlier source, so revalidate the combined context.
	if len(c.refs) > 1 {
		if err := c.Validate(ctx, t, location); err != nil {
			return brain.Input{}, err
		}
	}
	return out, nil
}
