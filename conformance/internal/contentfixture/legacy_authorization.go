package contentfixture

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ruipengliu/lerna/domain/content"
)

// FixInitialLegacyCutoff records a host's shorter initial upgrade authorization
// before constructing or starting its consumer. It never renews a failed scope.
func (w *World) FixInitialLegacyCutoff(original content.LegacyPrimaryQualification, until time.Time) content.LegacyPrimaryQualification {
	w.t.Helper()
	if original.Namespace != w.Config.Schema || original.ID != "ticket05-stopped-"+w.Config.Schema || original.Binding != fmt.Sprintf("linux-directory:%d:%d", w.device, w.inode) || !time.Now().Before(until) || !until.Before(original.ValidUntil) {
		w.t.Fatal("initial legacy authorization is not the original shorter scope")
	}
	original.ValidUntil = until
	encoded, err := json.Marshal(struct {
		Event         string                             `json:"event"`
		Qualification content.LegacyPrimaryQualification `json:"qualification"`
	}{"legacy_initial_short_upgrade_authorization", original})
	if err != nil {
		w.t.Fatal(err)
	}
	if err = w.register(string(encoded)); err != nil {
		w.t.Fatal(err)
	}
	return original
}
