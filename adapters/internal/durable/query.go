package durable

import (
	"encoding/hex"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func QueryDigest(value string) error {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") || strings.ToLower(value) != value {
		return api.E("invalid_request", "invalid_query_digest")
	}
	if _, err := hex.DecodeString(value[7:]); err != nil {
		return api.E("invalid_request", "invalid_query_digest")
	}
	return nil
}
func QueryInput(in runtime.QueryBindingInput) error {
	if !api.ValidID(in.QueryID) || !api.ValidID(in.PrincipalID) || in.CredentialGeneration == 0 || in.CredentialGeneration > api.MaxSafeInteger {
		return api.E("invalid_request", "invalid_query_identity")
	}
	if in.TTL < time.Second || in.TTL > 5*time.Minute || in.TTL%time.Millisecond != 0 {
		return api.E("invalid_request", "invalid_query_ttl")
	}
	if err := QueryDigest(in.RolesDigest); err != nil {
		return err
	}
	return QueryDigest(in.QueryDigest)
}
func QueryExpected(in runtime.QueryBinding) error {
	if err := QueryInput(runtime.QueryBindingInput{QueryID: in.QueryID, PrincipalID: in.PrincipalID, CredentialGeneration: in.CredentialGeneration, RolesDigest: in.RolesDigest, QueryDigest: in.QueryDigest, TTL: time.Second}); err != nil {
		return err
	}
	if !api.ValidID(in.BindingID) {
		return api.E("invalid_request", "invalid_query_binding")
	}
	if _, err := api.ParseTime(in.ExpiresAt); err != nil {
		return api.E("invalid_request", "invalid_query_binding_expiry")
	}
	return nil
}
func QueryMatches(binding runtime.QueryBinding, in runtime.QueryBindingInput) bool {
	return binding.QueryID == in.QueryID && binding.PrincipalID == in.PrincipalID && binding.CredentialGeneration == in.CredentialGeneration && binding.RolesDigest == in.RolesDigest && binding.QueryDigest == in.QueryDigest
}
func QueryOriginal(binding, expected runtime.QueryBinding) bool {
	return binding.BindingID == expected.BindingID && binding.ExpiresAt == expected.ExpiresAt && QueryMatches(binding, runtime.QueryBindingInput{QueryID: expected.QueryID, PrincipalID: expected.PrincipalID, CredentialGeneration: expected.CredentialGeneration, RolesDigest: expected.RolesDigest, QueryDigest: expected.QueryDigest})
}
