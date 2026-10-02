# 方法实现共同合同

开发者应能从一次请求推导出唯一的本地决定、持久产物和恢复入口。本章固定所有方法共用的字段、错误及裁决顺序；模块内的接口表补齐本方法差异。它是未发布的设计合同，不代表 SDK 或服务已经实现。

CAS表示“比较原修订后更新”：原修订不匹配就拒绝，不自动改成最新值。authority表示有权签发该类授权或证据资格的受信负责方；拥有某个普通对象，不等于拥有签发资格。

## 1 怎样读模块接口表

输入使用 `字段:类型`；`?` 表示可选，未标记的字段必须提供。除表列字段和公共外壳外，拒绝未知字段。集合须遵守发现公布的数量/字节上限。ObjectRef、ContentRef 等使用[核心字典](../data/field-reference.md)，完整逻辑记录使用[模块字段](../data/module-records.md)。正文 Ref 指向准确、可取得且通过声明 Schema 的内容，不是任意 JSON 逃逸字段。

- 写方法使用 Command：固定 protocol/profile、logical_service_id、command_id、method、target_id、expires_at、payload；标 `CAS` 的方法还必须带 expected_revision
- 创建的 target_id 是实际创建 owner；对象修改的 target_id 是原对象 ID。payload 中的 owner/对象必须一致；不能通过指定新 owner 转移已有责任
- 原 command_id 在一个逻辑服务和租户内只裁决一次。幂等命中返回原决定；同键异内容返回 idempotency_conflict。新权限、当前时间或默认值变化不改原决定
- 标 `源版本` 的归并方法不用对象 CAS，按原来源 owner/id/revision/digest 幂等。它们不能修改原目标或绕过当前启动门禁
- 查询使用 Query：query_id、method、target_id、payload；无 expected_revision，无永久命令墓碑。列表输入统一为 limit:Count、cursor?:string 和表列固定过滤器。limit 为1..100，超过声明上限拒绝，不静默截断
- 字段表中的 `read` 返回当前授权可见的完整记录视图及准确 ObjectRef；指定 revision 时读取准确历史版本。不存在、正文已清理、远端不可核验分别返回 not_found、gone、dependency_unavailable，不能互换
- 列表统一返回 items、collection_revision、next_cursor?、exhausted、partial、gaps。空页可有 next_cursor；exhausted 不清除 partial。与目标完成有关的内部全集不依赖这个响应

## 2 回执与异步准备

Receipt 的公共字段为 command_id、request_digest、stage、accepted_at?、decided_at?、output?、error?。accepted 必须有 accepted_at 和下次查询的原 object_ref/prepare_ref；applied/rejected 必须有 decided_at，分别携 output/error。回执清理或当前不得披露时，查询返回带原身份的 gone/redacted 视图，不改历史决定。

模块表标 `A` 表示 applied 是本地业务决定已提交，后台目标工作或核对可继续。标 `P→A/R` 表示允许先 accepted：先耐久保存唯一准备记录与 Job；后台完成后将同一原命令推进为 applied 或 rejected。最终只出现一个 A 或 R。不能在 accepted 后丢掉准备责任，也不能用第二个创建命令代替查询原准备。

持久准备的 phase、失败原因和未结责任通过本方法 read/原回执查询。接口超时只结束等待；领域截止、原首次接纳截止和准备完成期限分别保存。P→A/R方法必须显式传prepare_deadline:Time；owner在首次接纳时核验它不晚于相关Task/批准/许可的最紧截止，并固定在原准备记录。到期先封新准备；可证明未产生后续责任时固定rejected/prepare_expired，有未知外部交接时仍accepted并只核对/清理，不能伪报已撤销。原命令被及时 accepted 后，expires_at 不撤销其责任；之后仍受领域 deadline、当前授权和能力期限约束。

## 3 固定裁决顺序

