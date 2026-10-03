import type { ReactNode } from "react";
import type { ConnectionState } from "@harness/sdk";
export const navigation = [
  { id: "work", title: "工作台", icon: "home" },
  { id: "interaction", title: "输入与分支", icon: "branch" },
  { id: "memory", title: "内容与记忆", icon: "file" },
  { id: "governance", title: "授权与治理", icon: "shield" },
  { id: "installation", title: "安装与评测", icon: "gear" },
] as const;
export type NavigationID = (typeof navigation)[number]["id"];
function Icon({ name }: { name: string }) {
  const paths: Record<string, string> = {
    home: "M3 11 12 3l9 8M5 10v11h5v-6h4v6h5V10",
    branch: "M6 3v12a4 4 0 0 0 4 4h8M6 7h7a4 4 0 0 0 4-4",
    file: "M5 3h9l5 5v13H5ZM14 3v6h5",
    shield: "M12 3 4 6v6c0 5 8 9 8 9s8-4 8-9V6ZM12 7v7m0 3v1",
    gear: "m9 3-.8 3-2.5 1-2.7-1-2 4 2 2v3l-2 2 2 4 3-1 2 1 1 3h5l1-3 2-1 3 1 2-4-2-2v-3l2-2-2-4-3 1-2-1-.8-3ZM12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6",
  };
  return (
    <svg
      viewBox="0 0 24 24"
      width="20"
      height="20"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
    >
      <path d={paths[name]} />
    </svg>
  );
}
export function Shell({
  selected,
  onSelect,
  connection,
  onReconnect,
  onDisconnect,
  children,
}: {
  selected: NavigationID;
  onSelect: (id: NavigationID) => void;
  connection: ConnectionState;
  onReconnect: () => void;
  onDisconnect: () => void;
  children: ReactNode;
}) {
  const title = navigation.find((entry) => entry.id === selected)?.title ?? "工作台";
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <button
          type="button"
          className="brand"
          onClick={() => {
            onSelect("work");
          }}
        >
          Harness
        </button>
        <nav aria-label="主导航">
          {navigation.map((entry) => (
            <button
              key={entry.id}
              type="button"
              className={selected === entry.id ? "nav-item active" : "nav-item"}
              aria-current={selected === entry.id ? "page" : undefined}
              onClick={() => onSelect(entry.id)}
            >
              <Icon name={entry.icon} />
              <span>{entry.title}</span>
            </button>
          ))}
        </nav>
        <div className="sidebar-bottom">
          <span>原身份 · 原 owner</span>
          <span>有界投递与可核验事实</span>
        </div>
      </aside>
      <div className="main-shell">
        <header className="app-header">
          <div>
            <h1>{title}</h1>
            <p>查看原服务保存的任务与交付事实。</p>
          </div>
          <div className="header-actions">
            <span className={`connection ${connection}`}>
              <i />
              {connection === "ready"
                ? "已连接"
                : connection === "connecting"
                  ? "连接中"
                  : "未连接"}
            </span>
            <button
              className="text-button"
              type="button"
              onClick={connection === "ready" ? onDisconnect : onReconnect}
            >
              {connection === "ready" ? "断开界面" : "连接原服务"}
            </button>
            <button
              className="button primary"
              type="button"
              onClick={() => {
                onSelect("work");
                requestAnimationFrame(() =>
                  document
                    .getElementById("new-goal")
                    ?.scrollIntoView({ behavior: "smooth", block: "start" }),
                );
              }}
            >
              新建目标
            </button>
          </div>
        </header>
        <main>{children}</main>
        <footer className="app-footer">
          <span>Harness · 原服务事实</span>
          <span>harness/1 · architecture-2026-10-data1</span>
        </footer>
      </div>
    </div>
  );
}
