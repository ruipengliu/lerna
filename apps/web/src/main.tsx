import { StrictMode, useEffect, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { getHostStatus, type HostStatus } from '@lerna/sdk';
import topology from '../../../dev/topology.json';
import './style.css';

declare const __LERNA_DEV_MODE__: 'single' | 'multiprocess';
const hosts = topology[__LERNA_DEV_MODE__];
interface HostView { name: string; status?: HostStatus; error?: string }

function App() {
  const [results, setResults] = useState<HostView[]>(hosts.map(host => ({ name: host.name })));
  const [updatedAt, setUpdatedAt] = useState('');

  useEffect(() => {
    const controller = new AbortController();
    let timer: ReturnType<typeof setTimeout>;
    async function refresh() {
      const next = await Promise.all(hosts.map(async host => {
        try {
          const signal = AbortSignal.any([controller.signal, AbortSignal.timeout(3000)]);
          return { name: host.name, status: await getHostStatus(`/dev-api/${host.name}`, signal) };
        } catch {
          return { name: host.name, error: '暂时无法连接' };
        }
      }));
      if (controller.signal.aborted) return;
      setResults(next);
      setUpdatedAt(new Date().toLocaleTimeString('zh-CN'));
      timer = setTimeout(refresh, 3000);
    }
    void refresh();
    return () => { controller.abort(); clearTimeout(timer); };
  }, []);

  const available = results.filter(result => result.status && Object.values(result.status.dependencies).every(Boolean)).length;
  return (
    <main>
      <p className="eyebrow">LERNA / LOCAL DEVELOPMENT</p>
      <h1>本地工程骨架</h1>
      <p className="intro">{__LERNA_DEV_MODE__ === 'single' ? 'SQLite 单体' : 'PostgreSQL 多进程'}装配 · {available}/{hosts.length} 个宿主依赖可用</p>
      <p className="notice">任务能力待实现，当前保持关闭。这里显示进程和依赖的实际状态。</p>
      <div className="hosts">
        {results.map(result => (
          <article key={result.name}>
            <div className="host-title"><h2>{result.name}</h2><span className={result.status ? 'connected' : 'offline'}>{result.status ? '已连接' : result.error ?? '连接中'}</span></div>
            {result.status ? (
              <>
                <dl>
                  <div><dt>数据库</dt><dd>{result.status.dependencies.database ? '可连接' : '不可连接'}</dd></div>
                  <div><dt>内容目录</dt><dd>{result.status.dependencies.content_root ? '可用' : '缺失'}</dd></div>
                  <div><dt>文件目标</dt><dd>{result.status.dependencies.managed_root ? '可用' : '缺失'}</dd></div>
                  <div><dt>业务接纳</dt><dd>{result.status.accepting_tasks ? '已开放' : '关闭'}</dd></div>
                </dl>
                <p className="instance">{result.status.instance_id} · {result.status.phase}</p>
              </>
            ) : <p className="empty">等待宿主状态</p>}
          </article>
        ))}
      </div>
      <footer>每 3 秒核对一次{updatedAt ? ` · 最近核对 ${updatedAt}` : ''}</footer>
    </main>
  );
}

const container = document.getElementById('root');
if (!container) throw new Error('Missing application root');
createRoot(container).render(<StrictMode><App /></StrictMode>);
