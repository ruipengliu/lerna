//go:build !darwin || !cgo

package keys_test

import (
	"errors"
	"testing"

	"github.com/ruipengliu/lerna/contracts/command"
	"github.com/ruipengliu/lerna/infra/keys"
)

// 规则：G5、G7、G8、开始-5
func TestUnsupportedPlatformCredentialStoreRefusesExplicitly(t *testing.T) {
	store, e := keys.OpenFileBased("/synthetic/unsupported.keychain-db")
	var failure *command.Failure
	if store != nil || !errors.As(e, &failure) || failure.Detail.Code != "CREDENTIAL_STORE_UNSUPPORTED" {
		t.Fatal("unsupported platform exposed a credential store")
	}
	fixture, e := keys.NewSyntheticKeychain()
	if fixture != nil || !errors.As(e, &failure) || failure.Detail.Code != "CREDENTIAL_STORE_UNSUPPORTED" {
		t.Fatal("unsupported platform accepted synthetic keychain creation")
	}
}
