package content

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"sort"
)

const VersionIdentityAlgorithm = "lerna-content-version-1"

func VersionIdentity(ref v.ContentRef) (string, string, error) {
	if _, err := v.Encode(ref); err != nil {
		return "", "", err
	}
	// IDs and positive decimal versions are ASCII, so this closed positional
	// array has exactly the no-number canonical JSON encoding.
	data, err := json.Marshal([]string{string(ref.Owner.TenantID), string(ref.Owner.OwnerID), string(ref.ContentID), string(ref.Version)})
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(append([]byte(VersionIdentityAlgorithm+"\n"), data...))
	key := hex.EncodeToString(sum[:])
	return "cv-" + key, key, nil
}
func TupleDigest(payload v.ContentPutPayload) (string, error) {
	sources := append([]v.ContentRef{}, payload.Sources...)
	sort.Slice(sources, func(i, j int) bool {
		left, _ := v.Encode(sources[i])
		right, _ := v.Encode(sources[j])
		return string(left) < string(right)
	})
	payload.Sources = sources
	data, err := v.Encode(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("lerna-content-declaration-1\n"), data...))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
func (r Record) ValidateIdentity() error {
	id, key, err := VersionIdentity(r.Ref)
	if err != nil {
		return err
	}
	if id != r.ObjectID || key != r.ObjectKey {
		return errors.New("Content version identity mismatch")
	}
	return nil
}
