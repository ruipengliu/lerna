import json
from pathlib import Path
owners={
'引用所指的内容 owner；值对象':'值引用 · 定位内容 owner',
'保存该依赖的记录 owner；值对象':'值对象 · 随依赖记录保存',
'原内容 owner 登记；holder 持有字节':'内容 owner · holder 持有字节',
'实际 consumer owner；本页关联取 Grant owner 分支':'consumer owner（本页：Grant）',
'Grant owner；usage_owner_id 指实际计量者':'Grant owner · 计量事实另属使用端',
'Grant owner；固定 endpoint / instance 使用':'Grant owner · 固定端点实例',
'实际业务 owner（Home 或应用 owner）':'业务 owner · Home / 应用',
'实际 consumer owner；非交互服务':'实际 consumer owner',
'Extensions 索引 / 各业务 owner 持有事实':'Extensions 索引 · 跨 owner 持有事实'
}
for p in Path('.scratch/uml-models').glob('*.json'):
 d=json.loads(p.read_text())
 if 'module' not in d or 'data' not in d:continue
 for n in d['data']['nodes']:
  n['owner']=owners.get(n['owner'],n['owner'])
  for a in n['attributes']:
   if '|' in a['type'] and len(a['type'])>29:a['type']='Enum'
 p.write_text(json.dumps(d,ensure_ascii=False,indent=2)+'\n')
p=Path('.scratch/uml-models/build_globals.py');s=p.read_text().replace("'operation','content','0..*','0..*','证据 / 结果'","'operation','content','0..*','0..101','证据 / 结果'");p.write_text(s)
