# 首次完整验证失败记录

修订 `d32529e8202f4a585de6188099556b7dd2916e30`，工作区干净，执行 `make verify`。阶段原始结果见 [阶段文件](07-parallel-verification-stages.json)。依赖、生成、编译、静态检查通过，全量 `go test -mod=readonly -race ./...` 失败，后续 profile 阶段未运行。

工具返回的失败输出节选：

```text
--- FAIL: TestControlContractChecks (6.23s)
    --- FAIL: TestControlContractChecks/uncooperative-pause (0.23s)
        checks_test.go:12: UNAVAILABLE
FAIL lerna/profiles/control
--- FAIL: TestRunningPauseWaitsForActualStop (0.27s)
    control_test.go:142: UNAVAILABLE
FAIL lerna/tasks
make: *** [Makefile:11: verify] Error 1
```

内容、授权、SDK 及其余已返回包检查通过。随后 `go test -race ./tasks ./profiles/control -run 'TestRunningPauseWaitsForActualStop|TestControlContractChecks/uncooperative-pause' -count=5` 通过。并行负载关联属于推断，未声称已定位生产缺陷。

`c213865` 将验证脚本测试包并发设为 1，各测试内部并发、race 与原业务时限不变。最终完整验证报告单独归档，不把本次失败记录改成通过。
