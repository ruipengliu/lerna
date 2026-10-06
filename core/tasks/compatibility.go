package tasks

import (
	"context"
	"encoding/hex"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type recoveryModelCalls interface {
	RecoveryModelCalls(context.Context) ([]*v1.ModelCall, error)
}

type recoveryReasonerDrivers interface {
	RecoveryReasonerDrivers(context.Context) ([]*v1.ReasonerDriver, error)
}

// CheckStartupCompatibility 只核验原模型和推理历史解释资格，不重新编码或推进负责方。
func (s *Service) CheckStartupCompatibility(ctx context.Context) error {
	all, e := s.store.(recoveryModelCalls).RecoveryModelCalls(ctx)
	if e != nil {
		return e
	}
	for _, call := range all {
		if e = checkSavedModelCall(call); e != nil {
			return e
		}
	}
	drivers, e := s.store.(recoveryReasonerDrivers).RecoveryReasonerDrivers(ctx)
	if e != nil {
		return e
	}
	for _, driver := range drivers {
		if e = command.CheckSavedHeaders(driver); e != nil {
			return e
		}
		if e = s.checkReasonerDriverVersion(driver); e != nil {
			return e
		}
		// 配置原文允许合法非规范化 JSON；核验后丢弃规范化副本，不重写原设置。
		if _, e = normalizedModelSettings(driver.Policy.Settings); e != nil {
			return e
		}
	}
	return nil
}

func checkSavedModelCall(call *v1.ModelCall) error {
	if e := command.CheckSavedHeaders(call); e != nil {
		return e
	}
	settings, e := normalizedModelSettings(call.GetSettings())
	if e != nil || !proto.Equal(settings, call.GetSettings()) {
		return command.Fail("PREPARATION_UNRECOVERABLE")
	}
	if call.State != "PREPARING" {
		digest, e := hex.DecodeString(call.DescriptorDigest)
		if e != nil || len(digest) != 32 {
			return command.Fail("PREPARATION_UNRECOVERABLE")
		}
	}
	return nil
}
