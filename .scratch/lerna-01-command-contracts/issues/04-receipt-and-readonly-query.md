# 04: 固定回执与只读查询

**What to build:** 调用方经 command.get 公开合同入口读取原决定和当前异步进展，准确区分接纳事实、业务进展和传输不确定性；同版 Go／TypeScript 能编解码这些结果而不误报 Task 完成。

**Blocked by:** 02 — 严格命令解析与验证。

**Status:** resolved

- [x] CommandReceipt 的闭合 Schema 与生成类型包含原 command_ref、accepted／applied／rejected、关联 object_ref、revision 及适用的 reason、next_action；字段存在条件明确，不用空字符串代替不存在的引用。
- [x] command.get 输出分别表达原固定回执与当前异步进展；accepted 表示接纳责任，applied 表示该方法的修改成功点，均不能推导为 Task succeeded。
- [x] commit_unknown 属于传输结果，不能成为 CommandReceipt.state；conflict 作为固定拒绝原因表达，不能成为第四种接纳状态。
- [x] 不可用、not_found、gone 和可读取原决定可明确区分；not_found 只表示当前原 owner 未找到，gone 保留最小身份，不能把这些结果当作新建同义工作的许可。
- [x] 通过可控只读事实源和公开查询入口，演示进展从处理中变为成功或失败时，原回执仍保持同一决定；所有结果均有 Go／TypeScript 共同正反例与状态组合检查。
- [x] 查询入口只读取已有事实，不触发模型、安装、动作或新的业务责任；用公开可观察事实验证只读性，不依赖私有函数调用次数。
- [x] 正常查询、暂时不可用、未找到、正文已清理及无权视图的合同都可验证；明确区分合同夹具与真正持久化恢复证据。

## Scope

本任务使用可控只读事实源验证公开行为，不建立生产命令账本，也不实现接纳事务或网络恢复。入口的受信鉴权与 owner 隔离由任务 05 完成，真实持久事实由后续实现切片提供。

## Comments

Implemented 2026-10-03 on isolated branch `codex/contract-ticket-04`, based on ticket02 integration exit `c0e12f7`. Exact unpublished machine contract and generated Go / TypeScript types remain version `1.0.0`; generator `1.0.0` now supports named, uniquely discriminated closed oneOf variants. No future Task method is registered. Shape and relationship details follow `receipt-decisions.md` and `query-decisions.md`.

Public result seams: generic Go Decode / Encode / Validate and TS decode / encode / validate, plus bound DecodeCommandResponse / EncodeCommandResponse and TS equivalents. Closed receipt states have distinct generated variants; Go's private union wrapper offers New / As functions and rejects an empty union. TS has genuine discriminated unions. Generic paths also enforce cross-field bindings, exact revision monotonicity and nested revision consistency; a special helper is not required to reject contradictory found results. The bound response helpers additionally verify the request's original command_ref, independently of its read association command_id. Backend invalidity returns unavailable / dependency_unavailable with the original reference and no backend detail.

ReadCommandFacts / readCommandFacts are deliberately low-level, unauthenticated fact primitives for authorized assembly and controlled fixtures. The TypeScript primitive is not exported from the SDK facade. Fact sources expose no admission, Job, model, installation or action methods. Publicly observed fixture facts stay unchanged across reads while progress advances from active to succeeded or failed and the accepted receipt remains exact. This read's start cutoff is enforced with a controlled clock; original write expiry does not prohibit historical reads. Context / AbortSignal propagate cancellation. Ticket05 still supplies authentication, authorization and fixed-owner directory resolution before any production read.

Observed red → green tracer cycles: the fixed-receipt public roundtrip initially failed to compile because no CommandReceipt or safe variant existed; after the generated union and schemas were added, roundtrip and the shared state/revision corpus passed. The controlled readonly read tests then failed to compile because ReadCommandFacts did not exist; after introducing that minimal primitive, normal and fault observations passed. TS readonly fault assertions exposed a test comparing parser object prototypes rather than wire observations; assertions were corrected to compare JSON facts without changing parser behavior. Additional corpus cases and binding/generation regression checks exercise existing strict validators rather than claiming a separate initial red cycle for every fixture.

Verification: `make check` and `make test-race` passed using repository-pinned Go 1.27.1, Node 24.19.0, pnpm 12.8.1 and TypeScript 7.0.2. Check includes format, Go vet, strict TS typecheck, generation consistency, 8 generation refusal probes, Go tests, 17 TS public tests, both builds and 153 shared value/command/response fixtures; every accepted fixture completed real Go→TS and TS→Go encode/decode. There are 44 response fixtures covering normal/terminal Task observations, absent/unavailable/gone/forbidden views, fixed revision 5/current 6, optional nested versions, exact integers beyond Number.MAX_SAFE_INTEGER, illegal cross-field and state combinations, and transport uncertainty. `git diff --check` passed. No production persistence, signature authentication, network recovery or durable receipt guarantee is claimed; only ticket04 is resolved and the overall spec stays in progress.

Integration verification: merged ticket03 tip `3c156a6` into this branch, preserving its SubjectBinding / DelegatedSubject schemas, digest implementation and public exports; shared generated files were rebuilt from both inventories. After merge, `make check` passed with 153 bidirectional value/command/response fixtures **plus** 46 independently specified digest cases in the Go and TS digest suites (199 combined corpus cases, not 199 typed-roundtrip fixtures). The merge did not reinterpret digest goldens as response fixtures. `make test-race` passed again.

Readonly review found two substantive alias issues with independent red → green evidence. The Go returned optional revision pointer and TS returned receipt object could be used by a caller to mutate facts retained by an injected reader; both failed public read tests before returned observations were detached through validated wire decoding. The TS reader could also mutate its input CommandRef and trick the result check into accepting another owner; an explicit fault test failed with gone / wrong owner, then passed as unavailable / original owner after passing an independent copy to the reader. Final readonly tests prove both source facts and original owner remain unchanged. After the last TS input-copy change, `make lint test build` and generation consistency were checked again; all 21 TS tests and Go suites passed. These changes preserve the contract and avoid introducing write APIs or new support claims.
