// Package nodetls binds native TLS to persistent Harness node registration.
package nodetls

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"lerna/authorization"
	"math/big"
	"net/url"
	"time"
)

func failure(c authorization.Code) error { return &authorization.Error{Code: c} }
func nodeURI(namespace, node string) *url.URL {
	return &url.URL{Scheme: "harness-node", Host: "v1", Path: "/", RawQuery: url.Values{"namespace": {namespace}, "node": {node}}.Encode()}
}

// GenerateCA is a trusted installation helper, never a discovery/RPC method.
func GenerateCA(now time.Time, ttl time.Duration) (tls.Certificate, error) {
	if ttl < time.Second || ttl > 365*24*time.Hour {
		return tls.Certificate{}, failure(authorization.Invalid)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Harness node issuer"}, NotBefore: now.Add(-time.Second), NotAfter: now.Add(ttl), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, MaxPathLen: 0, MaxPathLenZero: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// Request signs the complete administrator-reviewed intent, including its operation
// and expiry. The private key is generated and retained by the joining node.
func Request(key *ecdsa.PrivateKey, in authorization.NodeMutation) ([]byte, error) {
	if key == nil || key.Curve != elliptic.P256() {
		return nil, failure(authorization.Invalid)
	}
	return x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: authorization.NodeRequestBinding(in)}, URIs: []*url.URL{nodeURI(in.Namespace, in.Node)}}, key)
}

type Issuer struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
}

func NewIssuer(c tls.Certificate) (*Issuer, error) {
	if len(c.Certificate) != 1 {
		return nil, failure(authorization.Invalid)
	}
	cert, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		return nil, failure(authorization.Invalid)
	}
	key, ok := c.PrivateKey.(*ecdsa.PrivateKey)
	if !ok || key == nil || key.Curve != elliptic.P256() || !cert.IsCA || cert.KeyUsage&x509.KeyUsageCertSign == 0 || !key.PublicKey.Equal(cert.PublicKey) {
		return nil, failure(authorization.Invalid)
	}
	// Own the key, so host mutation cannot silently change an enabled issuer.
	raw, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, failure(authorization.Invalid)
	}
	owned, err := x509.ParsePKCS8PrivateKey(raw)
	if err != nil {
		return nil, failure(authorization.Invalid)
	}
	return &Issuer{cert, owned.(*ecdsa.PrivateKey)}, nil
}
func (i *Issuer) Issue(ctx context.Context, in authorization.NodeMutation, now time.Time) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(in.CSR) == 0 || len(in.CSR) > 8192 {
		return nil, failure(authorization.Invalid)
	}
	csr, err := x509.ParseCertificateRequest(in.CSR)
	if err != nil || csr.CheckSignature() != nil || csr.Subject.CommonName != authorization.NodeRequestBinding(in) || len(csr.URIs) != 1 || csr.URIs[0].String() != nodeURI(in.Namespace, in.Node).String() {
		return nil, failure(authorization.Denied)
	}
	key, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, failure(authorization.Invalid)
	}
	if now.Before(i.certificate.NotBefore) || !now.Before(i.certificate.NotAfter) || in.Expires > i.certificate.NotAfter.Unix() || in.Expires <= now.Unix() {
		return nil, failure(authorization.Denied)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, failure(authorization.Unavailable)
	}
	template := &x509.Certificate{SerialNumber: serial, NotBefore: now, NotAfter: time.Unix(in.Expires, 0), BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}, URIs: []*url.URL{nodeURI(in.Namespace, in.Node)}}
	return x509.CreateCertificate(rand.Reader, template, i.certificate, csr.PublicKey, i.key)
}
