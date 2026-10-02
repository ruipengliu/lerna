package orchestrator

import (
	"github.com/ruipengliu/lerna/internal/durable"
)

func (c *Coordinator) expireInputs(tx *durable.Tx, ch *Change, t *TaskState) error {
	rows, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "input", State: "open", Limit: c.Config.Limits.Checks + 1})
	if e != nil {
		return e
	}
	if len(rows) > c.Config.Limits.Checks {
		return durable.ErrInvariant
	}
	for _, r := range rows {
		v, e := Decode[InputState](&r)
		if e != nil {
			return e
		}
		if ch.Now.Before(parseTime(v.View.Deadline)) {
			continue
		}
		v.View.State = "expired"
		v.View.Revision++
		if e = c.Repository.Put(tx, t, rec("input", r.ID, t.Task.TaskID, v.View.Revision, "closed", false, v)); e != nil {
			return e
		}
		if e = c.Repository.Put(tx, t, rec("input_revision", ID("input_revision", r.ID, Hash(v.View.Revision)), t.Task.TaskID, v.View.Revision, "closed", true, v)); e != nil {
			return e
		}
		clearWait(t, "input", r.ID)
		t.NoProgressCount++
		t.RepairCount++
		t.Task.Revision++
		queue(ch, t, "decide", "task", t.Task.TaskID)
		if e = c.persist(tx, ch, t); e != nil {
			return e
		}
	}
	return nil
}
