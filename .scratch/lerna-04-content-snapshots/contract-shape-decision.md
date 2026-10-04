# 04 首票 1.2 Content 机器合同形状决定

2026-10-04；基线 `f706fe4a61e091c4a7d28bd31137a7fc0ffed6e5`。全文只读 `/workspace/lerna-content-01-d15e5e390c46262d/create-schema.py`（未执行草稿），并核对正式04 spec/decisions/final-api-handoff及既有1.0/1.1机器Schema、receipts/readfacts/commands和ADR0006/0007相关设计。**采用其三方法框架，按下列必要收紧后生成；不增加1.2 Decision、Task、Grant或Provider。** 此文是已授权的具体决定，不需要用户再次确认；不是实现/测试证据。

## 1. 直接修改清单

1. 草稿三个请求的可选 `trace` 必须改为 **trace_context**。沿公共Envelope原字段，排除于业务摘要；不能让通用ParseCommand拒绝typed请求自身字段。请求不接受expected_revision；本片以准确不可变版本创建，没有伪造的更新修订。
2. 1.2 accepted 的 `object_ref` 指向 **ContentTarget**，而非可含任意kind/revision的通用ObjectRef；准确tenant/owner/id分别等于content_ref.owner/content_id。**删除accepted顶层revision**，不使用Content.version或Job/work revision填它。必需字段为 `state,command_ref,object_ref,content_ref,retain_until`；可选next_action固定query_original。
3. accepted新增 **retain_until**：首次接纳时固定的有效保留上限，按请求和全部适用政策/来源取最早值。它是固定历史许可上限，不保证正文一定保留到该刻，也不覆盖后续更早撤权/收紧。这样调用方能看见请求期限被收紧后的实际接纳值，不默认为保存到请求时间。
4. bytes_base64不仅maxLength：使用严格RFC4648标准字母表、padding形状，Go/TS语义验证解码后≤262144、重编码全字节相等。允许空字符串。349528是最大合法编码长度，但该长度本身仍可编码262146字节，不能代替解码上限。
5. sources设置 `uniqueItems:true` 并增加准确版本身份去重/冲突语义检查；全ref重复、同owner/id/version不同hash/media/length均拒绝。不能靠uniqueItems把同版本异声明当两个来源。继承闭包上限也必须实际检查。
6. 草稿各get状态、content progress、三方法inventory保留；补本稿§4–6的跨字段/响应绑定验证与精确定义，不能仅Schema通过就称正确。1.2 `responseSchema` 分类要覆盖ContentGetResponse及其分支，使非法服务端观察被拒绝。

其余结构按下面采用。1.2 Schema未发布，以上属于本票闭合合同首次定形，不改冻结1.0/1.1、不新增ADR。

## 2. 准确身份、固定声明及Job

- Content version采用正int64十进制字符串（1..9223372036854775807），非连续亦合法；`(tenant,owner,content_id,version)`唯一且不可覆写。同名不同version是不同内容；hash/media_type/byte_length是该版本必核固定声明，不是另一个可任选版本。
- put/get target均用ContentTarget：tenant_id、owner_id、kind=content、id；与payload.content_ref的owner/id完全相等。version只在content_ref中，公共target的id不换成内部hash。
- put payload采用 `{content_ref,sources,purpose,bytes_base64,retain_until}` 全必需。purpose采用草稿有界128字符标识语法，区分大小写，不是授权；正文hash为原解码字节SHA256，byte_length精确等于字节数，media_type保留既有无参数语法。
- 固定版本声明同时绑定准确ref、正文、source集合、purpose、**原请求retain_until**。sources作为集合规范排序用于版本声明比较；原CommandDigest仍按原请求数组顺序固定，不能规范重排来消除同Command异摘要冲突。
- 原Command同摘要：先当前主体/原命令访问权限，再读并返回原固定回执，优先于新的accept_before/保留期判断；不重新发布、不改回执。异摘要idempotency_conflict。当前无权不得借幂等查询泄露原回执。
- 新Command同版本同固定声明：在当前准入资格有效时关联原责任/事实，保存**新Command自己的accepted**，不新增出版Job或复活failed/gone；新回执content_ref及首次记录的有效retain_until相同。不是复制另一个command_ref的receipt。新Command异声明version_conflict；同字节改变来源、purpose或请求期限也属异声明。
- 若原版本已failed/gone，新同声明命令即使被允许关联，也只指向原失败/清理事实，不重开出版。已经到期或当前失去新准入许可则固定expired/forbidden，不冒充有新保存责任。原Command获准重放与新Command准入不是同一判断。
- 原版本当前保留期收紧不能重写已accepted回执；存储分别保存原固定声明/首次有效上限、当前生效上限和关闭状态。版本冲突比较不得把后来政策修订混入原声明。
- 内部Job.Object.ID可用 `cv-` 加完整64位hex SHA256，preimage为固定domain-prefix+闭合canonical版本身份元组（不含hash/media/body）。准确字段单独耐久存储并唯一约束；每次Claim/Start/Finish验证hash ID与原元组、record/Job owner一致，摘要碰撞或异绑定拒绝，不能按hash盲认同一对象。公共ref/receipt始终使用原content_id/version；runtime.Job不需要新字段或业务版本语义。

