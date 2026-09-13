# 08：在预算内生成并发布答案

本票建立 API 模型驱动的简单问答链路：SDK 接纳任务，Core 持久预留，Brain 组装受控输入并请求 Model，完整校验后保存受控产物，Core 正式准入，SDK 查询当前可读正文。模型输出不执行工具、修改权限或写长期记忆。

## 模块与组装

- `brain.Model` 声明文本、结构化输出、流式、上下文容量、可执行输入上界和受信处理位置；`Generate` 一次调用对应至多一次供应商请求。参考 Ark Adapter 不支持流式，不启用模型工具和隐式回退。
- `brain.AnswerBrain` 实现现有 `tasks.Brain`，固定完整 Schema 为 `{"answer":"...","sources":["content:..."]}`；拒绝重复键、未知字段、空答案、null 来源、非获准引用和非正常结束状态。Schema 校验只证明结构和引用范围，不证明任意答案正确。
- `tasks.GenerationPort` 在现有 RunStore 中保存预留、请求序号、用量和原发布请求。供应商类型不进入 Core；SDK/Protobuf 增加可选生成预算与公开用量汇总。
- `answers.ContentAccess` 通过 07 的内容接口组装必需事实，复核精确修订及处理/披露许可。`answers.Port` 将持久输出操作与结果引用绑定，再执行 Core 的版本、所有者、Worker 代次、租约和控制检查。
- `answers.Query` 先走 Task SDK 的当前读取授权，再查询内容状态和正文。任务完成、正文当前可用、UI 交付分别表达；本票 `Delivered=false`，没有用户阅读确认。

参考组装可读 [profile harness](../../profiles/answer/harness.go)。生产组装提供自己的 Model、来源政策、受信身份、内容服务和时钟，按接口依赖；参考库不要求独立数据库或模型服务作为默认本地验证依赖。端云传输及完整个性化检索留给后续票。

## 预算与恢复规则

任务预算 `ModelRequests/ModelTokens` 必须同时为零或同时非零。零代表旧脚本路径，不能用来执行新模型路径。任务最多 64 个请求、1,048,576 token；单轮预留 1–3 次请求，每次输入/输出上界均正数，合计不超过该硬界。实际参考配置更小，见下表。

预留先原子提交，并递增任务版本；Brain 使用提交后的版本。请求在事务外执行，但每个序号先经 `BeginRequest` 单次准入；重放同序号不能再次请求。持久事件记录各请求序号，不创建 token 或片段级身份。每轮格式修复在同一预留中重试，最多两次修复，不隐式追加额度。

`已用 + 未结清占用 + 可用 = 任务上限`。已核实消耗幂等结算，未启动份额只在该轮已经停止后释放。未知请求保留每请求完整输入/输出上界；超时、取消、进程退出、供应商无用量均不视为零。迟到核实只减少原预留的未知部分、增加原轮实际消耗，不更新决策版本或重开发布资格。可信 Worker 的结算端口不依赖仍有效的业务执行权限；它不能借此发起新请求或披露答案。

预留/请求启动提交未知时不调用模型。重开 RunStore 可查原工作代次与预留，不能根据“尚未收到响应”重置预算。崩溃后尚未开始的预留默认仍占用，参考恢复不推断退款。未能恢复的生成结果只能在任务重新取得合法资格且有剩余预算后再生成；本票不提供自动供应商用量查询或后台补偿调度器。

完整答案保存沿用预留中持久化的内容操作身份。保存回包未知先 LOOKUP 原操作；正文与 Core 发布不构成跨存储原子事务。`PreparePublication` 持久化精确 Core 请求，`Port.Recover` 先核对原提交/原受控产物，在当前资格仍满足时完成发布；不会调用模型。已过期任务资格不能被恢复接口放宽，未采用产物按 07 的留存及清理规则处理。查询已删除、缺失、损坏或到期正文不会从历史回执复现答案。

当前来源政策为受信本地配置。发送前、返回后及发布前分别检查；不同政策权威与 Core CAS 之间仅保证这些有限新鲜度检查，不声称跨权威原子撤销。实际处理位置来自受信 Adapter 配置，任务和模型文本不能改变它。目标/约束本身须按来源政策获准处理及披露；任务目标沿用现有持久任务契约，因此不能把禁止保存在本地的原文直接作为 Task.Goal。

## 参考模型与边界

