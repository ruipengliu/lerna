package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"slices"
	"time"
)

// NodeIssuer is the host-injected certificate adapter. Issue verifies the signed
// request and returns only public certificate bytes; private keys never enter State.
type NodeIssuer interface {
	Issue(context.Context, NodeMutation, time.Time) ([]byte, error)
}
type NodeConfig struct {
	MaxTTL, RequestTTL, IOTimeout time.Duration
	MaxRecords, MaxOperations     int
}
type NodeMutation struct {
	OperationID, Namespace, Node, Kind string
	ExpectedRevision                   uint64
	Subjects                           []string
	RequestExpires, Expires            int64
	CSR                                []byte
}

// NodeRequestBinding binds the entire enrollment intent into the signed CSR.
func NodeRequestBinding(in NodeMutation) string {
	in.CSR = nil
	b, _ := json.Marshal(in)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func CertificateDigest(der []byte) string {
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type NodeRecord struct {
	Namespace, Node, CertificateSHA256 string
	Certificate                        []byte
	Subjects                           []string
	Expires                            int64
	Revision                           uint64
	Disabled                           bool
	// Previous certificates remain as immutable grant provenance, never TLS credentials.
	Previous []string
}
type NodeOperation struct {
	Subject string
	Request NodeMutation
	Record  NodeRecord
}
type NodeJournal struct {
	Presentations map[string]NodePresentation
	Config        NodeConfig
	Records       map[string]NodeRecord
	Operations    map[string]NodeOperation
}
type NodeAuthority struct {
	service *Service
	config  NodeConfig
	issuer  NodeIssuer
}

func (s *Service) Nodes(c NodeConfig, issuer NodeIssuer) (*NodeAuthority, error) {
	if issuer == nil || c.MaxTTL < time.Second || c.MaxTTL > 24*time.Hour || c.RequestTTL < time.Second || c.RequestTTL > 10*time.Minute || c.RequestTTL > c.MaxTTL || c.IOTimeout < time.Millisecond || c.IOTimeout > 5*time.Second || c.MaxRecords < 1 || c.MaxRecords > 512 || c.MaxOperations < 2*c.MaxRecords || c.MaxOperations > 4096 {
		return nil, fail(Invalid)
	}
	return &NodeAuthority{s, c, issuer}, nil
}
func (n *NodeAuthority) journal(st *State) error {
	if st.Nodes == nil {
		st.Nodes = &NodeJournal{Config: n.config, Records: map[string]NodeRecord{}, Operations: map[string]NodeOperation{}}
	}
	if st.Nodes.Config != n.config {
		return fail(Invalid)
	}
	return nil
}
func copyNode(r NodeRecord) NodeRecord {
	r.Certificate = slices.Clone(r.Certificate)
	r.Subjects = slices.Clone(r.Subjects)
	r.Previous = slices.Clone(r.Previous)
	return r
}
func nodeAdmin(st *State, token string, now time.Time) (Principal, error) {
	p, err := authenticate(st, token, now)
	if err != nil {
		return p, err
	}
	if !p.Administrator {
		return p, fail(Denied)
	}
	return p, nil
}
func (n *NodeAuthority) Mutate(ctx context.Context, token string, in NodeMutation) (NodeRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, n.config.IOTimeout)
	defer cancel()
	in.CSR = slices.Clone(in.CSR)
	in.Subjects = slices.Clone(in.Subjects)
	if !validName(in.Namespace) || !validName(in.Node) || len(in.CSR) > 8192 || len(in.Subjects) > 16 {
		return NodeRecord{}, fail(Invalid)
	}
	var old *NodeRecord
	var subject string
	check := func(st *State, now time.Time) error {
		p, err := nodeAdmin(st, token, now)
		if err != nil {
			return err
		}
		subject = p.Subject
		if st.Namespace != in.Namespace {
			return fail(Denied)
		}
		if err = n.journal(st); err != nil {
			return err
		}
		epoch, err := parseOperationWindow(st, in.OperationID)
		if err != nil {
			return err
		}
		if prior, ok := st.Nodes.Operations[in.OperationID]; ok {
			if prior.Subject != p.Subject {
				return fail(Denied)
			}
			if !reflect.DeepEqual(prior.Request, in) {
				return fail(IdentityConflict)
			}
			r := copyNode(prior.Record)
			old = &r
			return nil
		}
		if err := n.service.partitionOperation(st, now, &st.ContentOperations, st.RuntimeOperations, in.OperationID, p.Subject, false); err != nil {
			return err
		}
		if _, ok := st.ContentOperations[in.OperationID]; ok {
			return fail(IdentityConflict)
		}
		if epoch <= st.ClosedThrough || epoch != st.Window || now.UnixNano() >= st.WindowExpires {
			return fail(Expired)
		}
		if st.Revision != in.ExpectedRevision {
			return fail(Conflict)
		}
		r, exists := st.Nodes.Records[in.Node]
		// Reserve room for disabling every currently active node, even when issuance is full.
		limit := n.config.MaxOperations - n.config.MaxRecords
		if in.Kind == "DISABLE" {
			limit = n.config.MaxOperations
		}
		if len(st.Nodes.Operations) >= limit {
			return fail(Unavailable)
		}
		switch in.Kind {
		case "DISABLE":
			if r.Disabled {
				return fail(Conflict)
			}
			if !exists {
				return fail(NotFound)
			}
			if len(in.CSR) != 0 || len(in.Subjects) != 0 || in.Expires != 0 || in.RequestExpires != 0 {
				return fail(Invalid)
			}
		case "ENROLL", "ROTATE", "REENROLL":
			if in.Kind == "ENROLL" && exists {
				return fail(Conflict)
			}
			if in.Kind != "ENROLL" && !exists {
				return fail(NotFound)
			}
			if in.Kind == "ROTATE" && (r.Disabled || now.Unix() >= r.Expires) {
				return fail(Denied)
			}
			if in.Kind == "ROTATE" && !slices.Equal(in.Subjects, r.Subjects) {
				return fail(Denied)
			}
			if !exists && len(st.Nodes.Records) >= n.config.MaxRecords {
				return fail(Unavailable)
			}
			if len(r.Previous) >= 32 {
				return fail(Unavailable)
			}
			if len(in.CSR) == 0 || in.RequestExpires <= now.Unix() || in.RequestExpires > now.Add(n.config.RequestTTL).Unix() || in.Expires <= now.Unix() || in.Expires > now.Add(n.config.MaxTTL).Unix() {
				return fail(Invalid)
			}
			for i, v := range in.Subjects {
				if !validName(v) || slices.Contains(in.Subjects[:i], v) {
					return fail(Invalid)
				}
				active := false
				for _, p := range st.Principals {
					if p.Subject == v && !p.Disabled && p.Expires >= in.Expires {
						active = true
					}
				}
				if !active {
					return fail(Denied)
				}
			}
		default:
			return fail(Unsupported)
		}
		return nil
	}
	err := n.service.update(ctx, func(st *State, now time.Time) error { old = nil; return check(st, now) })
	if err != nil {
		return NodeRecord{}, err
	}
	if old != nil {
		return *old, nil
	}
	var der []byte
	if in.Kind != "DISABLE" {
		now, err := n.service.clock.Now()
		if err != nil {
			return NodeRecord{}, fail(TimeUntrusted)
		}
		der, err = n.issuer.Issue(ctx, in, now)
		if err != nil {
			return NodeRecord{}, err
		}
		if ctx.Err() != nil {
			return NodeRecord{}, ctx.Err()
		}
		if len(der) == 0 || len(der) > 8192 {
			return NodeRecord{}, fail(Invalid)
		}
	}
	var out NodeRecord
	err = n.service.update(ctx, func(st *State, now time.Time) error {
		old = nil
		if err := check(st, now); err != nil {
			return err
		}
		if old != nil {
			out = *old
			return nil
		}
		r := st.Nodes.Records[in.Node]
		if in.Kind == "DISABLE" {
			r.Disabled = true
		} else {
			digest := CertificateDigest(der)
			for _, other := range st.Nodes.Records {
				if other.CertificateSHA256 == digest || slices.Contains(other.Previous, digest) {
					return fail(IdentityConflict)
				}
			}
			if r.CertificateSHA256 != "" {
				r.Previous = append(r.Previous, r.CertificateSHA256)
			}
			r.Namespace = in.Namespace
			r.Node = in.Node
			r.Certificate = slices.Clone(der)
			r.CertificateSHA256 = digest
			r.Subjects = slices.Clone(in.Subjects)
			r.Expires = in.Expires
			r.Disabled = false
			// Re-enrollment after key loss never authorizes old grant provenance.
			if in.Kind == "REENROLL" {
				r.Previous = nil
			}
		}
		st.Revision++
		r.Revision = st.Revision
		st.Nodes.Records[in.Node] = r
		st.Nodes.Operations[in.OperationID] = NodeOperation{subject, in, copyNode(r)}
		out = copyNode(r)
		return nil
	})
	if err != nil {
		return NodeRecord{}, err
	}
	return out, nil
}
func (n *NodeAuthority) Get(ctx context.Context, token, node string) (NodeRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, n.config.IOTimeout)
	defer cancel()
	var out NodeRecord
	err := n.service.update(ctx, func(st *State, now time.Time) error {
		if _, err := nodeAdmin(st, token, now); err != nil {
			return err
		}
		if err := n.journal(st); err != nil {
			return err
		}
		r, ok := st.Nodes.Records[node]
		if !ok {
			return fail(NotFound)
		}
		out = copyNode(r)
		return nil
	})
	if err != nil {
		return NodeRecord{}, err
	}
	return out, nil
}
func (n *NodeAuthority) LookupOperation(ctx context.Context, token, id string) (NodeRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, n.config.IOTimeout)
	defer cancel()
	var out NodeRecord
	err := n.service.update(ctx, func(st *State, now time.Time) error {
		if _, err := nodeAdmin(st, token, now); err != nil {
			return err
		}
		if err := n.journal(st); err != nil {
			return err
		}
		epoch, err := parseOperationWindow(st, id)
		if err != nil {
			return err
		}
		op, ok := st.Nodes.Operations[id]
		if !ok {
			if epoch <= st.ClosedThrough || now.UnixNano() >= st.WindowExpires {
				return fail(Expired)
			}
			return fail(NotFound)
		}
		out = copyNode(op.Record)
		return nil
	})
	if err != nil {
		return NodeRecord{}, err
	}
	return out, nil
}
func checkNode(st *State, now time.Time, namespace, node, digest, subject string) error {
	if st.Nodes == nil || st.Namespace != namespace {
		return fail(Denied)
	}
	r, ok := st.Nodes.Records[node]
	if !ok || r.Disabled || now.Unix() >= r.Expires || r.CertificateSHA256 != digest {
		return fail(Denied)
	}
	if subject != "" && !slices.Contains(r.Subjects, subject) {
		return fail(Denied)
	}
	return nil
}
func (n *NodeAuthority) Check(ctx context.Context, namespace, node, digest, subject string) error {
	ctx, cancel := context.WithTimeout(ctx, n.config.IOTimeout)
	defer cancel()
	return n.service.update(ctx, func(st *State, now time.Time) error {
		if err := n.journal(st); err != nil {
			return err
		}
		return checkNode(st, now, namespace, node, digest, subject)
	})
}
