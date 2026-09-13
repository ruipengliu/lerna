# 上下文装配超时误报权限拒绝

第 20 票回归在 `TestExtractedPreferenceControlsIndependentTaskOutput` 的真实跨任务上下文装配处偶发返回 `PERMISSION_DENIED`。本轮确认的原因是装配自身五秒预算耗尽后，下游受控读取的失败关闭结果被原样当作权限结论返回。修复范围是超时/取消分类及禁止释放正文，没有扩大预算或授权。

## 复现与定位

代码基点为 `52bd74f`，Linux、Go 1.26.1、race 构建，使用隔离目录的真实授权、Core、SQLite、来源文件、Memory 和 Context；无外部模型请求。构建命令：

```sh
GOTOOLCHAIN=go1.26.1 go test -race -c -o build/extraction-diagnosis.test ./profiles/extractioncheck
```

以三个独立进程并行运行下列命令，每个进程两次。复现机器 CPU 0 在进程可用 CPU 集合内；其他机器应选其允许的同一个 CPU。

```sh
taskset -c 0 build/extraction-diagnosis.test \
  -test.run='^TestExtractedPreferenceControlsIndependentTaskOutput$' \
  -test.count=2 -test.v -test.timeout=180s
```

- 不限制 CPU 的三个进程、共六次运行全部通过，证据 `evidence/20-context-stress-{0,1,2}.log`。
- 固定到同一 CPU 的三个进程、共六次运行全部在原装配位置失败，其中五次权限拒绝、一次不可用，证据 `20-context-single-cpu-{0,1,2}.log`。
- 同一 CPU 单进程通过，证据 `20-context-one-process-one-cpu.log`。因此复现保留了 CPU 竞争条件。

先比较外层预算、较短依赖超时和授权状态变化三个原因，再在真实 Memory Validate 入口加入临时阶段/耗时/`ctx.Err()` 探针。三个受限进程的最后失败均位于 retention 阶段，上下文已是 `context deadline exceeded`；该次 Validate 仅经过约 0.54–0.63 秒，但装配的共享时间已耗尽。证据 `20-context-probe-{0,1,2}.log`。日志中的 scope 快速拒绝是此前测试故意使用错误任务身份的反例，不是本次故障。探针不包含凭证或正文，已从代码删除。

## 修复与回归

在真实已保存上下文的保留权限复查处定点取消，再调用实际 Memory/grant 实现，稳定得到原来的错误分类。红灯证据 `20-context-cancellation-red.log` 中 `fired=true err=PERMISSION_DENIED`，用例约 2.8 秒结束。此精确故障注入在原 profile 接口边界进行，不用成功桩替代读写。

`Assembler.Assemble` 和 `Assembler.Validate` 现在在返回边界检查自己的尝试上下文：若已结束，返回 `CONTEXT_UNAVAILABLE`；Assemble 同时清空结果正文。原持久快照不因一次运行超时被宣称失效，后续正常尝试仍沿原身份核对。上下文有效时仍保留原权限拒绝、冲突等错误。

真实路径同时验证 Assemble 和 Validate 的定点取消、无正文释放及随后正常使用，绿色证据 `20-context-cancellation-green.log`。再次运行原单 CPU 三进程条件，三次均为 `CONTEXT_UNAVAILABLE`，未再出现 `PERMISSION_DENIED`，证据 `20-context-classified-{0,1,2}.log`。这些压力运行仍然以非零退出，因为原正向测试要求成功；它们只证明超时分类已改变，不证明受限 CPU 下成功率或性能已提高。

相关串行包 race 回归通过，见 `20-context-classification-regression.log`：contextassembly、contextmemory、taskcontext 和 extractioncheck 的测试通过；contextpolicy 和 profiles/contextcheck 没有独立测试，未将其编译成功记为行为验证。相关 vet/diff 检查通过，临时诊断二进制已删除。第 20 票仍须完成其余功能、完整仓库验证与双轴审查；这份诊断不替代整票验收。
