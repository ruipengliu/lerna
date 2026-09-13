package fetchcheck

import (
	"context"
	"lerna/adapters/executionlocal"
	"lerna/adapters/fetchexecution"
	"lerna/adapters/fetchqueries"
	"lerna/adapters/fetchtask"
	"lerna/adapters/searchexecution"
	"lerna/adapters/searchprivacy"
	"lerna/execution"
	"lerna/fetch"
	"lerna/sdk"
	"lerna/tasks"
	"lerna/websearch"
	"time"
)

// acquisitionExecution keeps execution authority, observed queries and content
// on the same host binding. The work guard and observation guard have distinct
// roles: acquisitionQueries binds the latter to the original action's budget.
type acquisitionExecution struct {
	host         *harness
	queries      *tasks.ActionPort
	guard        *fetchtask.Guard
	observations *fetchqueries.Scope
	content      execution.Content
}

func bindAcquisitionExecution(h *harness, queries *tasks.ActionPort) (*acquisitionExecution, error) {
	guard, err := fetchtask.New(h.auth, h.work, h.core, h.token)
	if err != nil {
		return nil, err
	}
	b := &acquisitionExecution{host: h, queries: queries, guard: guard, content: h.access}
	if queries != nil {
		b.observations, err = acquisitionQueries(h, queries, h.cap)
		if err != nil {
			return nil, err
		}
		b.content, err = h.access.WithQueries(queries)
		if err != nil {
			return nil, err
		}
	}
	return b, nil
}

// Every constructed execution is enrolled before its client becomes usable.
func (b *acquisitionExecution) install(driver execution.Driver) error {
	h := b.host
	service, err := execution.New(h.grants, h.work, b.content, driver, h.binding, h.cap, config(), h.operation)
	if err != nil {
		return err
	}
	if err := h.lifetime.track(service); err != nil {
		return err
	}
	h.exec = service
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(service, "local"), "local")
	return nil
}

func bindPageExecution(h *harness, queries *tasks.ActionPort, networkLimit uint32) error {
	b, err := bindAcquisitionExecution(h, queries)
	if err != nil {
		return err
	}
	target, err := fetchexecution.New(h.http, h.attempts, h.evidence, h.auth, fetchexecution.Config{Guard: b.guard, Token: h.token, Namespace: "local", Subject: "operator", Capability: h.cap, MaxBytes: int64(h.pageMaxBytes), MaxRequests: min(uint32(2), networkLimit), TaskLimit: networkLimit, Timeout: time.Second})
	if err != nil {
		return err
	}
	target, err = target.WithObservations(b.observations)
	if err != nil {
		return err
	}
	if err := b.install(target); err != nil {
		return err
	}
	h.target = target
	return nil
}

// Search retains its chosen transport and limits across recovery. Its disclosure
// adapter is rebound to the same queries as execution content and observations.
type searchAcquisition struct {
	provider     websearch.Searcher
	privacy      *searchprivacy.Guard
	bounds       searchProviderConfig
	networkLimit uint32
}

func (s *searchAcquisition) bind(h *harness, queries *tasks.ActionPort) error {
	b, err := bindAcquisitionExecution(h, queries)
	if err != nil {
		return err
	}
	// Rebinding retains the reader's verified lengths and therefore query costs.
	privacy := s.privacy
	var observations searchexecution.ObservationScope
	if b.queries != nil {
		privacy, err = privacy.WithQueries(b.queries)
		if err != nil {
			return err
		}
		observations = b.observations
	}
	driver, err := searchexecution.New(s.provider, h.attempts, h.evidence, h.auth, searchexecution.Config{Observations: observations, Guard: b.guard, QueryGuard: privacy, Token: h.token, Namespace: "local", Subject: "operator", Capability: h.cap, MaxResults: int(s.bounds.MaxResults), MaxBytes: int64(s.bounds.MaxBytes), MaxRequests: 1, TaskLimit: s.networkLimit, Timeout: time.Duration(s.bounds.TimeoutMS) * time.Millisecond})
	if err != nil {
		return err
	}
	return b.install(driver)
}

func (a *fetchActionHost) recoveryExecution(ctx context.Context, t tasks.Task) (*harness, error) {
	current, err := a.h.core.Load(ctx, t.Ref)
	if err != nil {
		return nil, err
	}
	if current.Task.Version != t.Version || current.Task.Owner != t.Owner || current.Task.OwnerEpoch != t.OwnerEpoch || current.Actions == nil {
		return nil, fetch.Denied
	}
	recovery := *a.h
	recovery.work = a.h.work.WithActionRecovery(tasks.QualificationOf(current))
	port, err := recovery.work.Actions(current.Actions.Limits)
	if err != nil {
		return nil, err
	}
	if a.searchDriver != nil {
		if err := a.searchDriver.bind(&recovery, port); err != nil {
			return nil, err
		}
	} else {
		b, err := bindAcquisitionExecution(&recovery, port)
		if err != nil {
			return nil, err
		}
		driver, err := recovery.target.WithRecovery(b.observations, b.guard)
		if err != nil {
			return nil, err
		}
		if err := b.install(driver); err != nil {
			return nil, err
		}
	}
	return &recovery, nil
}