## 3. retain_until、当前权限与有限出版期限

retain_until是**原请求的绝对保留上限**，不是TTL、accept_before或新的执行截止。首次准入按数据库当前时间取：
`effective = min(request.retain_until, applicable source/policy retention caps)`；
effective≤now时固定expired、无新出版责任。来源未获read/process/save或purpose无交集时forbidden；原来源缺失/未published/不可核验不允许先保存派生正文。原声明的保存资格也覆盖PG staging这个真实holder。

当前授权与来源政策可后来收紧期限，绝不延长原effective；所有新处理/保存/披露入口检查较早当前截止。到期必须先封使用并由耐久维护/清理责任收尾；get不执行cleanup，不因读请求创建Job。已披露字节无法收回，真实残留仍负责。

首次出版必须有原有限截止与重试上限：推荐保存 `publish_deadline = min(effective, admitted_at + configured_finite_publish_budget)`，预算配置在启动时显式给定并随接纳固定，不在重开时重算。它是该owner的有限处理政策，不另开公共Task deadline；超时记录失败及实际残留/清理责任。accept_before仅控制首次接纳；新get的accept_before仅控制本次读取，不改变原责任寿命。有限I/O/Claim期限还需截短到当前实际许可/出版截止。

政策fixture保持受信、耐久、可收紧，并分别区分read/process/save/sync/disclose。对content.get既检查查询主体的准确scope/purpose及所需读取/披露用途，也核全部适用来源当前资格；payload purpose或ContentRef不授予权限。首票未开放的派生/同步等路径明确拒绝，不通过允许全部来占位；后票所需完整来源交集保持原边，不成为首票额外整片前置。

## 4. put回执、拒绝及错误分层

异步put只产生accepted/rejected，删除原通用applied分支是正确的：对象已存在并published时的新同声明命令也返回固定accepted，current progress另读，不发明同步Task应用成功。

草稿新增五个ErrorCode可采用：version_conflict、integrity、input_over_limit、range_invalid、source_unavailable；不改旧版本枚举。领域限定：
- 格式非法、未知/重复字段、不规范base64/数值、target不匹配：schema_invalid。
- 已规范解码但声明hash或byte_length不符：integrity；字节超有限额度：input_over_limit。
- 当前无权：forbidden；不得用source_unavailable暴露未经授权来源是否存在。
- 仅对已获准可观察的确切来源，不能得到合格已发布输入：source_unavailable；暂时无法可信读取元数据/政策是dependency_unavailable，不能当not_found。
- 受控range越界：range_invalid，按授权及状态可见性规则返回，不通过范围错误先透露实际长度。

严格输入不能形成可信身份时只返PublicError，不勉强写拒绝账本。可进入受信原Command准入的合法请求发生领域拒绝时，在原owner固定Rejected；修改请求只能用新Command。同Tx接纳/拒绝与提交未知、容量拒绝、当前auth及原receipt优先级沿既有机制实现，不吞数据库未知为固定rejected。

初始publication failed原因保留草稿 integrity/dependency_unavailable/forbidden/expired/source_unavailable；其中dependency_unavailable只在**耐久确认决定不再出版**（原有限重试预算结束并保留清理责任）时使用。暂时对象服务不可用仍preparing/有限重试，或本次get unavailable；failed不是“刚超时一次”。

## 5. content.get精确公开语义

采用 `{content_ref,purpose,range?}`；所有非rejected响应回传请求的**完整准确ContentRef**，客户端decode响应也绑定原ref。对于有权限的已存在版本，请求hash/media/length与原声明不一致返回rejected integrity；不能当not_found、不能自动回另一版本/正文。

