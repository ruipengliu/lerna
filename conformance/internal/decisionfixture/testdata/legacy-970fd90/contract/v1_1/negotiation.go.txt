package v1_1

import wire "github.com/ruipengliu/lerna/contract/gen/go/v1_1"

// SupportedMethods describes the completed local contract paths, not a network
// discovery response or a promise of production storage or authentication.
func SupportedMethods() []MethodSupport { return wire.SupportedMethods() }

// Negotiate requires an exact version, profile, method and both complete schema
// digests. It checks compatibility only; it never authorizes or executes work.
func Negotiate(data []byte) (MethodSupport, error) {
	var result MethodSupport
	request, err := Decode[NegotiationRequest](data)
	if err != nil {
		return result, refusal("schema_invalid", err)
	}
	if request.ContractVersion != Version {
		return result, refusal("version_unsupported", nil)
	}
	for _, method := range SupportedMethods() {
		if request.ContractVersion == method.ContractVersion && request.Profile == method.Profile && request.Method == method.Method {
			if request.InputSchemaDigest != method.InputSchemaDigest || request.OutputSchemaDigest != method.OutputSchemaDigest {
				return result, refusal("version_unsupported", nil)
			}
			return method, nil
		}
	}
	return result, refusal("unsupported", nil)
}

// DeclaredMethods exposes complete development schema fingerprints without claiming availability.
func DeclaredMethods() []MethodSupport { return wire.DeclaredMethods() }
