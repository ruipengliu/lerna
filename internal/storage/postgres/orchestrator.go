package postgres

import (
	q "github.com/ruipengliu/lerna/internal/storage/postgres/querygen"
	"github.com/ruipengliu/lerna/internal/storage/taskstore"
)

func TaskQueries() taskstore.Queries {
	return taskstore.Queries{GetMany: q.OrchestratorGetRecords, PutMany: q.OrchestratorSaveRecords, DeselectKeys: q.OrchestratorDeselectKeys, Routes: q.OrchestratorSaveRoutes,
		BatchLockTasks: q.OrchestratorBatchLockTasks, BatchLockBalances: q.OrchestratorBatchLockBalances, CurrentRecords: q.OrchestratorCurrentRecords, OpenTaskRecords: q.OrchestratorOpenTaskRecords, SaveBalances: q.OrchestratorSaveBalances,
		SaveRoute: q.OrchestratorSaveRoute, ScheduleCreate: q.OrchestratorScheduleCreate, ScheduleLock: q.OrchestratorScheduleLock, ScheduleAdvance: q.OrchestratorScheduleAdvance, ScheduleTurns: q.OrchestratorScheduleTurns, FairCandidates: q.OrchestratorFairCandidates,
		Task:           q.OrchestratorGetTask,
		LockTask:       q.OrchestratorLockTask,
		SaveTask:       q.OrchestratorSaveTask,
		Tree:           q.OrchestratorTree,
		Chain:          q.OrchestratorChain,
		List:           q.OrchestratorList,
		Recovery:       q.OrchestratorRecovery,
		Balances:       q.OrchestratorBalances,
		LockBalances:   q.OrchestratorLockBalances,
		SaveBalance:    q.OrchestratorSaveBalance,
		Record:         q.OrchestratorGetRecord,
		Records:        q.OrchestratorRecords,
		OpenRecords:    q.OrchestratorOpenRecords,
		SaveRecord:     q.OrchestratorSaveRecord,
		Deselect:       q.OrchestratorDeselect,
		GateCreate:     q.OrchestratorGateCreate,
		LockGate:       q.OrchestratorLockGate,
		ReadGate:       q.OrchestratorReadGate,
		GateAdvance:    q.OrchestratorGateAdvance,
		CapacityCreate: q.OrchestratorCapacityCreate,
		LockCapacity:   q.OrchestratorLockCapacity,
		CapacityChange: q.OrchestratorCapacityChange,
		Receiver:       q.OrchestratorGetReceiver,
		SaveReceiver:   q.OrchestratorSaveReceiver,
	}
}
