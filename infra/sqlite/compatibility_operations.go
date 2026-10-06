package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) RecoveryOperations(ctx context.Context) ([]*v1.Operation, error) {
	var all []*v1.Operation
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM operations WHERE user_id=? AND domain_id=? ORDER BY id", s.user, s.domain+"/ledger")
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			op := new(v1.Operation)
			if e = proto.Unmarshal(b, op); e != nil {
				return e
			}
			all = append(all, op)
		}
		return rows.Err()
	})
	return all, e
}

func (s *Store) RecoveryModelCalls(ctx context.Context) ([]*v1.ModelCall, error) {
	var all []*v1.ModelCall
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM model_calls WHERE user_id=? AND domain_id=? ORDER BY id", s.user, s.domain)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			call := new(v1.ModelCall)
			if e = proto.Unmarshal(b, call); e != nil {
				return e
			}
			all = append(all, call)
		}
		return rows.Err()
	})
	return all, e
}
