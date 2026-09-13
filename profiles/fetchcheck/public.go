package fetchcheck

import (
	"context"
	"errors"
	"lerna/adapters/fetchauth"
	"lerna/adapters/httpfetch"
	"lerna/fetch"
	"net"
	"net/netip"
	"time"
)

// PublicReport contains only facts from the explicitly public fixture. It has
// no token, cookie, local Content reference or ungoverned response body.
type PublicReport struct {
	Stage                       string
	Mode, Source, Status        string
	StartedAt                   time.Time
	MaxBytes                    int64
	MaxRequests                 int
	TimeoutMillis               int64
	AllowedNetworks             []string
	Requests                    int
	FetchedAt                   *time.Time `json:",omitempty"`
	FinalURL, MediaType, SHA256 string     `json:",omitempty"`
	Bytes                       int
	ControlledRoundTrip         bool
}

// CheckPublic performs at most one actual HTTPS request, with no proxy or retry.
// DNS addresses are captured as exact allowed destinations before acquisition.
// A DNS change, network failure or denial stays a failure; no replay substitutes.
func CheckPublic(ctx context.Context) PublicReport {
	const target = "https://example.com/"
	report := PublicReport{Mode: "public-network", Source: target, StartedAt: time.Now().UTC(), Status: "unavailable", Stage: "resolve", MaxBytes: 16384, MaxRequests: 1, TimeoutMillis: 5000}
	resolve, stop := context.WithTimeout(ctx, 2*time.Second)
	addresses, err := net.DefaultResolver.LookupNetIP(resolve, "ip", "example.com")
	stop()
	if err != nil || len(addresses) == 0 || len(addresses) > 16 {
		return report
	}
	prefixes := []netip.Prefix{}
	for _, ip := range addresses {
		ip = ip.Unmap()
		if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.Zone() != "" {
			report.Status = "denied"
			return report
		}
		prefix := netip.PrefixFrom(ip, ip.BitLen())
		prefixes = append(prefixes, prefix)
		report.AllowedNetworks = append(report.AllowedNetworks, prefix.String())
	}
	report.Stage = "setup"
	h, err := fresh(ctx, []string{target, target})
	if err != nil {
		return report
	}
	defer h.destroy()
	authority, err := fetchauth.New(h.auth, fetchauth.Scope{Token: h.token, Namespace: "local", Subject: "operator", Purpose: "task", Location: "local", Recipient: "local", Resources: map[string]string{target: "root"}})
	if err != nil {
		return report
	}
	adapter, err := httpfetch.New(httpfetch.Config{Authority: authority, URLs: []string{target}, Networks: prefixes})
	if err != nil {
		return report
	}
	report.Stage = "acquire"
	acquired, err := adapter.Fetch(ctx, fetch.Request{URL: target, MaxBytes: report.MaxBytes, MaxRequests: report.MaxRequests, Timeout: 5 * time.Second})
	report.Requests = acquired.Requests
	if err != nil {
		report.Status = publicFailure(err)
		return report
	}
	report.FetchedAt = &acquired.FetchedAt
	report.FinalURL = acquired.FinalURL
	report.MediaType = acquired.MediaType
	report.SHA256 = acquired.SHA256
	report.Bytes = len(acquired.Body)
	report.Stage = "retain"
	operation, err := h.operation(ctx)
	if err != nil {
		return report
	}
	ref, err := h.evidence.Save(ctx, operation, acquired)
	if err != nil {
		report.Status = publicFailure(err)
		return report
	}
	report.Stage = "verify"
	restored, err := h.evidence.Read(ctx, ref)
	if err != nil {
		report.Status = publicFailure(err)
		return report
	}
	if string(restored.Body) != string(acquired.Body) || restored.SHA256 != acquired.SHA256 || !restored.FetchedAt.Equal(acquired.FetchedAt) {
		return report
	}
	report.Stage = "complete"
	report.Status = "acquired"
	report.ControlledRoundTrip = true
	return report
}
func publicFailure(err error) string {
	for _, v := range []struct {
		e    error
		name string
	}{{fetch.Denied, "denied"}, {fetch.TimedOut, "timed_out"}, {fetch.Cancelled, "cancelled"}, {fetch.Expired, "expired"}, {fetch.TooLarge, "too_large"}, {fetch.Unsupported, "unsupported"}, {fetch.LimitExceeded, "limit_exceeded"}, {fetch.Invalid, "invalid"}} {
		if errors.Is(err, v.e) {
			return v.name
		}
	}
	return "unavailable"
}
