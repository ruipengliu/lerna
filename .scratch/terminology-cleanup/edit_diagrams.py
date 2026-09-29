from pathlib import Path
import xml.etree.ElementTree as ET
import json

pairs = [
    ('生产稳定 Orchestrator 分区 + 多可用区  /  同提交域短事务  /  跨域原命令与查询',
     '稳定 Orchestrator + 数据库分片 + 多可用区  /  本地短事务  /  跨库原命令与查询'),
    ('模块 ≠ owner ≠ 进程 ≠ 提交域', '模块、owner、进程与事务边界分别定义'),
    ('逻辑模块 ≠ owner ≠ 进程 ≠ 提交域', '逻辑模块、owner、进程与事务边界分别定义'),
    ('跨 owner 关联通过原身份核对，不是跨库事务', '跨 owner 关联按原对象标识核对，不承诺跨库事务'),
    ('组装／物化', '组装／步骤实例化'),
    ('内部职责共 Orchestrator 提交域', '内部职责共用 Orchestrator 本地事务'),
    ('准确字节 / 资格', '准确字节 / 使用权限'),
    ('按原身份接纳', '按原命令标识接纳'),
    ('原索引／关闭工作', '索引／内容停用作业'),
    ('各自提交域', '各自本地事务范围'), ('自身提交域', '自身本地事务范围'),
    ('当前启动资格与结算状态', '当前启动许可与结算状态'),
    ('沿原身份核对', '按原命令与对象标识核对'),
    ('暴露、资格与批准', '暴露、适用性与批准'),
    ('批准核验当前资格', '批准核验当前评测适用性'),
    ('独立当前资格', '独立适用状态'), ('使资格失效', '使评测不再适用'),
    ('锁、步骤与持久 jobs', '清单、步骤与持久 jobs'),
    ('当前资格及装配', '授权、评测与装配'),
    ('准确引用与当前资格', '准确引用与使用权限'),
    ('治理与资格', '治理与授权'),
    ('不可变报告与当前资格、批准分离', '不可变报告、当前适用性与批准分别保存'),
    ('安装锁', '安装锁定清单'),
    ('保留集', '独立测试集'),
    ('线字段', '协议字段'), ('线方法', '协议方法'), ('线格式', '协议编码格式'),
    ('模块形状', '模块结构'), ('软件形状', '模块结构'), ('合同', '契约'),
    ('提交域', '本地事务范围'), ('责任槽', '作业记录'), ('工作槽', '作业记录'),
    ('关闭索引', '去重与终态索引'), ('关闭依据', '去重与终态记录'),
]
manifest=[]
for p in Path('docs/architecture/diagrams').glob('*'):
    if p.suffix not in {'.drawio','.html'}:continue
    old=p.read_text(); s=old
    for a,b in pairs:s=s.replace(a,b)
    if s==old:continue
    p.write_text(s)
    if p.suffix=='.drawio':
        before=ET.fromstring(old).findall('diagram')
        after=ET.fromstring(s).findall('diagram')
        for index,(a,b) in enumerate(zip(before,after)):
            if ET.tostring(a)!=ET.tostring(b):
                manifest.append({'source':str(p),'page_index':index,'page_name':b.get('name'),'render_status':'pending'})
Path('.scratch/terminology-cleanup/drawio-manifest.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n')
print('Changed drawio pages:',len(manifest))
