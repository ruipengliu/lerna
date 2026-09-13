package nodetls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"time"

	"lerna/authorization"
)

// Trust is installed only by the trusted host. Until bounds acceptance of an
// issuer, including on established connections and resumed TLS sessions.
type Trust struct {
	Certificate []byte
	Until       time.Time
}
type Endpoint struct {
	nodes           *authorization.NodeAuthority
	clock           authorization.Clock
	namespace, node string
	certificate     tls.Certificate
	roots           *x509.CertPool
	trust           map[string]time.Time
	timeout         time.Duration
}

func NewEndpoint(nodes *authorization.NodeAuthority, clock authorization.Clock, namespace, node string, certificate tls.Certificate, trust []Trust, timeout time.Duration) (*Endpoint, error) {
	if nodes == nil || clock == nil || namespace == "" || node == "" || len(trust) == 0 || len(trust) > 4 || timeout < time.Millisecond || timeout > 5*time.Second || len(certificate.Certificate) != 1 {
		return nil, failure(authorization.Invalid)
	}
	raw, err := x509.MarshalPKCS8PrivateKey(certificate.PrivateKey)
	if err != nil {
		return nil, failure(authorization.Invalid)
	}
	owned, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}))
	if err != nil {
		return nil, failure(authorization.Invalid)
	}
	e := &Endpoint{nodes: nodes, clock: clock, namespace: namespace, node: node, certificate: owned, roots: x509.NewCertPool(), trust: map[string]time.Time{}, timeout: timeout}
	now, err := clock.Now()
	if err != nil {
		return nil, err
	}
	for _, t := range trust {
		if len(t.Certificate) > 8192 {
			return nil, failure(authorization.Invalid)
		}
		cert, err := x509.ParseCertificate(t.Certificate)
		if err != nil || !cert.IsCA || !t.Until.After(now) || t.Until.After(cert.NotAfter) || t.Until.After(now.Add(365*24*time.Hour)) {
			return nil, failure(authorization.Invalid)
		}
		digest := authorization.CertificateDigest(cert.Raw)
		if _, ok := e.trust[digest]; ok {
			return nil, failure(authorization.Invalid)
		}
		e.roots.AddCert(cert)
		e.trust[digest] = t.Until
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cert, err := x509.ParseCertificate(owned.Certificate[0])
	if err != nil {
		return nil, failure(authorization.Invalid)
	}
	if _, err = e.verify(ctx, []*x509.Certificate{cert}, node, x509.ExtKeyUsageServerAuth); err != nil {
		return nil, err
	}
	if _, err = e.verify(ctx, []*x509.Certificate{cert}, node, x509.ExtKeyUsageClientAuth); err != nil {
		return nil, err
	}
	return e, nil
}
func (e *Endpoint) verify(ctx context.Context, certs []*x509.Certificate, expected string, usage x509.ExtKeyUsage) (string, error) {
	if len(certs) != 1 || certs[0] == nil || len(certs[0].Raw) > 8192 {
		return "", failure(authorization.Denied)
	}
	// Parse owned DER rather than trusting mutable exported Certificate fields.
	leaf, err := x509.ParseCertificate(certs[0].Raw)
	if err != nil {
		return "", failure(authorization.Denied)
	}
	now, err := e.clock.Now()
	if err != nil {
		return "", failure(authorization.TimeUntrusted)
	}
	chains, err := leaf.Verify(x509.VerifyOptions{Roots: e.roots, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{usage}})
	if err != nil {
		return "", failure(authorization.Denied)
	}
	trusted := false
	for _, chain := range chains {
		if now.Before(e.trust[authorization.CertificateDigest(chain[len(chain)-1].Raw)]) {
			trusted = true
		}
	}
	if !trusted || leaf.IsCA || len(leaf.URIs) != 1 || leaf.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
		return "", failure(authorization.Denied)
	}
	uri := leaf.URIs[0]
	node := uri.Query().Get("node")
	if node == "" || uri.String() != nodeURI(e.namespace, node).String() || expected != "" && expected != node {
		return "", failure(authorization.Denied)
	}
	if err = e.nodes.Check(ctx, e.namespace, node, authorization.CertificateDigest(leaf.Raw), ""); err != nil {
		return "", err
	}
	return node, nil
}
func (e *Endpoint) tlsConfig(peer string, server bool) *tls.Config {
	c := &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, Certificates: []tls.Certificate{e.certificate}, Time: func() time.Time {
		now, err := e.clock.Now()
		if err != nil {
			return time.Time{}
		}
		return now
	}}
	if server {
		c.ClientAuth = tls.RequireAnyClientCert
	} else {
		// URI identity replaces DNS identity. VerifyConnection below performs full
		// chain, purpose, time, issuer cutoff, target and current registration checks.
		c.InsecureSkipVerify = true
		c.ClientSessionCache = tls.NewLRUClientSessionCache(16)
	}
	c.VerifyConnection = func(state tls.ConnectionState) error {
		ctx, cancel := context.WithTimeout(context.Background(), e.timeout)
		defer cancel()
		_, err := e.Authenticate(ctx, state, peer, server)
		return err
	}
	return c
}
func (e *Endpoint) ServerConfig() *tls.Config { return e.tlsConfig("", true) }
func (e *Endpoint) ClientConfig(peer string) (*tls.Config, error) {
	if peer == "" {
		return nil, failure(authorization.Invalid)
	}
	return e.tlsConfig(peer, false), nil
}

