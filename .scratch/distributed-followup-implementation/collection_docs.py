from pathlib import Path
base=Path('docs/architecture')
p=base/'contracts/protocol.md';s=p.read_text();s=s.replace('原有 40 个严格方法、本轮补齐的 53 个保留方法，以及为预算关闭、输入请求读取、在线费用结算与可信确认补充的 8 个交接方法，共构成 101 个领域方法','当前 104 个领域方法包括预算关闭、输入读取、费用结算、可信确认，以及 Operation／Activation／Grant 集合恢复所需的三个枚举查询');s=s.replace('全部 101 个方法','全部 104 个方法').replace('| 14 | 查询与恢复重点','| 14 | 查询与恢复重点')
for module,old,new in [('execution',14,15),('extensions',5,6),('security',12,13)]:s=s.replace(f'| [{module}](../{module}/README.md) | {old} |',f'| [{module}](../{module}/README.md) | {new} |')
anchor='## 3. '
pos=s.index(anchor)
section='''<a id="collection-snapshots"></a>
### 按类型订阅的集合恢复

订阅的范围是当前主体在原逻辑服务上、所选类型下获准披露的完整集合，不要求客户端预先知道对象 ID。服务只声明自身负责且同时实现下表枚举与准确读取的方法；不支持的类型在 subscribe 时返回 unsupported。一个 owner 的完整枚举不表示跨 owner 全局完整，客户端对每个已登记并订阅的负责端分别保存水位和缺口。

| object_type | 集合查询及无筛选输入 | 单对象当前查询 | 集合的披露边界 |
| --- | --- | --- | --- |
| task | task.list：省略 statuses，沿 next_cursor 到末页 | task.read | 原 Home 当前获准任务，含终态；gaps 非空不能标为完整 |
| operation | execution.list：query_id、limit、cursor? | execution.get | 原 Executor 当前获准 Operation，含已关闭及效果未知记录 |
| memory | memory.list：types=[]、states=[]，沿 next_cursor | memory.inspect | 当前获准管理控制元数据，含 disabled／deleted；不授予正文读取资格 |
| surface | interaction.surface_list：app_ids=[]、task_refs=[]、include_expired=true | interaction.surface_read | 当前获准 Surface，含过期项；partial、gaps 或 unreachable_endpoints 均保留缺口 |
| activation | extensions.list：query_id、limit、cursor? | extensions.read(kind=activation) | 原安装 owner 当前获准 Activation，含 blocked／disabled；不枚举 InstallLock |
| grant | grant.list：query_id、limit、cursor? | grant.read | 原 Grant owner 当前获准 GrantRecord，含 revoked／按时间已到期；不授予许可使用资格 |

客户端先取得 Subscribed.cursor 并开始缓冲后续提示，再发起上表集合查询。领域页游标、snapshot_at 或 task.list.upper_bound 都不是订阅提示水位；尤其 created_at 上界不代表事务提交切点。客户端先合并当前集合，再处理从订阅水位起的所有提示；提示中的未知 ID 也必须按表读取，不能只刷新已知对象。各 owner 只保证自己的查询与提示覆盖，不提供跨 owner 原子快照。分页期间有缺口、权限范围改变或提示缓冲溢出时，旧集合不能被提升为完整。

execution.list、extensions.list、grant.list 复用同一个分页形状，target_id 为准确 owner_id。输入为 `{query_id,limit,cursor?}`，limit 为 1–100；输出为 `{query_id,owner_id,snapshot_at,expires_at,items,next_cursor?,exhausted,partial,gaps}`，items 分别是完整 Operation、Activation、GrantRecord。获准枚举与相应单对象查询使用同一当前披露策略，不能因列表入口降低敏感字段权限。

这三个查询在首次接纳时，以 owner 本地一致读取固定当前获准成员 ID，按 ID 字典序保存有限集合；冻结的是成员，不是记录修订。后续页返回成员的当前记录，修订升高不使其自动缺失。服务以认证的 tenant、actor、业务 sender（存在时）、owner、method、query_id、原 limit 和当前权限范围绑定查询；代理网关身份不替代业务 sender。同 query_id 原样重读首部复用原集合与期限；条件或权限范围改变返回 query_conflict，调用方先废弃旧分页再以新 query_id 枚举。游标是不可伪造的不透明集合／位置引用，跨身份、owner、方法或查询移用也拒绝，正文中的 ID 不能选择认证范围。

每页至少扫描一个未遍历位置，或以 exhausted=true 结束；单页最多扫描 1000 个位置并最多返回 limit 项，空页可以带 next_cursor。next_cursor 缺席当且仅当 exhausted=true。每次发送前重新检查当前披露资格；成员已不可读或权威来源不可用时不输出其 ID，返回不含敏感标识的 gap。权限范围增减会使原集合失效，不能仅跳过撤权项后继续声称覆盖当前集合；新可见的历史对象也要求新集合。范围未变而记录更新时直接返回当前记录。partial 必须与非空 gaps 同时出现；exhausted 仅表示冻结集合遍历结束。

初始上限为每集合 10000 个 ID 或 1 MiB 成员元数据先到者、10 分钟不可续期、每 tenant／actor／业务 sender／owner 合计 4 个活动集合；查询槽与集合保存于 owner 的共享存储，副本切换不依赖原进程内存。容量截断返回 partial=true、gaps 包含 membership_limit，后续页保持该标志；到期或已清理集合返回 cursor_expired。每页有独立扫描／返回预算，同一主体不断更换 query_id 不重置累计预算。这里不保留跨请求数据库事务或长期 MVCC 快照；集合自身不是业务事实或新领域目录。内存、Surface 原有更小的集合上限继续生效。

客户端只有在所有目标类型／owner 的枚举均已到末页、没有 partial／gaps／不可达端且从起始水位至处理位置的提示连续时，才可标记“在该水位已完整恢复”，不能称为不再变化的全局快照。恢复每轮最多 100 页、10000 项、60 秒；任一上限先到即保留明确缺口。暂时失联、游标失效或权限改变最多自动重新开始 2 轮，带抖动退避并共用原恢复预算；仍不足则展示来源与类型级缺口，保留已知对象的受权查询，并等待新权限／容量事实、用户刷新或正常周期校准。固定容量截断不立即重跑相同全量查询，不以不断重订阅制造快照风暴。

Activation.revision 是可见持久投影的独立修订；phase、ready_instance、instance_readiness、new_use_disabled 或 residual_work 等任何可见变化都在同事务递增它并写变化责任。generation 仍只表示活动绑定代际。extensions.read、extensions.list 与 Change.revision 对应同一个 revision；QueryResult.resource_revision 若出现也必须一致。客户端对所有带修订投影按对象保留最高值，迟到旧记录不能覆盖较新事实，同修订不同内容视为协议冲突并重新核对；提示只推进“需查询”的最高水位，不能代替尚未取得的记录。旧查询到达且低于待查询水位时继续读取；权限失效或移除以当前查询结果及缺口处理，不由修订高低重新授予展示资格。

'''
s=s[:pos]+section+s[pos:];p.write_text(s)
p=base/'contracts/transport.md';s=p.read_text();s=s.replace('服务按当前权限过滤这些类型下可披露的对象，返回','类型的集合范围及对应枚举／单对象读取入口见[集合恢复](protocol.md#collection-snapshots)。服务按当前权限过滤这些类型下可披露的对象，返回')
s=s.replace('客户端读取领域完整快照，同时有界缓冲已收到的提示','客户端按[六类型映射](protocol.md#collection-snapshots)逐页枚举当前获准集合，同时有界缓冲已收到的提示')
s=s.replace('客户端缓冲溢出时断开并重新建立快照','客户端缓冲溢出时断开并按有界恢复预算重新建立快照')
s=s.replace('reason 分别为 cursor_expired／queue_overflow','reason 分别为 cursor_expired／queue_overflow')
old='权限撤回后，服务立即停止披露敏感对象标识，缓存提示和回复每次实际发送前复核资格。连接身份失效则关闭；仅对象权限减少时重新按当前范围查询并撤去不可继续展示的内容，不能凭历史订阅继续读旧快照。通知丢失可恢复查询，服务端推送的控制命令仍须经原 Delivery／Receipt 持久链路，不能降成可丢 Change。'
new='权限范围增减均发送 reason=authorization_changed 的 snapshot_required 并暂停旧订阅；帧只标订阅，不泄露被移除对象 ID。新增可见的历史对象即使没有业务修订变化，也必须通过新枚举发现。当前资格适配器在授权变更后使受影响订阅失效；跨 owner 变更通知不能证明连续时，订阅器至少每 30 秒通过当前资格适配器校准范围，无法证明范围未变则保守置缺口、停披露并按有界预算恢复。该周期只用于发现范围变化，每条消息和实际发送前仍须当前资格检查。连接身份失效则关闭；客户端收到权限缺口即废弃完整性标记与旧分页，撤去未经当前复核的缓存，再不带 cursor 重订阅。不同身份不能续用原 cursor。\n\n集合恢复明确保留 partial、gaps、不可达端及容量截断，不把 Surface 的 200 项冻结集合或 Memory 的有限集合当作无限目录；固定截断不触发同样全量查询的立即循环。领域页游标和订阅水位独立，任务创建时间上界也不等于提示切点；客户端需先订阅、后枚举，再读取水位之后的未知及已知对象提示。具体预算、完整条件和有限重试见[集合恢复](protocol.md#collection-snapshots)。通知丢失可恢复查询，服务端推送的控制命令仍须经原 Delivery／Receipt 持久链路，不能降成可丢 Change。'
assert old in s;s=s.replace(old,new);p.write_text(s)
p=base/'contracts/methods.md';s=p.read_text()
for method,prefix in [('execution.list','Execution'),('extensions.list','Extensions'),('grant.list','Grant')]:
 stem=method.split('.')[0];read='get' if stem=='execution' else 'read';needle=f'| `{stem}.{read}`';start=s.index(needle);end=s.index('\n',start)
 s=s[:end+1]+f'| `{method}` | query | {prefix}ListInput → {prefix}ListOutput | QueryResult／Error |\n'+s[end+1:]
