# 核实 Go 扩展运行器与隔离能力

Type: research
Interaction: AFK
Labels: wayfinder:research
Status: resolved
Assignee: extension_runtime_research
Blocked by:
Parent: [Harness 技术实现方案决策地图](../map.md)

## Question

在 Go、单机且无必需独立基础设施的条件下，受信编译扩展、独立进程和嵌入式 WebAssembly 运行器分别能提供什么加载、资源限制、宿主访问及中断能力，哪些不受信代码不能默认接纳？

## Scope

核对 Go 编译/动态插件边界、wazero 等一个合适的嵌入式候选的官方运行限制，以及任意本机脚本的进程隔离边界。区分可运行接口与安全隔离；不实现运行器，不扩展成全面产品选型。

## Output

`docs/research/harness-extension-runtime-boundaries.md`。

## Comments

由扩展生命周期票细化时产生的事实问题，研究在独立 worktree 和 research 分支进行。根会话继续完善与具体运行器无关的清单和激活恢复设计。

## Answer

已完成官方文档与固定源码核对，报告：[Harness 扩展运行器与隔离边界](../../../docs/research/harness-extension-runtime-boundaries.md)。研究分支 `research/harness-extension-runtime`，worktree `/tmp/lerna-wayfinder-extension-runtime`，报告提交 `15dc41116de1fcdb7c1157a54d78636d41647a41`；已整合至主工作区。

- 受信 Go 实现可随宿主编译；Go 动态 plugin 受平台、构建耦合和不可卸载限制，普通子进程也不自动获得文件、网络或进程树隔离。
- wazero v1.12.0 是可嵌入 Go 的候选，最低 Go 1.25.0，无必需独立服务；支持范围按 Core Wasm、自定义 ABI 和按需 WASI Preview 1 描述。
- 默认不挂载宿主目录；只读挂载不保证路径隔离。环境、网络、时间和随机源的真实能力均须显式控制。
- 内存页限制不等于宿主 RSS 上限；必须显式启用 context 中断，并约束实例化及宿主回调。引擎不能强杀任意阻塞 Go 回调，也不自动恢复外部副作用。
- 报告中的资源额度仅为实验起点；首个参考平台、具体预算与运行器选择仍由扩展方案及后续评测确定。

没有安装依赖或运行实验，不声明安全审计、性能或平台验收通过。事实调查已完成，具体候选采纳由 [确定插件、Skill 与外部 Agent 的接入生命周期](12-extension-lifecycle.md) 决定。
