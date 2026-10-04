import pathlib,sys,json,re,os
root=pathlib.Path(__file__).parent;mode,group,filename,session=sys.argv[1:]
path=root/'materialized-runtime-manifest.json';record=json.loads(path.read_text());g=record['groups'][group];log=root/filename;text=log.read_text()
assert f'ACTUAL_GROUP {mode} {group} {g["count"]}' in text and text.rstrip().endswith('NATIVE_EXIT status=0 group_absent=True')
actual=re.findall(r'^=== RUN\s+(\S+)\s*$',text,re.M);actual=[name for name in actual if '/' not in name]
passed=re.findall(r'^--- PASS: (\S+) \(([0-9.]+)s\)',text,re.M)
assert len(actual)==len(set(actual))==g['count'] and actual==g['names']
assert [name for name,_ in passed]==g['names']
g[mode]={'status':'passed','log':str(log),'tool_session':int(session),'actual_case_names':actual,'case_elapsed_sum_seconds':round(sum(float(seconds) for _,seconds in passed),3),'qualification':'sum of actual top-level case timings, not native wall-time','native_exit':0,'group_absent':True}
with open(path,'w') as f:json.dump(record,f,indent=2);f.write('\n');f.flush();os.fsync(f.fileno())
print(mode,group,g['count'],'actual0/absent','case_sum',g[mode]['case_elapsed_sum_seconds'])
