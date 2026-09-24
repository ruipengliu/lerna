# Harness 扩展运行器与隔离边界

研究日期：**2026-09-09**。范围：Go、单机、无必需独立基础设施的扩展运行参考方案；仅核对官方文档与固定版本源码，未安装或运行候选。本报告的“事实”指可追溯的 API／源码契约，“建议／推论”指由这些事实导出的 Harness 设计判断，不表示已完成安全审计或验收。

## 1. 可采用的最小边界

**建议：受信 Go 插件随 Harness 编译；动态安装的受限插件优先验证嵌入式 wazero；任意本机 Python／shell 脚本只进入显式受信模式，或交给已声明能力的外部沙箱执行后端。** 后者是可选扩展要求，不应把 Docker、常驻系统服务或远端基础设施变成最小安装的前置条件。依据分别见下列 Go、Wasm 和子进程事实。

这里“插件”仍指领域文档中可安装的实现；编译内置是其中一种交付方式。`wasm` 文件、Go `plugin` 包及操作系统子进程是实现机制，不是三个安全等级的自动证明。

| 机制 | 可以承诺的运行能力 | 最小方案的接纳条件（建议） |
| --- | --- | --- |
| 随宿主编译的 Go 实现 | 直接调用 Go 接口；版本随宿主发布 | 必须受信；共享宿主权限、内存与故障域 |
| Go `plugin` 动态库 | 打开动态库、查找导出符号；初始化一次 | 不作为通用安装／卸载方案；平台和构建耦合见 §2 |
| 普通本机子进程 | IPC、独立地址空间、进程退出和重启 | 必须显式受信，或额外配置并验证操作系统沙箱 |
| 内嵌 wazero | 验证／编译／实例化 Wasm、受控导入、线性内存限制、可取消调用 | 只接受固定 ABI 与允许的导入；默认无宿主目录、网络和环境继承；仍有 §3 的边界 |

## 2. Go 扩展与子进程的事实