| status | 允许表达的事实 |
| --- | --- |
| preparing | 权威记录已接纳原版本，尚无published；不返回staging正文 |
| published | 当前获准，原对象独立读回完整hash/length核验通过，返回canonical base64；range只影响返回切片 |
| failed | 原版本记录了不可继续出版的终态原因；不修改原accepted，不代表清理全部完成 |
| gone | 已有明确持久正文清理事实、允许披露最小元数据，evidence_available=false；不能仅因撤权/到期/读文件失败推出gone，更不代表所有离线holder擦除 |
| unavailable | 当前不能取得可信观察或已published字节损坏：reason=dependency_unavailable或integrity；不能降成not_found/gone |
| not_found | 在原owner、获准的查询scope内，确知没有该准确版本记录；不证明别的owner或迟到提交不存在 |
| rejected | 当前权限不足、读取请求expired、受控range不合法等；不披露其他版本或内部来源身份 |

保留期到但物理删除尚未确认：封正文读取，按当前可披露权限返回rejected expired/forbidden，**不得先用gone冒充清理完成**。后续确认本owner正文gone后可在独立获准metadata访问下返回gone；该最小元数据访问不要求已被撤销的正文读取资格，却须自己的当前授权。离线holder残留由受信holder观察承担，gone的evidence_available=false只指此内容接口不能再提供正文，不宣称global erased。

已published后损坏/丢文件返回unavailable，历史publication保持published；仅明确清理事实才gone。get既不修复字节、不推动Job，也不把暂不可读改为publication failed。

本次不另加get响应保留期字段：accepted含首次有效上限，当前使用是否允许由当前get裁决；这两点已足够，不把accepted上限当当前可读保证。以后若真实调用需要当前期限详情应按准确合同版本扩展，不能塞未知字段。

## 6. range与响应约束

range采用offset/length非负int64十进制字符串。先做安全解析；公开边界获准后满足 `offset <= total && length <= total-offset`，避免offset+length溢出。零长范围在0..EOF合法；空对象支持全读或{0,0}。不自动截短超尾范围。

完整对象hash/length通过后才切片。published响应：无range请求则无range字段，正文长度等于ref.byte_length；有range请求则必须回完全相同range，返回正文解码长度等于length，即使range刚好覆盖全部正文也保留该字段。ContentRef始终描述**完整原对象**，不把partial hash/length塞进去；客户端不能凭partial字节声称独立核了whole hash。返回空字节同样canonical base64空字符串。Go/TS decoder须把响应与原请求绑定，不接受非请求范围或另一个准确版本。

继承1MiB总包、深度和Unicode规则；所有exact整数不经JS number。读策略实现可以有界全读≤256KiB核验再切片；Content-backed worker adapter仍先比较ref.byte_length与自己的remaining bound，不因get有固定上限就忽略更小worker容量。

## 7. command.get与旧reader

沿既有1.1 ReadCommandFacts原则：先准确原owner/currentauth，读取请求有独立有限截止；返回固定receipt和当前事实，查询不接纳工作。command.get envelope target绑定payload.command_ref，不把read自己的command_id当原Command身份。

草稿 `CommandProgressContent{kind:content,content_ref,publication:preparing|published|failed}` 可以保留：publication仅是历史发布进程，不代表当前正文可读/仍获准。包括正文gone时也可继续published；真实读取看content.get。拒绝回执只能progress none；accepted若不能取得可信对象进展则progress unavailable，不伪造none/preparing。无需把所有ContentGet状态再复制进CommandProgress。

1.2 response语义验证至少包括：command_ref等于receipt.command_ref；accepted object_ref为准确content target；receipt.content_ref匹配object_ref；progress.content_ref六字段与receipt一致；rejected不带content progress；published/get各范围长度和原ref约束。错误cause只留本地，公开拒绝不泄露正文或SQL。

旧1.0/1.1 accepted无法无损表达新增必需content_ref/retain_until，所以旧reader对此**返回unavailable**，不得通过删除新字段投影一个有歧义的“成功”回执。新reader对旧非Content receipt也不能伪造Content字段；只声明本1.2 Content账本读取范围，不建立通用跨版本registry。旧合同及archive零改。

## 8. 生成/首票出口

methods清单准确只有 command.get、content.put、content.get，advertised=false直到相应完整profile正式开放；局部真实entry/合同测试仍可直接调用，首票不等待whole关闭。typed完整schema、Envelope摘要入口与生成类型须一致；通用CommandEnvelope接受未知方法用于摘要，不等于方法被支持。

本票必要正常/反例包括trace_context与未知trace、双版本独立Job/receipt、原命令重传与新同声明、来源/期限改变的版本冲突、有效retain被来源缩短、expired但未删不gone、缺/损对象、canonical base64余位/边界、range空与EOF/越界/溢出、CommandProgress错ref拒绝及旧reader不损失桥接。域正常行为经真实PG+对象put/get/重开完成；本稿未执行任何一项，也未要求首票提前实现后票完整删除/holder/竞争/SIGKILL矩阵。

