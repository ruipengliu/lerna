//go:build integration

package component_test

import (
	"context"
	"errors"
	"slices"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

type observedProposalPublication struct {
	key     string
	ref     v.ContentRef
	body    []byte
	sources []v.ContentRef
}

// This observer delegates every identity and body to the real independent
// publisher. Its finite reply-loss option is a mechanical boundary fault;
// business assertions read the actual Source facts, never call counts.
type proposalPublisherObserver struct {
	decision.Publisher
	planned        []observedProposalPublication
	loseFirstReply bool
	lost           bool
}

func (p *proposalPublisherObserver) PlanPublication(ctx context.Context, key string, body []byte, sources []v.ContentRef, permission decision.Permission) (v.ContentRef, error) {
	ref, err := p.Publisher.PlanPublication(ctx, key, body, sources, permission)
	if err == nil {
		p.planned = append(p.planned, observedProposalPublication{key, ref, slices.Clone(body), slices.Clone(sources)})
	}
	return ref, err
}

func (p *proposalPublisherObserver) Publish(ctx context.Context, key string, body []byte, sources []v.ContentRef, permission decision.Permission) (v.ContentRef, error) {
	ref, err := p.Publisher.Publish(ctx, key, body, sources, permission)
	if err == nil && p.loseFirstReply && !p.lost {
		p.lost = true
		return v.ContentRef{}, errors.New("fixture publication reply lost after independent commit")
	}
	return ref, err
}
