package interaction

import lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"

// 以下把核心契约的状态翻译成界面文字；界面状态由各维度组合得出，不单独存储。

func phase(p string) string {
	switch p {
	case "RUNNING":
		return "运行中"
	case "WAITING":
		return "等待中"
	case "PAUSED":
		return "已暂停"
	case "CANCELLING":
		return "取消已保存，部分动作可能仍在核对"
	case "SUCCEEDED":
		return "已成功"
	case "FAILED":
		return "已失败"
	case "CANCELLED":
		return "已取消"
	}
	return p
}

func routing(s lernav1.RoutingStatus) string {
	switch s {
	case lernav1.RoutingStatus_ROUTING_STATUS_RECORDED:
		return "已记录，任务尚未接纳"
	case lernav1.RoutingStatus_ROUTING_STATUS_TASK_ACCEPTED:
		return "任务已接纳"
	case lernav1.RoutingStatus_ROUTING_STATUS_PROCESSED:
		return "已处理"
	case lernav1.RoutingStatus_ROUTING_STATUS_REJECTED:
		return "处理被拒绝"
	}
	return s.String()
}

func inputKind(k lernav1.InputKind) string {
	switch k {
	case lernav1.InputKind_INPUT_KIND_NEW_GOAL:
		return "新目标"
	case lernav1.InputKind_INPUT_KIND_MODIFY_TASK:
		return "修改任务"
	case lernav1.InputKind_INPUT_KIND_ANSWER:
		return "回答"
	case lernav1.InputKind_INPUT_KIND_CONFIRMATION:
		return "确认"
	case lernav1.InputKind_INPUT_KIND_RECORD_ONLY:
		return "仅记录"
	case lernav1.InputKind_INPUT_KIND_CONTROL:
		return "控制"
	}
	return k.String()
}

func reqStatus(s lernav1.RequirementSetStatus) string {
	if s == lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_ACCEPTED {
		return "已接纳"
	}
	return "草稿，待澄清"
}

func effect(e lernav1.EffectOutcome) string {
	switch e {
	case lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED:
		return "已生效"
	case lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED:
		return "未生效"
	case lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN:
		return "未知"
	}
	return "尚无回报"
}

func late(l lernav1.LateEffect) string {
	switch l {
	case lernav1.LateEffect_LATE_EFFECT_RULED_OUT:
		return "不会迟到生效"
	case lernav1.LateEffect_LATE_EFFECT_MAY_OCCUR:
		return "可能迟到生效"
	}
	return "迟到可能性未回报"
}

func settled(op *lernav1.OperationView) string {
	if op.GetSettled() {
		return "已收尾"
	}
	if !op.GetLedgerAccepted() {
		return "交接中（执行管理尚未确认接纳）"
	}
	return "未收尾"
}

func outcome(o lernav1.TaskOutcome) string {
	switch o {
	case lernav1.TaskOutcome_TASK_OUTCOME_SUCCEEDED:
		return "成功"
	case lernav1.TaskOutcome_TASK_OUTCOME_FAILED:
		return "失败"
	case lernav1.TaskOutcome_TASK_OUTCOME_CANCELLED:
		return "已取消"
	}
	return o.String()
}

func verdict(v lernav1.Verdict) string {
	switch v {
	case lernav1.Verdict_VERDICT_SATISFIED:
		return "满足"
	case lernav1.Verdict_VERDICT_UNSATISFIED:
		return "未满足"
	case lernav1.Verdict_VERDICT_UNKNOWN:
		return "未知"
	}
	return v.String()
}
