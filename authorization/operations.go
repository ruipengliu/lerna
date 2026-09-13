package authorization

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/internal/randomid"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Service) NewOperation(ctx context.Context, token string) (string, error) {
	nonce, err := randomid.New()
	if err != nil {
		return "", err
	}
	var out string
	err = s.update(ctx, func(st *State, now time.Time) error {
		if _, err := authenticate(st, token, now); err != nil {
			return err
		}
		if st.Window == 0 || now.UnixNano() >= st.WindowExpires || st.Window <= st.ClosedThrough {
			st.ClosedThrough = st.Window
			st.Window++
			st.WindowExpires = now.Add(s.config.WindowTTL).UnixNano()
		}
		payload := st.Authority + "/" + strconv.FormatUint(st.Window, 10) + "/" + nonce
		mac := hmac.New(sha256.New, st.Secret)
		mac.Write([]byte(st.Namespace + "\x00" + payload))
		out = base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		return nil
	})
	return out, err
}
func windowOf(st *State, id string) (uint64, error) {
	if st.Nodes != nil {
		if _, ok := st.Nodes.Operations[id]; ok {
			return 0, fail(IdentityConflict)
		}
	}
	return parseOperationWindow(st, id)
}
func parseOperationWindow(st *State, id string) (uint64, error) {
	if len(id) > 512 {
		return 0, fail(Invalid)
	}
	parts := strings.Split(id, ".")
	if len(parts) != 2 {
		return 0, fail(Invalid)
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil {
		return 0, fail(Invalid)
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		return 0, fail(Invalid)
	}
	if base64.RawURLEncoding.EncodeToString(payload) != parts[0] || base64.RawURLEncoding.EncodeToString(signature) != parts[1] {
		return 0, fail(Invalid)
	}
	mac := hmac.New(sha256.New, st.Secret)
	mac.Write([]byte(st.Namespace + "\x00" + string(payload)))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return 0, fail(Denied)
	}
	fields := strings.Split(string(payload), "/")
	if len(fields) != 3 || fields[0] != st.Authority || len(fields[2]) != 64 {
		return 0, fail(Denied)
	}
	epoch, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil || epoch == 0 {
		return 0, fail(Invalid)
	}
	return epoch, nil
}

type Mutation struct {
	Namespace, OperationID string
	Command                *wire.AuthorizationCommand
}

