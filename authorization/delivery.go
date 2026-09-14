package authorization

import (
	"context"
	"time"
)

// DeliveryTransaction is a trusted local seam. Its callback may be retried;
// external effects and nested store calls must occur outside the callback.
type DeliveryTransaction interface {
	RuntimeTransaction
	DeliveryData() []byte
	SetDeliveryData([]byte)
}

type deliveryTransaction struct {
	*runtimeTransaction
	delivery []byte
}

func (t *deliveryTransaction) DeliveryData() []byte     { return t.delivery }
func (t *deliveryTransaction) SetDeliveryData(v []byte) { t.delivery = v }

func (s *Service) UpdateDelivery(ctx context.Context, fn func(DeliveryTransaction) error) error {
	if fn == nil {
		return fail(Invalid)
	}
	return s.update(ctx, func(st *State, now time.Time) error {
		tx := &deliveryTransaction{s.runtime(st, now), st.DeliveryData}
		if err := fn(tx); err != nil {
			return err
		}
		if len(tx.delivery) > 4<<20 {
			return fail(Unavailable)
		}
		st.RuntimeData, st.DeliveryData = tx.data, tx.delivery
		return nil
	})
}