1. 验证传输身份和当前数据域；严格解析原始 JSON、协议/profile 与外壳。未能安全识别请求时返回传输/结构错误，不产生业务动作
2. 核对逻辑路由与当前请求/披露资格。进入原命令作用域后先查既有决定；当前不能披露时不返回原敏感 output，但也不重新执行
3. 对首次请求检查闭合 payload、必需引用和幂等业务键。有效外壳的确定性业务拒绝与原摘要持久保存；失败重投不自动变成功
4. 锁定原对象，先比较原 CAS/源版本，再检查合法前态、当前权限、期限、关系完整性和预算。所有方法采用同一优先级，避免相同竞争返回随机语义
5. 在一个已声明的本地事务保存业务变化或准备责任、原回执及必要 Job。CommitUnknown 先查原键，不出站、不释放未知额度
6. 事务外进行跨 owner 交接。下一事务归并可信原事实；不把网络代码放入可自动重试的数据库闭包

无变化控制命令仍返回 applied，但不递增业务/控制修订或产生无用 Job。已经处于另一个终态时不能假装执行了本次控制。具体同态/终态处理在各模块状态表中固定。

## 4 统一错误与恢复

Error 固定 `code、scope、reason、retry、detail?`；detail 不带秘密或无权对象。reason 是本 profile 的稳定枚举值；用户文案可以本地化。下表为所有方法继承的公共错误；模块另列本方法 reason。业务拒绝使用同一套 code，不随 adapter 改成自由文本。

| code | 触发条件 | retry 与客户端行为 |
| --- | --- | --- |
| invalid_request | 字段/范围/关系或闭合 Schema 不合法 | none；修正业务请求时使用新命令，不自动改旧载荷 |
| forbidden | 当前主体、来源、用途、目标或审批不足 | after_change；先取得所需资格，再提交新的有界请求 |
| expired | 首次接纳、请求或必要凭据已过准确截止 | none；不能延长原命令/票据以重试 |
| revision_conflict | expected_revision 与原权威当前头不同 | after_change；重读并由上层决定新请求，不自动刷新CAS |
| idempotency_conflict | 同原命令/业务键对应不同内容或主体 | none；停止自动重试并报告冲突 |
| invalid_state | 对象当前前态不得该方法 | after_change或none；由模块状态表确定，终态不能等待复活 |
| unsupported | 方法、spec、状态恢复能力或平台合同未声明支持 | none；不静默近似、降级或改目标 |
| overloaded | 尚未接纳且容量不足 | backoff；按同请求/原期限重投，不能据此判断此前未知调用未接纳 |
| dependency_unavailable | 必需来源/权威/平台当前不可核验 | backoff；原已接纳责任继续查询原身份，不返回not_found |
| effect_unknown / accounting_unknown | 原实际效果/费用仍未知 | query_original；保持预留、资源或关闭责任 |
| not_found / gone | 原owner确定从未存在 / 原身份存在但正文已回收 | none；gone不得复用身份 |
| cursor_expired / snapshot_required | 固定游标过期、权限变化、来源目录变化或变化流有缺口 | after_change；重开查询/快照，不拼接新旧集合 |

overloaded、dependency_unavailable 在接纳前可作为未决定的调用错误；不得伪造已持久 rejected。接纳后遇同类故障，更新原准备/等待并返回原 accepted 状态。只有可证明的领域拒绝才进入原 rejected。错误重试建议从不授予第二次副作用。

## 5 规范与可发布范围

模块接口表冻结业务选择、输入输出、前态和成功点。核心 JSON Schema 当前仍是形式化子集，不能因为新增文本方法表就对外宣称完整 profile。实现阶段须将拟开放方法逐个转为同版闭合 Schema、方法登记和 SDK，并运行正反例；字段名称和业务规则已由本合同决定，不能在生成代码时重新选择另一种语义。

一个部署可只开放首个实施切片。发现必须逐项列方法、Schema 摘要和可选能力；未开放的方法返回 unsupported。可选能力不开启时，不分配其对象、预算或后台线程，也不影响基本任务闭环。发布的协议版本及旧解码器保留规则见[共同协议](README.md)。
