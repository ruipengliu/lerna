from pathlib import Path
import json
short={
'grant_revisions：本次必要许可':'必要许可修订','grant_refs：预分配来源':'预分配来源','endpoint_id + instance_id':'固定端点与实例','policy.parent_grant_ref':'父许可','原 allocate 命令的一次确认':'分配确认',
'content_ref：精确正文引用':'正文引用','sources：完整来源依赖':'来源依赖','content_ref：候选正文':'候选正文','memory_ref：唯一发布':'唯一发布','登记原版本的持有责任':'版本持有责任','同一当前身份与修订':'当前控制',
'snapshot：完整当前快照':'当前快照','request_refs：准确请求修订':'准确请求修订','request_id + request_revision':'提交请求修订','surface_id：独立应用事件':'独立应用事件','每 endpoint 的呈现意图':'设备呈现意图',
'parent_task_id / 父任务':'父任务','child_task_id / 同 Home 子':'同 Home 子任务','唯一 allocation_id':'唯一额度','固定 agent_binding':'精确绑定','精确 agent_ref':'精确声明',
'plan_id 与 plan_digest':'准确计划','导致失效的 exposure_ids':'使资格失效','可选 report_id / digest':'可选报告','approval_id / 逐目标激活':'逐目标激活',
'当前 instance_readiness 投影':'当前就绪投影','approval_id / 精确批准':'精确批准','manifest / 精确清单':'精确清单','被持有的 lock_id':'持有版本锁',
'completed 返回提案':'完成时返回','需要生成时唯一调用':'可选模型调用',
'原决策与恢复查询':'原决策 / 查询','原操作／控制与查询':'原操作 / 控制','准确字节与当前资格':'准确字节 / 资格','路径选择与输出校验':'路径 / 校验','取得／核对原使用':'原使用依据','正文字段':'正文字段',
'正文与来源的精确引用':'正文 / 来源','修订、查询集合与 jobs':'修订 / jobs','保存／处理／披露资格':'分用途使用','内容控制与持有者登记':'控制 / 持有者',
'原撤销修订与传播 job':'撤销 / 传播','会话、端点与原回执':'端点 / 回执','撤销传播与落实核对':'传播 / 核对',
'输入、固定目标命令与 job':'输入 / 原命令','领取与保存原消费回执':'领取 / 原回执','固定目标命令／原结果查询':'原命令 / 查询','规范意图与本人决定':'意图 / 本人决定',
'原使用身份 / 有限窗口':'原使用 / 窗口','当前绑定与 readiness':'当前绑定 / 就绪','串行读写 Gate／epoch':'Gate / epoch','通过后进入发送边界':'获准后发送','事务外核验原使用':'核验原使用',
'查原激活及当前 ready':'原激活 / 就绪'
}
for p in Path('.scratch/uml-models').glob('*.json'):
 d=json.loads(p.read_text())
 if 'module' not in d:continue
 for kind in ['data','component']:
  if kind not in d:continue
  for e in d[kind]['edges']:
   if e.get('label') in short:e['label']=short[e['label']]
 p.write_text(json.dumps(d,ensure_ascii=False,indent=2)+'\n')
