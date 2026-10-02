package orchestrator

import (
	"github.com/ruipengliu/lerna/internal/durable"
	"sort"
)

// RecoverTask derives responsibility from bounded current/open projections.
// It never recreates a Decision, Operation, command or reservation identity.
func (c *Coordinator) RecoverTask(tx *durable.Tx, id string) error {
	ch, t, e := c.load(tx, id, false, nil)
	if e != nil {
		return e
	}
	ch.Repair = true
	if !terminal(t) {
		queue(ch, t, "verify", "task", id)
		if t.PendingDecision != "" {
			queue(ch, t, "poll", "decision", t.PendingDecision)
		} else {
			candidates, e := c.Repository.Records(tx, Filter{TaskID: id, Kind: "candidate", Current: "candidate", Limit: 2})
			if e != nil {
				return e
			}
			if len(candidates) == 0 {
				queue(ch, t, "decide", "task", id)
			}
		}
	}
	for _, kind := range []string{"intent", "effect", "reservation", "control", "delegation", "input_preparation", "extraction"} {
		limit := c.Config.Limits.Scan
		rows, e := c.Repository.Records(tx, Filter{TaskID: id, Kind: kind, State: "open", Limit: limit + 1})
		if e != nil {
			return e
		}
		if len(rows) > limit {
			return failure("precondition_failed", "recovery projection exceeds configured finite set")
		}
		for _, r := range rows {
			switch kind {
			case "intent":
				v, e := Decode[Intent](&r)
				if e != nil {
					return e
				}
				queueAction(ch, t, "dispatch", r.ID, v.Preparation)
			case "effect":
				queue(ch, t, "poll", "operation", r.ID)
			case "reservation":
				v, e := Decode[Reservation](&r)
				if e != nil {
					return e
				}
				queue(ch, t, "settle", v.ObjectKind, v.ObjectID)
			case "control":
				v, e := Decode[ControlState](&r)
				if e != nil {
					return e
				}
				queue(ch, t, "control", "executor", v.ExecutorID)
			case "delegation":
				queue(ch, t, "poll", "delegation", r.ID)
			case "input_preparation":
				queue(ch, t, "poll", "input", r.ID)
			case "extraction":
				queue(ch, t, "extract", "extraction", r.ID)
			}
		}
	}
	if t.AllocationID != "" {
		receiver, e := c.Repository.Receiver(tx, t.AllocationID)
		if e != nil {
			return e
		}
		if receiver != nil && (terminal(t) || receiver.Receiver.State != "open") {
			queue(ch, t, "settle", "receiver", t.AllocationID)
		}
	}
	return c.flush(tx, ch, nil, durable.Done())
}

func (c *Coordinator) RecoverGlobal(tx *durable.Tx, kind, after string, limit int) (string, error) {
	if e := c.Repository.Read(tx); e != nil {
		return "", e
	}
	rows, e := c.Repository.OpenRecords(tx, kind, after, limit)
	if e != nil {
		return "", e
	}
	now, e := tx.Now()
	if e != nil {
		return "", e
	}
	works := []WorkRef{}
	for _, r := range rows {
		if kind == "receiver_work" {
			v, e := Decode[WorkRef](&r)
			if e != nil {
				return "", e
			}
			works = append(works, v)
		} else {
			works = append(works, WorkRef{Kind: "verify", ObjectKind: "defect", ObjectID: r.ID})
		}
	}
	sort.Slice(works, func(i, j int) bool { return workKey(works[i]).Responsibility < workKey(works[j]).Responsibility })
	items := []durable.JobRepair{}
	for _, w := range works {
		k := workKey(w)
		items = append(items, durable.JobRepair{Kind: k.Kind, Responsibility: k.Responsibility, Source: w.ObjectID})
	}
	if _, e = tx.RepairMany(items, now); e != nil {
		return "", e
	}

	if len(rows) < limit {
		return "", nil
	}
	return rows[len(rows)-1].ID, nil
}
