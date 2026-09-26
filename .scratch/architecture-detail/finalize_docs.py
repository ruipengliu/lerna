from pathlib import Path
import json

root=Path('docs/architecture')
methods=json.loads((root/'contracts/schemas/methods.json').read_text())['methods']
groups={}
for name,spec in methods.items():
    groups.setdefault(spec['source'].split('/')[0],[]).append((name,spec))
lines=['# 领域方法签名索引','','[线格式](protocol.md) · [机器登记](schemas/methods.json) · [共享 Schema](schemas/protocol.schema.json)','',
       f'本表是同版登记的 {len(methods)} 个方法的查阅视图。输入、输出定义名指向共享 Schema 的 `$defs`，完整字段以 Schema 为准；成功含义及异常责任按所属模块定义。`条件` 表示 Command 必须携带 expected_revision，Query 不携带。全部方法属于未发布草案。','']
for group,items in sorted(groups.items()):
    lines += ['## '+group,'',f'[领域规则](../{group}/README.md) · [实现设计](../{group}/implementation.md)','','| 方法 | 种类／条件 | 输入 → 输出 | 回执阶段 |','| --- | --- | --- | --- |']
    for name,s in sorted(items):
        kind=s['kind']+('／条件' if s['expected_revision'] else '')
        stages='、'.join(s['stages']) if s['stages'] else 'QueryResult／Error'
        lines += [f'| `{name}` | {kind} | {s["input"]} → {s["output"]} | {stages} |']
    lines += ['']
(root/'contracts/methods.md').write_text('\n'.join(lines))
p=root/'contracts/protocol.md';s=p.read_text()
s=s.replace('原有40个严格方法与本轮补齐的53个保留方法共同构成93个领域方法；当前登记无 reserved 方法。','原有 40 个严格方法、本轮补齐的 53 个保留方法，以及为预算关闭、输入请求读取、在线费用结算与可信确认补充的 8 个交接方法，共构成 101 个领域方法；当前登记无 reserved 方法。')
s=s.replace('## 4. 93个方法的覆盖','## 4. 方法覆盖').replace('全部93方法','全部 101 个方法')
p.write_text(s)
p=root/'contracts/examples/protocol/README.md';s=p.read_text()
s=s.replace('# 核心调用与恢复夹具','# 领域调用与恢复夹具')
s=s.replace('本目录的 18 个 JSON 文件是有界的跨调用记录序列，涵盖 40 个严格登记方法。`invalid-mutations.json` 保存 99 个反例、来源文件、变更路径和应命中的规则；校验器逐项应用到原例副本，不修改原例。方法登记还列出当前不能宣称支持的 53 个 reserved 方法。','本目录的 47 个 JSON 文件是有界的跨调用记录序列，覆盖全部 101 个严格登记方法。`invalid-mutations.json` 保存 290 个定向反例、来源文件、变更路径和应命中的规则；校验器逐项应用到原例副本，不修改原例。每个方法的准确输入、输出、种类及错误恢复动作见[方法登记](../../schemas/methods.json)。')
s=s.replace('`input_requests` 与 `approvals`','`input_requests`、`approvals` 与 `confirmations`')
extra={
'20-budget-allocation':'任务调额、父侧划拨及按原接收方封账结算',
'21-capability-catalog':'按候选查准确能力、参数及效果核对声明',
'22-resource-lifecycle':'设备占用、续期、观察、本人接管及交还',
'23-cross-home-budget':'跨 Home 创建与 receiver 关闭竞争，原分配只结算一次',
'30-grant-lifecycle':'确认签发、当前读取及撤销',
'31-pairing-lifecycle':'预认证请求、本人批准、领取及凭据撤销',
'32-lease-settlement':'固定实例的离线租约及最终结算',
'33-installation-lifecycle':'安装锁、停用及无引用清理',
'34-compatibility-release':'首装兼容报告、批准与逐目标发布',
'35-improvement-exposure':'正式改善策略、保留占用及泄露后撤回',
'36-local-install-restart':'共库首装与重启，本次实例另取开放依据',
'37-remote-instance-reopen':'远端新实例取得 reopen，历史激活依据保持不变',
'38-revoked-approval-startup':'保留历史查询，撤回后拒绝新启动',
'39-evaluation-cancel':'取消实验及独立环境清理责任',
'40-content-lifecycle':'上传发布、交付前登记副本、内容关闭及清理',
'41-memory-query-list':'准确修订读取与无正文管理',
'42-memory-extraction':'排队提取、有限 Home 任务与候选一次发布',
'43-memory-view-sync':'快照与连续墓碑先提交再确认',
'44-surface-lifecycle':'完整快照更新与设备关闭意图',
'45-application-delivery':'已注册处理端的原命令转交及业务消费',
'46-content-restriction':'收紧用途，物理残留单独记录',
'47-declarative-form':'准确请求版本、类型化表单与禁止脚本',
'48-memory-changed-pages':'并发删除后的固定分页位置及披露复核',
'49-source-projection':'来源修订推进与预授权有限提取',
'50-online-use-settlement':'在线使用累计费用、未知保留及最终差额释放',
'51-online-use-once-zero':'最终零费用不返还一次性使用身份',
'52-confirmation-grant':'Grant owner 登记挑战、本人决定及签发事务消费',
'53-confirmation-release':'评测 owner 确认精确发布命令并批准',
'54-confirmation-acceptance':'Task Home 确认精确验收命令并一次消费',
}
rows='\n'.join(f'| [{name}]({name}.json) | {desc} |' for name,desc in extra.items())
s=s.replace('\n在仓库根目录运行：','\n'+rows+'\n\n在仓库根目录运行：')
s=s.replace('| [18-unrelated-effect](18-unrelated-effect.json) | 其他任务的未知操作不混入本任务完成门槛 |\n\n','| [18-unrelated-effect](18-unrelated-effect.json) | 其他任务的未知操作不混入本任务完成门槛 |\n')
p.write_text(s)
p=root/'review.md';s=p.read_text()
s=s.replace('本记录分别保留首次成稿与本轮补强的检查范围。','本记录保留各阶段历史检查范围；当前九模块详细设计的范围和结果见第 8 节。')
s=s.replace('## 4. 本轮补强范围','## 4. 核心契约补强记录（历史）')
s=s.replace('reserved 方法的完整线字段、SDK、默认组件和真实互操作套件仍待交付；它们不能借已覆盖方法的通过结果声明可用。','现有领域方法的完整字段已在本次详细设计补齐；SDK、默认组件和真实互操作套件仍待实现，不能借构造序列通过声明服务可用。')
s=s.replace('python3 -B docs/architecture/validation/validate_protocol.py\n','python3 -B docs/architecture/validation/validate_protocol.py\npython3 -B docs/architecture/validation/validate_transport.py\n')
p.write_text(s)