// Authenticate must also run for each application message (including responses
// at a client). TLS handshake success alone is not current authorization.
func (e *Endpoint) Authenticate(ctx context.Context, state tls.ConnectionState, peer string, server bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	usage := x509.ExtKeyUsageServerAuth
	if server {
		usage = x509.ExtKeyUsageClientAuth
	}
	node, err := e.verify(ctx, state.PeerCertificates, peer, usage)
	if err != nil {
		return "", err
	}
	local, err := x509.ParseCertificate(e.certificate.Certificate[0])
	if err != nil {
		return "", failure(authorization.Denied)
	}
	localUsage := x509.ExtKeyUsageClientAuth
	if server {
		localUsage = x509.ExtKeyUsageServerAuth
	}
	if _, err = e.verify(ctx, []*x509.Certificate{local}, e.node, localUsage); err != nil {
		return "", err
	}
	return node, nil
}

// Presentation is a server-side adapter entry. The subject is only a claim:
// current registration and GrantAuthority must both authorize that relationship.
func (e *Endpoint) Presentation(ctx context.Context, state tls.ConnectionState, subject, operation, semantic string) (authorization.GrantPresentation, error) {
	if !state.HandshakeComplete || state.Version != tls.VersionTLS13 {
		return authorization.GrantPresentation{}, failure(authorization.Denied)
	}
	node, err := e.Authenticate(ctx, state, "", true)
	if err != nil {
		return authorization.GrantPresentation{}, err
	}
	digest := authorization.CertificateDigest(state.PeerCertificates[0].Raw)
	if subject == "" {
		return authorization.GrantPresentation{}, failure(authorization.Denied)
	}
	if err = e.nodes.Check(ctx, e.namespace, node, digest, subject); err != nil {
		return authorization.GrantPresentation{}, err
	}
	return authorization.GrantPresentation{Namespace: e.namespace, Subject: subject, Audience: e.node, Presenter: node, CertificateSHA256: digest, OperationID: operation, SemanticSHA256: semantic}, nil
}

// RestorePresentation accepts only a certificate stored by the trusted ingress
// with the original operation. It revalidates current trust/registration without
// claiming that the old connection still exists. Ordinary requests cannot use it.
func (e *Endpoint) RestorePresentation(ctx context.Context, der []byte, subject, operation, semantic string) (authorization.GrantPresentation, error) {
	if len(der) == 0 || len(der) > 8192 || subject == "" {
		return authorization.GrantPresentation{}, failure(authorization.Denied)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return authorization.GrantPresentation{}, failure(authorization.Denied)
	}
	node, err := e.verify(ctx, []*x509.Certificate{cert}, "", x509.ExtKeyUsageClientAuth)
	if err != nil {
		return authorization.GrantPresentation{}, err
	}
	local, err := x509.ParseCertificate(e.certificate.Certificate[0])
	if err != nil {
		return authorization.GrantPresentation{}, failure(authorization.Denied)
	}
	if _, err = e.verify(ctx, []*x509.Certificate{local}, e.node, x509.ExtKeyUsageServerAuth); err != nil {
		return authorization.GrantPresentation{}, err
	}
	digest := authorization.CertificateDigest(der)
	if err = e.nodes.Check(ctx, e.namespace, node, digest, subject); err != nil {
		return authorization.GrantPresentation{}, err
	}
	return authorization.GrantPresentation{Namespace: e.namespace, Subject: subject, Audience: e.node, Presenter: node, CertificateSHA256: digest, OperationID: operation, SemanticSHA256: semantic}, nil
}

// LocalIdentity exposes the trusted deployment scope, without key material.
func (e *Endpoint) LocalIdentity() (namespace, node string) { return e.namespace, e.node }
