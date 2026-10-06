# 16: 默认推理

**What to build:** 提供 M1 的默认推理：根据目标、条件版本、进展和证据来源，确定性地选择和裁剪上下文，经模型调用产生提议，解析四类提议。用户用真实模型（或模拟供应商）运行一个简单任务能得到合理提议。

**Blocked by:** 15（模型调用经出口闸门）

**Status:** resolved

- [x] 上下文快照只用确定性的选择和裁剪，不做模型摘要
- [x] 每个提议请求最多一个调用位置；关闭自动重试、降级、修复和摘要
- [x] 解析四类提议；提议绑定版本、引用证据并说明缺口
- [x] 有限计划长度上限为 1，参数只允许具体值
- [x] 可以机器核验的条件使用明确核验规则；主观条件关联用户确认
- [x] 与脚本化推理通过同一套推理接口一致性测试

## Answer

Implemented the governed default reasoner and a persistent, bounded task driver exposed through the production CLI. Context selection and clipping are deterministic; each request retains its original position 0 without automatic retry, repair, fallback or summaries. ACTION, QUESTION, REQUIREMENTS and COMPLETE use the shared default/scripted contract, exact schema and derived concrete parameters. Condition findings and subjective confirmations retain their original governed evidence and current authority.

The production driver resumes original requests, admissions, model outcomes and physical sends. Reliable UNSAT candidates reach the independent completion owner; rejection continues only through the exact immutable `Verification.ContinuationRequestRef` and saved snapshot, with fresh authority for a remedy. Actual start-receipt read failure waits for the original completion owner. Original closed/no-send P4 proofs permit progress without inventing an observation; UNKNOWN remains blocked. Real cancellation preserves original model fees and unsent actions, then completes only from the trusted closing protocol's fixed CANCELLED Result and both original ACKs.

Whole validation passed on frozen tree `84bd85b73a48c6f776d9ea9301308d2982b57763`, based only on formal14 `48389affa72b393ca6612209a5b273a7a52689ca`. The original untrimmed `env GOFLAGS=-v BUF_BASE=48389affa72b393ca6612209a5b273a7a52689ca make check` ran from 2026-10-06 06:55:49 UTC to 08:26:27 UTC: exit 0, 5438.694 seconds total and 5059.754 seconds for the complete race/fault package. All 669 file hashes/modes and 363 runtime/contract/SQL/test source hashes matched the freeze. All 120 original fault parents were accounted for: 88 PASS and 32 subprocess-only helpers with their actual skip reasons. Planning's nine commit cases, cancellation's 27 cases, both closing protocols' 42 cases each and all negative controls passed.

Actual current storage evidence: 439 I/O events / 2105 images; bootstrap 1537 events / 7690 images; P4 51 events / 260 images with one real subjournal open and 18 writes; native 14 events / 225 images across five byte and three namespace policies; joint 978 SQLite plus 14 native events / 993 chronological cuts / 4965 paired images. Every emitted matrix prefix passed all five policies. Production remedy counters were MODEL 4/bills 4, target requests 2/effects 1/bills 2, settled 34/reserved 0. Production CANCELLED counters were MODEL 1/bill 1/fee 7 and target requests/effects/bills 0, stable across replay/restart. Current named fault-occurrence selector tests also passed independently in 1.865 seconds.

Acceptance details are recorded in [ADR0012](../../../docs/adr/0012-bounded-reasoner-driver.md). Permanent raw evidence: `ticket16-full-make-check.log`, SHA256 `e04bcb95df95fff0d1d70fcde0b9b2cda02af71b3dd61bae9b584a36efdb0284`, and `ticket16-full-result-summary.json` / `ticket16-full-acceptance-audit.json`. This qualifies the captured finite recovery images on the recorded macOS/APFS platform, not physical power cuts.