根据用户指定的火山官方[模型列表](https://www.volcengine.com/docs/82379/1330310)选用 `doubao-seed-2-0-lite-260428`，固定 Chat Completions 端点 `https://ark.cn-beijing.volces.com/api/v3/chat/completions`。能力标签为 `ark-cn-beijing`，表示所绑定 Ark 区域服务，不另行保证供应商内部物理机位置。

官方列表标明上下文 256K、最大输入 224K，并支持结构化输出；按 K=1024 换算，保守预留整个 229,376 输入 token 上界，涵盖系统提示、Schema 与协议包装，不以字符估算声称精确 tokenizer。根据官方[Chat API 说明](https://www.volcengine.com/docs/82379/1494384)，参考请求关闭深度思考，以 `max_completion_tokens=512` 限制完整输出，并使用 strict JSON Schema。供应商返回实际用量后结算；异常用量拒绝结果、保留未知占用。上界依赖固定供应商契约，无法给出这一约束的替换 Adapter 必须声明不支持硬限。

| 配置 | 脚本契约 | 真实模型验收 |
| --- | --- | --- |
| 每轮请求 / 修复 | 1–2 / 0–1 | 1 / 0 |
| 每请求输入 / 输出 token | 600 / 100 | 229,376 / 512 |
| 必需输入 / 完整答案大小 | 32 KiB / 8 KiB | 同左 |
| 模型 / Brain 期限 | 受控同步返回 / 45 秒 | 40 秒 / 45 秒 |
| I/O / 结算期限（原验收配置） | 1 秒 | 同左 |
| Worker 租约 / 续租 | 10 秒 / 1 秒 | 同左 |
| 并发 / 最多工作代次 | 1 / 3 | 同左 |
| 默认模型控制轮询 | 100 ms；任务配置优先 | 同左 |
| 产物留存 | 5 分钟且不晚于来源 | 同左 |

固定端点不跟随重定向，模型名需匹配已验证配置与响应；不自动重试 429/5xx，不返回供应商原始错误体。`ARK_API_KEY` 由环境或本地凭证文件注入，不进入模型输入、Task、协议正文或报告。公开材料仅包含 Memory、Brain、Execution 三个系统名称；密钥和内部思维链未保存为证据。

本票的保守输入预留可能大于后续专项评测的预算，不能据此宣布那些 profile 达标；后续需要经验证的更紧 token 上界或另一 Adapter。这里不声明千级 API 调用或 90%/95% 专项质量门槛通过。

## 可重复验证

```sh
go test ./tasks ./profiles/answer ./adapters/arkmodel
go build -o build/contractcheck ./cmd/contractcheck
./build/contractcheck -profile bounded-answer-v1
make verify
```

脚本 profile 包含 19 项本地契约/竞态与 4 项真实 SQLite 子进程恢复。覆盖私密来源零请求、来源收紧、超大必需输入、结构修复/截断/伪造引用、未知/异常用量、能力不足、控制竞态、预算不足、产物保存未知和 Core 回包丢失。子进程在预留后、请求开始后未知、完整产物保存后未准入、正式发布后丢回包四个边界退出 73，然后重开原状态核对。

真实模型入口单独运行，**不纳入 `make verify`**，每运行一次最多发出一次付费请求：

```sh
go run ./cmd/answercheck -env-file .env > build/real-answer-report.json
# 或只注入 ARK_API_KEY 环境变量：go run ./cmd/answercheck
```

凭证文件仅解析 `ARK_API_KEY=...`，不会作为 shell 执行；报告不包含密钥。缺配置返回 `not_run`，已执行但不符合契约返回 `failed`，退出码非零；脚本通过不覆盖真实失败。每次新建私有 SQLite 证据目录，输出报告记录公开材料、模型能力、预算、实际请求数/用量、停止状态、任务结果引用和核对答案。

原格式 1 gob 采用加字段兼容，旧预算/账本解码为零/空；Worker 的旧版本二进制夹具增加这些默认值断言，并继续跑原脚本、重开与原回执核对。01–07 的控制、授权与内容回归仍是必需验证。

## 完成与验证记录（2026-09-10）

实现提交 `8b47dbe`，审查修复 `56e6fdb`、`0eaae95`。最终代码修订为 `0eaae950f1cfdad4a27a852e0616135dd9f73806`；最终脚本及真实报告的构建均为 dirty=false。Go 1.26.1、linux/amd64；完整依赖版本与摘要见报告 Build。

- [有界问答报告](evidence/08-bounded-answer-report.json)：23 项必需检查通过，19 项契约/竞态及 4 项真实 SQLite 子进程恢复。
- [最终阶段报告](evidence/08-verification-stages.json)：依赖校验、生成一致性、编译、go vet、全量 race 测试、SDK 样例及全部 profile 均通过。
- 01–07 回归：[SDK 32](evidence/08-sdk-regression-report.json)、[授权 30](evidence/08-auth-regression-report.json)、[任务 13](evidence/08-tasks-regression-report.json)、[Worker 26](evidence/08-worker-regression-report.json)、[控制 31](evidence/08-control-regression-report.json)、[签名授权 30](evidence/08-grants-regression-report.json)、[内容 36](evidence/08-content-regression-report.json)。与 08 合计 221 项必需检查。
- [真实模型最终报告](evidence/08-real-answer-report.json)：一次请求，输入 398、输出 85，共 483 token；`stop` 正常结束，Task=COMPLETED，未知占用为零，受控正文当前可读，引用及系统名称匹配公开事实，Delivered=false。
- [双轴审查](08-bounded-answer-review.md)：Standards 1 项启发式、Spec 2 项，均修复并独立复核关闭。

本次实际模型总调用数为 3，均使用相同公开事实、每次最多 512 输出 token：首轮 399+84，严格用量/Schema 修复后 402+83，最终版本 398+85；合计输入 1,199、输出 252，总计 1,451 token。没有超出用户授权的三次上限，也没有发送私密数据或密钥正文。

修复前后各次完整验证均通过；审查发现属于原测试未覆盖的边界，新增测试先确认失败，再修复并最终重跑完整验证。结果编码后可能因 JSON 转义而膨胀，现已在保存前再按共享上限检查；生成配置也不能放宽查询/恢复的共同大小包络。

最终报告对应上述代码修订，后续完成记录提交仅更新文档与归档报告，不再调用模型。

### 后续参考宿主的组合 I/O 配置

22 票全仓回归定位到：个性化答案生成已成功，但发布的当前来源校验与 Core 提交合计超过原 1 秒外层期限。当前 answer 参考宿主的 RunLimits.IOTimeout 调为 5 秒，作用于 Runner 各端口调用及续租调用上界；底层单次 I/O 和模型结算期限仍保持原值。租约 10 秒、续租周期 1 秒及模型/任务预算不变。旧验收记录和冻结 RunLimits 不改写，旧 1 秒任务检查点不能假定可按新配置继续执行。诊断和新回归见 `22-acceptance-audit.md`。