p.write_text(s)
# Link complete rules once per owning module; keep each module's projection/authority explicit.
for module,method,record,read in [('execution','execution.list','Operation','execution.get'),('extensions','extensions.list','Activation','extensions.read(kind=activation)'),('security','grant.list','GrantRecord','grant.read')]:
 p=base/module/'README.md';s=p.read_text();needle='| '+('`grant.revoke` / `grant.read`' if module=='security' else 'extensions.read' if module=='extensions' else '`execution.get`')
 # Existing tables vary in code formatting; insert adjacent to method's table row.
 lines=s.splitlines();at=next(i for i,line in enumerate(lines) if line.startswith('|') and ('grant.revoke' if module=='security' else 'extensions.read' if module=='extensions' else 'execution.get') in line)
 lines.insert(at+1,f'| `{method}` | owner_id、query_id、limit、cursor? → 当前获准 {record} 集合页 | 按 {read} 的当前披露资格冻结有限成员；含终态，partial／gaps 不表示完整；[分页与订阅恢复](../contracts/protocol.md#collection-snapshots) |')
 s='\n'.join(lines)+'\n';p.write_text(s)
 p=base/module/'implementation.md';s=p.read_text();marker='\n## ';pos=s.index(marker,s.index(marker)+1)
 extra=f'\n{method} 由现有查询入口读取本 owner 的 {record} 仓储，复用 {read} 的当前披露策略。共享存储中的有限 collection_queries 保存认证范围、query_id、原参数、按 ID 排序的成员、期限与位置；页读取不固定旧记录修订，也不跨请求持有事务。查询槽、扫描上限、权限变化失效、partial 与提示合并统一按[集合恢复契约](../contracts/protocol.md#collection-snapshots)实现；此表仅为临时查询状态，不增加全局目录或业务 owner。\n'
 s=s[:pos]+extra+s[pos:];p.write_text(s)