**Go `plugin`。** 官方包说明限定 Linux、FreeBSD、macOS；首次打开会运行尚未初始化的包的 `init`，插件不能关闭。宿主和插件必须匹配工具链版本、构建标签、部分编译参数／环境以及公共依赖源码，否则可能崩溃；race detector 对插件支持也有限。官方直接指出，同一构建方统一构建时，可以生成导入代码并编译普通静态程序。[Go plugin 官方说明](https://go.dev/src/plugin/plugin.go)

**推论：** 不把 `plugin.Open` 当成“加载前无副作用的检查”；不承诺卸载释放代码、热替换、独立版本兼容或强制停止任意 Go 回调。受信编译扩展的禁用是停止路由调用；代码更新和彻底清理可通过宿主重启完成。接口版本和清单权限能约束合作实现，无法阻止同进程恶意 Go 代码直接调用操作系统。

**子进程。** `os/exec` 执行外部命令，默认不启动 shell；`Cmd.Env == nil` 继承宿主环境。`CommandContext` 默认取消动作是杀死该 `Process`，且默认不设置 `WaitDelay`；它不是完整进程树清理契约。[Go os/exec](https://pkg.go.dev/os/exec#Cmd) Linux `execve` 不改变真实 UID、GID 和附加组；名称空间等隔离需另外建立。[Linux execve](https://man7.org/linux/man-pages/man2/execve.2.html)、[Linux namespaces](https://man7.org/linux/man-pages/man7/namespaces.7.html)

**推论：** 只把脚本放入子进程，并不会撤销宿主用户已有的文件、网络或凭证访问权。清空环境、指定工作目录和设置超时是运行卫生措施，不足以把不受信本机代码变成可默认执行的代码。外部沙箱后端应声明实际平台、文件／网络权限、身份、资源额度及进程树终止能力；缺失所需能力就拒绝调度，不静默降级为普通子进程。任意脚本的依赖安装、Python 原生扩展和 shell 命令也不由嵌入 Wasm 引擎自动解决。

## 3. 固定候选：wazero v1.12.0

### 3.1 版本、依赖与目标平台

选定用于后续验证的稳定发布 **`github.com/tetratelabs/wazero v1.12.0`**，对应提交 **`2ab480b55fa408d6b35df97fe32a60d08bd6e201`**。这是可复核候选，不承诺它是访问日的最新版本。官方发布页显示该发布并列出行为修正；完整提交由官方仓库 tag ref 核对。[发布记录](https://github.com/wazero/wazero/releases/tag/v1.12.0)、[tag ref](https://api.github.com/repos/wazero/wazero/git/ref/tags/v1.12.0)

固定版本的 `go.mod` 要求 **Go 1.25.0**，依赖 **`golang.org/x/sys v0.44.0`**。因此不能照抄 README 标题的“zero dependency”来宣称零 Go 模块依赖；可依赖的部署判断是纯 Go 嵌入、无需 CGO，也无需独立运行器服务。[固定 go.mod](https://github.com/wazero/wazero/blob/v1.12.0/go.mod)、[固定 README](https://github.com/wazero/wazero/blob/v1.12.0/README.md)

固定 README 的平台信息如下；“可以交叉编译”不等于已经逐平台验收。

| 引擎 | 执行方式与官方测试矩阵 |
| --- | --- |
| Interpreter | 没有平台专用实现，可面向 Go 编译目标；测试 Linux amd64／arm64／riscv64，macOS arm64，以及 Windows、FreeBSD、NetBSD、OpenBSD、DragonFly BSD、illumos、Solaris 的 amd64 |
| Compiler | `CompileModule` 时产生机器码；测试 Linux amd64／arm64、macOS arm64，以及 Windows、FreeBSD、NetBSD、DragonFly BSD、illumos、Solaris 的 amd64 |

以上是该版本的[官方平台政策](https://github.com/wazero/wazero/blob/v1.12.0/README.md#platform)。默认配置在环境支持时选 Compiler，否则选 Interpreter；固定发布还修正了不可执行 mmap 环境的选择行为。[配置入口](https://github.com/wazero/wazero/blob/v1.12.0/config.go#L173)、[发布修正](https://github.com/wazero/wazero/releases/tag/v1.12.0)

**建议：** 首个参考 profile 显式使用 Interpreter，先在 Linux amd64 验证功能与边界。其性能需要测量；后续启用 Compiler 必须对目标 OS／CPU 和签名环境重复验收，而不是以自动选择证明所有平台相同。

### 3.2 ABI、默认暴露与文件陷阱

**事实：** 自带 `imports/wasi_snapshot_preview1` 实现，以 `wasi_snapshot_preview1` 为导入模块名；宿主需先实例化这些导入，缺失导入会使 guest 实例化失败。[固定 WASI 实现](https://github.com/wazero/wazero/blob/v1.12.0/imports/wasi_snapshot_preview1/wasi.go) 本报告因此只推荐 **Core Wasm + 明确的自定义 ABI，按需兼容 WASI Preview 1**；没有把 WASI Preview 2、Component Model 或任意语言的完整操作系统环境纳入兼容承诺。

**事实：** `ModuleConfig` 默认环境为空、文件访问未启用、stdin 为 EOF、stdout／stderr 丢弃；时钟和随机源默认是模拟／确定性的。启用真实时间、随机源或输出流是宿主的显式配置。[默认配置契约](https://github.com/wazero/wazero/blob/v1.12.0/config.go#L470)

**事实：** 实验性 `sock.Config` 可由宿主显式设置 TCP listener，实例化时变成预打开描述符；不是默认环境继承。[固定 socket 配置](https://github.com/wazero/wazero/blob/v1.12.0/experimental/sock/sock.go) **推论：** 最小 profile 不安装 socket 配置或网络宿主函数，可避免主动授予 guest 网络能力；不能据此声称任何扩展配置下均不可联网，因为宿主回调可以代为发请求。

**关键事实：** `WithDirMount` 官方明确允许通过 `../../` 越出目录；`WithReadOnlyDirMount` 也允许越界，只限制写操作。`WithFSMount` 同样提醒 `fs.FS`／`os.DirFS`／`fs.Sub` 不能被笼统视作 jailed 文件系统。[固定 FSConfig 的 Isolation 注释](https://github.com/wazero/wazero/blob/v1.12.0/fsconfig.go#L63)

**建议：** 默认零宿主目录挂载。需要资料时，以有限字节输入、内存中的固定文件集合，或授权后返回内容的宿主函数传递；任何可变宿主文件系统实现都需单独验证路径穿越、符号链接和并发替换。清单中的 `read:/workspace` 不能直接翻译成 `WithReadOnlyDirMount` 后宣称目录隔离已经成立。

### 3.3 内存、取消与宿主函数

**事实：** `WithMemoryLimitPages` 限制每个 Wasm memory；每页 64 KiB，默认上限 65,536 页，即 4 GiB。`WithCloseOnContextDone(true)` 默认未开启；开启后，调用 context 取消／到期或显式关闭模块会中断 guest 执行，并关闭该实例。[RuntimeConfig](https://github.com/wazero/wazero/blob/v1.12.0/config.go#L55) 官方给出无限循环的超时、取消和模块关闭示例。[取消示例](https://github.com/wazero/wazero/blob/v1.12.0/context_done_example_test.go)

**推论：** 线性内存限制不等于宿主进程 RSS 上限：解码／编译、引擎元数据、Go 堆、日志缓冲、并发实例和宿主函数分配仍需额外预算；context 超时不是确定的指令计量或 CPU 配额。不能把这两项 API 合起来写成“任意恶意插件不会耗尽宿主”。

**事实：** 宿主函数是 Go 函数，可接收 `context.Context` 和 guest 模块；其他参数／返回值使用 Wasm 数值类型，复杂数据经 guest 内存与偏移传递。[HostFunctionBuilder](https://github.com/wazero/wazero/blob/v1.12.0/builder.go#L80) Go context 是取消信号契约；wazero 的设计文档也明确讨论了阻塞系统调用不能直接响应 context 的问题。[Go context](https://pkg.go.dev/context#Context)、[wazero 阻塞 I/O 说明](https://github.com/wazero/wazero/blob/v1.12.0/RATIONALE.md#impact-of-blocking)

**推论／要求：** 引擎对 guest 指令的中断不代表能强杀任意阻塞 Go 回调。宿主函数必须检查参数长度和内存访问结果、限制自身分配与输出、检查当前任务授权，并把 context／deadline 传播给下游；不能导出通用 `exec`、任意路径 `open` 或无约束网络代理后继续宣称原来的隔离边界。可能永久阻塞的回调不进入最小 profile。

**事实：** 模块可 `CloseWithExitCode` 释放资源并使名称可复用；`api.Function.Call` 不允许前一次尚未返回时重复并发调用同一 Function。[Module 与 Function](https://github.com/wazero/wazero/blob/v1.12.0/api/wasm.go) **建议：** 每次调用新建 guest 实例，避免状态串扰；缓存仅保存经验证的编译结果。取消后丢弃实例，升级时让旧调用结束／取消，再路由到新版本。模块关闭不会自动撤销已经完成的外部副作用，也不等于扩展激活事务或任务持久化恢复。

## 4. 可供后续实现的参考 profile

以下是建议值，用于形成具体测试对象，**不是已经验证的运行保证**。

1. 单个 Go 宿主嵌入上述固定 wazero 版本；受信内置实现走普通 Go 接口。第一批 Wasm guest 限定为符合 `harness_v1` 自定义导入／导出契约的模块，WASI Preview 1 仅按实际工具链需要提供。
2. 加载前校验内容摘要、清单、ABI、导入白名单、二进制体积（起始建议 8 MiB）；校验必须覆盖实例化之前的阶段，因为 Wasm start 段可执行代码。未知导入或不兼容特性拒绝激活。`WithStartFunctions()` 只清空配置的导出启动函数，不应被当成跳过 Core start 段的安全开关。
3. 显式 Interpreter、每 memory 1,024 页（64 MiB）、`WithCloseOnContextDone(true)`；实例化与调用使用有截止时间的 context（普通小工具起始建议 2 秒），每节点同时最多两个 guest。上线前另测大输入与编译资源占用，不能依赖调用超时为编译阶段提供硬上限。
4. 默认不传宿主环境、目录、socket 或凭证；输入／结果各最多 1 MiB，日志最多 64 KiB。宿主回调以任务作用域的句柄访问授权对象，避免给 guest 原始凭证与通用操作系统入口。
5. 外部副作用仍经过 Harness 内核的授权、幂等与结果核对。超时后将结果未知的动作保留为待核对，不能重建 Wasm 实例后直接重放。
6. 本机脚本执行能力单独声明 `trusted-native` 或具体 sandbox backend；未配置后端时仍能运行内置与上述 Wasm profile，但拒绝要求隔离的任意原生脚本。

第 2 点 start 行为的依据为[Runtime.InstantiateModule](https://github.com/wazero/wazero/blob/v1.12.0/runtime.go#L105)和[ModuleConfig.WithStartFunctions](https://github.com/wazero/wazero/blob/v1.12.0/config.go#L513)。其他额度是本报告提出的验证起点，应由工作负载证据调整。

## 5. 必须补做的验收与尚未解决的风险

本次仅做资料和源码核对，下表均为**待实现验收**。

| 场景 | 通过条件 |
| --- | --- |
| 恶意／不兼容模块、未知导入、超大二进制、start 段无限循环 | 拒绝或按 deadline 终止；激活指针不指向失败版本 |
| 超过 64 MiB 的初始内存及循环 `memory.grow` | 按 API 失败语义拒绝／增长失败，宿主继续处理其他任务；记录实际 RSS |
| 导出函数无限循环；取消；显式 Close | 调用终止且实例不可复用；下一次调用的新实例正常 |
| 忽略 context 的受控阻塞宿主函数 | 在测试进程外设 watchdog，证明并记录其不能由 guest 取消机制强制终止；这种回调不得进入最小 profile |
| 读取宿主环境、打开文件、访问网络、跨实例访问数据 | 默认 profile 无授予；导入与宿主函数授权都能拒绝访问 |
| 目录挂载回归试验 | 在临时无敏感数据夹具中观察 `../`／符号链接行为；不得误把只读挂载报告为 confinement |
| 输出洪泛、超大 hostcall 长度、并发编译与实例反复创建／关闭 | 配额可执行、无无界缓冲，测量内存峰值与资源回收；宿主仍可响应 |
| 原生脚本产生子孙进程、保留 stdout、忽略普通终止信号 | 验证所选后端的进程树回收和 I/O 收束；普通 `CommandContext` 不自动获得 sandbox 标签 |
| 升级、取消与已提交外部操作并发发生 | 路由切换不混版本；结果未知进入核对流程，不自动重复副作用 |
| Linux 首发以及后续 Windows／macOS、Compiler 模式 | 对各实际支持组合重复功能与边界用例，记录 OS／CPU／Go／引擎版本 |

**剩余边界：** 嵌入式引擎、宿主函数及宿主 Go 进程属于可信计算基础；单个进程的崩溃／内存耗尽仍可能影响整个运行节点。未核验该候选的全部已知漏洞，也未证明对所有恶意模块的拒绝服务防护。若需求升级为抵抗恶意本机代码、严格进程级资源上限或更强故障隔离，应显式选择经过验证的操作系统沙箱／独立执行后端；本研究不把那种保证附加到“内嵌 Wasm”这一名称上。
