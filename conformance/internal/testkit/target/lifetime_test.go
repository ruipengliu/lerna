package target

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/internal/sqlclosetest"
)

// This is a mechanical database/sql lifetime test, with no physical database
// scope. It does not claim a native SQLite fault or a business effect.
func TestTargetClosePreservesFirstNativeFailure(t *testing.T) {
	cause := errors.New("mechanical native close failure")
	db := sqlclosetest.Open(sqlclosetest.Fault{CloseError: cause})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	released := false
	holder := &Target{db: db, cfg: Config{IOTimeout: time.Second}, gate: make(chan struct{}, 1), release: func() error {
		released = true
		return nil
	}}
	if err := holder.Close(); !errors.Is(err, cause) {
		t.Fatalf("first Close lost original cause: %v", err)
	}
	if err := holder.Close(); !errors.Is(err, cause) {
		t.Fatalf("second Close erased unknown native outcome: %v", err)
	}
	if released {
		t.Fatal("writer released despite unconfirmed native closure")
	}
}