p=base/'extensions/README.md';s=p.read_text().replace('Activation：phase、generation、ready_instance、last_observed_at','Activation：revision、phase、generation、ready_instance、last_observed_at').replace('phase 使用状态图；ready_instance 必须属于当前宿主实例，重启重新取证','revision 跟踪持久可见投影，每次变化递增；generation 仅为活动代际。phase 使用状态图；ready_instance 必须属于当前宿主实例，重启重新取证');p.write_text(s)
p=base/'extensions/implementation.md';s=p.read_text();needle='| Activation.instance_readiness |';pos=s.index(needle);end=s.index('\n',pos);s=s[:end+1]+'| Activation.revision | 本 owner 持久可见投影修订，独立于 generation | phase／ready／残留变化同事务递增并写提示责任；read、list、Change 共用该修订，迟到低修订不可覆盖 |\n'+s[end+1:];p.write_text(s)
p=base/'interaction/implementation.md';s=p.read_text();needle='每页重新复核权限，失效项跳过，exhausted 只表示本次集合结束。';s=s.replace(needle,needle+'\n\n用于按类型订阅恢复时，filters 必须覆盖全部当前获准 Surface（app_ids=[]、task_refs=[]、include_expired=true）。200 项冻结上限截断时必须保留 partial／gaps，不能把末页当作完整目录；权限范围变化废弃旧页集合并重新订阅，新增可见的旧 Surface 也从新集合发现。完整条件、先订阅再枚举及有限重试统一见[集合恢复](../contracts/protocol.md#collection-snapshots)，固定截断不触发相同快照的立即循环。');p.write_text(s)
