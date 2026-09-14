package authorization_test

import (
	"context"
	"errors"
	"lerna/adapters/sqliteauth"
	"lerna/authorization"
	"path/filepath"
	"testing"
)

func TestDeliveryAndRuntimeCommitTogetherAfterReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "authority.db")
	db, err := sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a, err := authorization.New(db, authorization.SystemClock{}, config())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Bootstrap(ctx, "local", "admin"); err != nil {
		t.Fatal(err)
	}
	if err = a.UpdateDelivery(ctx, func(tx authorization.DeliveryTransaction) error {
		tx.SetDeliveryData([]byte("inbox:pending"))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, err = authorization.New(db, authorization.SystemClock{}, config())
	if err != nil {
		t.Fatal(err)
	}
	aborted := errors.New("abort")
	process := func(tx authorization.DeliveryTransaction) error {
		if string(tx.DeliveryData()) != "inbox:pending" || len(tx.Data()) != 0 {
			t.Fatal("lost pending record or partial commit")
		}
		tx.SetData([]byte("business:accepted"))
		tx.SetDeliveryData([]byte("inbox:consumed;outbox:reply"))
		return nil
	}
	if err = a.UpdateDelivery(ctx, func(tx authorization.DeliveryTransaction) error {
		if e := process(tx); e != nil {
			return e
		}
		return aborted
	}); !errors.Is(err, aborted) {
		t.Fatal(err)
	}
	if err = a.UpdateDelivery(ctx, process); err != nil {
		t.Fatal(err)
	}
	if err = a.UpdateDelivery(ctx, func(tx authorization.DeliveryTransaction) error {
		if string(tx.Data()) != "business:accepted" || string(tx.DeliveryData()) != "inbox:consumed;outbox:reply" {
			t.Fatal("non-atomic result")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
