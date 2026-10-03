package development_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/adapters/development"
	rpc "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/api"
)

func TestPublicEndpointChannelClosesUnpairedSubjectBeforeOpeningDatabase(t *testing.T) {
	root := t.TempDir()
	c := development.DevelopmentConfig(root, "postgres")
	c.DatabaseID = api.NewID("database")
	c.DSNEnv = "HARNESS_UNCONFIGURED_ENDPOINT_CHANNEL_TEST_DSN"
	c.DevDatabaseEnvFile = ""
	c.EndpointChannels = &development.EndpointChannelConfig{GatewayInstanceID: api.NewID("instance"), ApplicationInstanceID: api.NewID("instance"), ApplicationAddresses: []string{"grpcs://127.0.0.1:12345"}, GatewayIdentities: []string{"spiffe://harness.test/gateway"}, Registrations: []rpc.EndpointRegistration{{TenantID: api.NewID("tenant"), SubjectID: c.SubjectID, CredentialGeneration: 1, EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), Generation: 1, RecipientServiceID: c.OwnerID}}}
	path := filepath.Join(root, "channel.json")
	if err := development.SaveConfig(path, c); err != nil {
		t.Fatal(err)
	}
	if _, err := development.LoadConfig(path); !api.IsCode(err, "forbidden") {
		t.Fatalf("unpaired channel config was not rejected before database/target IO: %v", err)
	}
	if app, err := development.OpenAppForRole(context.Background(), c, false, "application"); !api.IsCode(err, "forbidden") {
		if app != nil {
			app.Close()
		}
		t.Fatalf("programmatic unpaired channel config reached database IO: %v", err)
	}
}
