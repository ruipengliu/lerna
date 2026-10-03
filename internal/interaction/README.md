# 会话、输入与定时交付

`New(Config, Ports)` 只校验宿主配置；`Register` 按实际端口登记方法与 Job，不创建业务对象或发送请求。Session/Submission、Surface/Presentation、Schedule/Occurrence 分别保存原输入、呈现和交付，不裁决 Task 成功、Grant 消费或外部效果。

| 已实现入口 | 负责的事实 |
| --- | --- |
| session.create/read/list/archive/reopen/delete、branch.create/select、branches/messages.list | 固定 owner、主体、分支引用与历史截止；分支不复制 Task 或预算，归档/删除不取消 Task |
| session.submit_goal/steer/enqueue_goal_after、submission.read/list/withdraw/history、session.reply | 原 Submission 与新目标 FIFO、独立的同 Task steer 顺序；sending 后只记录撤回请求，沿原命令核对 |
| interaction.input | 保存准确 owner 请求、答案/预览和原转交命令；回答 Schema 与一次消费由实际 owner 决定 |
| surface.create/update/read/close、presentation.open/switch/begin/read/rendered/close、application_event/read | 登记绑定、准确快照、generation/intent/主体；事件只能进入配置的闭合 Schema 与固定 Command |
| schedule.create/update/read/list/pause/resume/delete、occurrence.read/list、schedule.skips.list | 原规则、计划时点、永久单槽、固定派发期限和真实 Task closure 依据 |

宿主显式声明同库 `Participants`。Content 的 `CheckTx` 只核当前同库门禁，`Read` 在 Tx 外读取准确正文。Delivery 的 Send/Lookup、Task Closure 以及对象介质访问均在 Tx 外。原投递从 confirmed committed 的意图出站；未知提交或回复丢失保留原 owner、CommandID、期限及责任，先查原回执。关闭内容、请求过期/终止或结构化答案不符时，queued 输入固定 rejected，不发送。

多请求 Surface 需要可选 `RequestBatchPort.CheckBatchTx(ctx, tx, auth, []api.ObjectRef) ([]RequestView, error)`，上限 20、无重复且按原输入顺序返回。Task 桥必须调用真正 owner 的 `RequestViewsTx`，一次确定全部祖先 Task 与请求锁。单请求兼容 `RequestPort.CheckTx`；缺少批端口时多请求返回 unsupported。适配器须提供 `runtime.TxSnapshotReader`：Peek 只确定路由，随后先核 Task/请求与 Content 门禁，再锁 Surface、Presentation 并核原 revision；路由变化回滚，不能沿旧快照追加新上游锁。

Renderer 支持受限闭合对象、有限 oneOf、字符串/数值/布尔及显式选项，不执行脚本或远端 Schema。正文读取前后均核当前身份、请求、来源与 retention，准确 hash/length 不符、必需正文超预算或下载失败时不返回可用呈现。not_modified 也重新核门禁；rendered 必须提交完整必需引用与成功呈现标记。当前 generation、intent、Surface revision 或凭据代次变化后，旧回调被拒绝。此元数据不证明业务消费。

`NewTZDB` 接受宿主锁定版本的有限 TZif；`OpenTZDB` 显式验证本地版本并载入指定区域。有限规则为 once_at、interval、daily、weekly、monthly；UTC interval 不漂移，夏令时 gap 跳过、fold 取较早时刻，不存在的月日跳过。Occurrence 在 planned_at 冻结规则版本、模板与 `deadline=planned_at+timeout`、`accept_before=min(deadline, planned_at+60s)`，更新、暂停、重开和重试均不延长。unknown delivery 占用原槽；释放必须比较 occurrence_id 并持有原 Task 的 goal_work_closed 与 effects_closed 及准确 closure 证明。纯迟到账务不阻塞。停机扫描每批至多 100 项或 10ms，保存原游标与 skipped range 后由 Job 续页。

集合分页上限 100，明确 exhausted/partial/gaps。游标绑定 scope、主体、凭据代次、角色、集合版本和首次截止；Dispatcher 提供 QueryBinding 时首屏使用该原截止。续页保留解析出的原游标截止，并与当前 QueryBinding 截止取更早值，后续查询不能延长期限。跨 Orchestrator 全局来源聚合需要宿主另行声明完整来源目录，本包的本方索引不能证明全局 coverage_complete。

验证入口为 `go test -race ./internal/interaction`。设置 `HARNESS_INTERACTION_STORE=postgres` 与 `HARNESS_TEST_POSTGRES_DSN` 后，同一公开命令与 typed use-case 套件使用真实 PG；默认使用 WAL/FULL/FK 的持久 SQLite。合同覆盖 DB 重开、回复丢失、未知提交、FIFO/steer、固定期限与 overlap、真实签名 closure、当前请求/正文拒绝及两个实际 PG 反序锁场景。日历正反例使用固定计划时点；会话行为使用固定受信时间端口，未扩大业务 TTL。实际浏览器与 WSS 交接由 Web/宿主集成套件另行取证；外部平台账户、跨 AZ 容量和生产恢复目标尚需部署验证。