func (s *Service) Execute(ctx context.Context, token string, in Mutation) (*wire.AuthorizationReceipt, error) {
	var out *wire.AuthorizationReceipt
	if in.Command == nil {
		return nil, fail(Invalid)
	}
	command := proto.Clone(in.Command).(*wire.AuthorizationCommand)
	normalizeCommand(command)
	err := s.update(ctx, func(st *State, now time.Time) error {
		principal, err := authenticate(st, token, now)
		if err != nil {
			return err
		}
		if st.Namespace != in.Namespace {
			return fail(Denied)
		}
		if err := s.authorizeManagement(st, principal, command, now); err != nil {
			return err
		}
		epoch, err := windowOf(st, in.OperationID)
		if err != nil {
			return err
		}
		if st.Signed != nil {
			if _, ok := st.Signed.Uses[in.OperationID]; ok {
				return fail(IdentityConflict)
			}
			if _, exists := st.Signed.Operations[in.OperationID]; exists {
				return fail(IdentityConflict)
			}
		}
		if _, ok := st.MemoryOperations[in.OperationID]; ok {
			return fail(IdentityConflict)
		}
		if _, ok := st.ExecutionOperations[in.OperationID]; ok {
			return fail(IdentityConflict)
		}
		if _, ok := st.ContentOperations[in.OperationID]; ok {
			return fail(IdentityConflict)
		}
		if _, exists := st.RuntimeOperations[in.OperationID]; exists {
			return fail(IdentityConflict)
		}
		if old, ok := st.Operations[in.OperationID]; ok {
			if old.Subject != principal.Subject {
				return fail(Denied)
			}
			if !proto.Equal(old.Command, command) {
				return fail(IdentityConflict)
			}
			out = proto.Clone(old.Receipt).(*wire.AuthorizationReceipt)
			return nil
		}
		if epoch <= st.ClosedThrough || epoch != st.Window || now.UnixNano() >= st.WindowExpires {
			return fail(Expired)
		}
		if command.ExpectedRevision != st.Revision {
			return fail(Conflict)
		}
		ref, err := s.change(st, command, now)
		if err != nil {
			return err
		}
		// Check the resulting snapshot so cleanup can reclaim a full history.
		// On failure update discards all changes to this private snapshot.
		if len(st.Operations) >= s.config.MaxWork {
			return fail(Unavailable)
		}
		st.Revision++
		out = &wire.AuthorizationReceipt{OperationId: in.OperationID, Revision: st.Revision, ResultRef: ref, Evidence: "local_authorization_commit"}
		st.Operations[in.OperationID] = OperationRecord{Subject: principal.Subject, Command: command, Receipt: out, Window: epoch, RetainUntil: now.Add(s.config.ReceiptRetention).UnixNano()}
		st.Changes = append(st.Changes, proto.Clone(out).(*wire.AuthorizationReceipt))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Service) authorizeManagement(st *State, p Principal, cmd *wire.AuthorizationCommand, now time.Time) error {
	if !known(cmd) {
		return fail(Unsupported)
	}
	if p.Administrator {
		return nil
	}
	proposed := cmd.GetIssueGrant()
	if proposed == nil || proposed.MayIssue {
		return fail(Denied)
	}
	b := s.newBudget()
	if err := s.validateScope(st, proposed.Scope, now, b); err != nil {
		return err
	}
	if err := s.policyCovers(st, proposed.Scope, now, b); err != nil {
		return err
	}
	for _, grant := range st.Grants {
		if err := b.step(); err != nil {
			return err
		}
		if grant.Subject != p.Subject || !grant.MayIssue || grant.Mode != "continuous" {
			continue
		}
		contained, err := s.scopeContains(st, grant.Scope, proposed.Scope, b)
		if err != nil {
			return err
		}
		if contained {
			return nil
		}
	}
	return fail(Denied)
}
func (s *Service) change(st *State, cmd *wire.AuthorizationCommand, now time.Time) (string, error) {
	switch c := cmd.Change.(type) {
	case *wire.AuthorizationCommand_RegisterPrincipal:
		return s.registerPrincipal(st, c.RegisterPrincipal, now)
	case *wire.AuthorizationCommand_ReplacePolicy:
		if c.ReplacePolicy == nil || len(c.ReplacePolicy.Rules) > s.config.MaxRules {
			return "", fail(Invalid)
		}
		b := s.newBudget()
		seen := make(map[string]bool)
		for _, rule := range c.ReplacePolicy.Rules {
			if rule == nil || !validName(rule.Id) || seen[rule.Id] {
				return "", fail(Invalid)
			}
			seen[rule.Id] = true
			if err := s.validateScope(st, rule.Scope, now, b); err != nil {
				return "", err
			}
		}
		st.Rules = c.ReplacePolicy.Rules
		return "policy", nil
	case *wire.AuthorizationCommand_IssueGrant:
		grant := c.IssueGrant
		if grant == nil || !validName(grant.Id) || !validName(grant.Subject) {
			return "", fail(Invalid)
		}
		if grant.Mode != "continuous" {
			return "", fail(Unsupported)
		}
		if _, exists := st.Grants[grant.Id]; exists {
			return "", fail(Conflict)
		}
		if len(st.Grants) >= s.config.MaxResources {
			return "", fail(Invalid)
		}
		subjectExists := false
		for _, p := range st.Principals {
			if p.Subject == grant.Subject && !p.Disabled && p.Expires > now.Unix() {
				subjectExists = true
			}
		}
		if !subjectExists {
			return "", fail(Invalid)
		}
		b := s.newBudget()
		if err := s.validateScope(st, grant.Scope, now, b); err != nil {
			return "", err
		}
		if err := s.policyCovers(st, grant.Scope, now, b); err != nil {
			return "", err
		}
		st.Grants[grant.Id] = grant
		return grant.Id, nil
	case *wire.AuthorizationCommand_RegisterResource:
		r := c.RegisterResource
		if r == nil || !validName(r.Id) || r.Id == "root" || len(st.Resources) >= s.config.MaxResources {
			return "", fail(Invalid)
		}
		if _, exists := st.Resources[r.Id]; exists {
			return "", fail(Conflict)
		}
		if _, exists := st.Resources[r.Parent]; !exists {
			return "", fail(Invalid)
		}
		cursor := r.Parent
		for depth := 1; cursor != ""; depth++ {
			if depth >= s.config.MaxDepth {
				return "", fail(Invalid)
			}
			cursor = st.Resources[cursor]
		}
		st.Resources[r.Id] = r.Parent
		return r.Id, nil
	case *wire.AuthorizationCommand_CloseWindows:
		if !c.CloseWindows {
			return "", fail(Invalid)
		}
		st.ClosedThrough = st.Window
		return "closed", nil
	case *wire.AuthorizationCommand_CleanRecords:
		if !c.CleanRecords {
			return "", fail(Invalid)
		}
		for id, record := range st.Operations {
			if record.Window <= st.ClosedThrough && now.UnixNano() >= record.RetainUntil {
				delete(st.Operations, id)
			}
		}
		kept := st.Changes[:0]
		for _, receipt := range st.Changes {
			if _, ok := st.Operations[receipt.OperationId]; ok {
				kept = append(kept, receipt)
			}
		}
		st.Changes = kept
		return "cleaned", nil
	default:
		return "", fail(Unsupported)
	}
}
func (s *Service) LookupOperation(ctx context.Context, token, id string) (*wire.AuthorizationReceipt, error) {
	var out *wire.AuthorizationReceipt
	err := s.update(ctx, func(st *State, now time.Time) error {
		p, err := authenticate(st, token, now)
		if err != nil {
			return err
		}
		epoch, err := windowOf(st, id)
		if err != nil {
			return err
		}
		record, ok := st.Operations[id]
		if !ok {
			if !p.Administrator {
				return fail(Denied)
			}
			if epoch <= st.ClosedThrough || now.UnixNano() >= st.WindowExpires {
				return fail(Expired)
			}
			return fail(NotFound)
		}
		if !p.Administrator && record.Subject != p.Subject {
			return fail(Denied)
		}
		if err := s.authorizeManagement(st, p, record.Command, now); err != nil {
			return err
		}
		out = proto.Clone(record.Receipt).(*wire.AuthorizationReceipt)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func validName(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._/-", r)) {
			return false
		}
	}
	return true
}

// Set order is not part of a policy's business meaning.
func normalizeCommand(cmd *wire.AuthorizationCommand) {
	normalizeScope := func(scope *wire.AuthorizationScope) {
		if scope == nil {
			return
		}
		sort.Strings(scope.Actions)
		sort.Strings(scope.Purposes)
		sort.Strings(scope.Locations)
		if set := scope.GetResources().GetSet(); set != nil {
			sort.Strings(set.Ids)
		}
	}
	if policy := cmd.GetReplacePolicy(); policy != nil {
		for _, rule := range policy.Rules {
			if rule != nil {
				normalizeScope(rule.Scope)
			}
		}
		sort.SliceStable(policy.Rules, func(i, j int) bool { return policy.Rules[i].GetId() < policy.Rules[j].GetId() })
	}
	if grant := cmd.GetIssueGrant(); grant != nil {
		normalizeScope(grant.Scope)
	}
}
