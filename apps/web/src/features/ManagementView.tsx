import { useMemo, useState } from "react";
import type { ContentRef, HarnessClient, JSONValue } from "@harness/sdk";
import type { NavigationID } from "../components/Shell";
import { CollectionView } from "./CollectionView";
import { MethodConsole } from "./MethodConsole";
import { contentRefs } from "./TaskInspector";
const areas = {
  interaction: {
    title: "输入与分支",
    prefixes: [
      "session.",
      "branch.",
      "submission.",
      "interaction.",
      "schedule.",
      "occurrence.",
      "input_request.",
    ],
    list: "session.list",
    description: "输入保存、应用转交与业务消费分别记录。切换分支不会迁移旧输出；归档不取消任务。",
  },
  memory: {
    title: "内容与记忆",
    prefixes: ["content.", "memory."],
    list: "memory.list",
    description: "按准确版本查看、纠正、收紧或删除长期记忆。正文发布与来源许可、清理责任分别维护。",
  },
  governance: {
    title: "授权与治理",
    prefixes: [
      "grant.",
      "confirmation.",
      "evidence.",
      "defect.",
      "authorization.",
      "release.",
      "policy.",
    ],
    list: "grant.list",
    description: "许可、本人确认、一次消费与效果独立成立。确认必须绑定准确原命令及当前请求版本。",
  },
  installation: {
    title: "安装与评测",
    prefixes: [
      "install.",
      "extension.",
      "activation.",
      "evaluation.",
      "evaluation_run.",
      "release_approval.",
      "install_lock.",
      "plugin.",
    ],
    list: "evaluation.list",
    description: "安装锁固定准确制品；启用、回退批准与冻结评测分别核验。未开放能力保持不可用。",
  },
} as const;
export function ManagementView({
  client,
  area,
  onPreview,
  onRequest,
}: {
  client: HarnessClient;
  area: Exclude<NavigationID, "work">;
  onPreview: (refs: ContentRef[]) => void;
  onRequest: (method: string, value: JSONValue) => void;
}) {
  const config = areas[area];
  const [selected, setSelected] = useState<JSONValue>();
  const methods = useMemo(
    () =>
      client.registry.discovery.methods.filter((method) =>
        config.prefixes.some((prefix) => method.name.startsWith(prefix)),
      ),
    [client, config],
  );
  const lists = methods.filter(
    (method) => method.kind === "query" && method.name.endsWith(".list"),
  );
  const list = lists.find((method) => method.name === config.list) ?? lists[0];
  return (
    <>
      <p className="area-description">{config.description}</p>
      <div className="management-grid">
        <div className="panel management-list">
          {list ? (
            <CollectionView
              client={client}
              methodName={list.name}
              title="当前授权集合"
              onSelect={(value) => {
                setSelected(value);
                onRequest(list.name, value);
              }}
            />
          ) : (
            <p className="notice">
              认证发现未开放此区域的列表。使用下方已开放的准确查询，不能从本地缓存推导全集。
            </p>
          )}
          {selected !== undefined && (
            <>
              <h3>选定原记录</h3>
              <pre className="record-view">{JSON.stringify(selected, null, 2)}</pre>
              <button
                className="text-button"
                type="button"
                onClick={() => onPreview(contentRefs(selected))}
              >
                预览准确引用
              </button>
            </>
          )}
        </div>
        <MethodConsole
          key={area}
          client={client}
          methods={methods}
          onResult={(method, value) => {
            onRequest(method, value as JSONValue);
          }}
        />
      </div>
    </>
  );
}
