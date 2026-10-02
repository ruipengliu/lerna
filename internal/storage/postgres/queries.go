package postgres

import (
	q "github.com/ruipengliu/lerna/internal/storage/postgres/querygen"
	"github.com/ruipengliu/lerna/internal/storage/sqlstore"
)

var queries = sqlstore.Queries{Now: q.DBNow, Reserve: q.ReserveCommand, Lookup: q.LookupCommand, Save: q.SaveCommand, Raise: q.RaiseJob, Hint: q.HintJob, Lock: q.LockJob, Candidates: q.ClaimCandidates, Lease: q.LeaseJob, Update: q.UpdateJob, Delete: q.DeleteDone, Open: q.OpenJobs}
