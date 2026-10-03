import { isObject } from "@harness/sdk";
import type { HarnessClient, JSONValue, ContentRef } from "@harness/sdk";
import { recordID } from "./CollectionView";
function record(value: JSONValue | undefined): Record<string, JSONValue> | undefined {
  if (!isObject(value)) return;
  return isObject(value.task) ? value.task : value;
}
export function contentRefs(value: JSONValue | undefined): ContentRef[] {
  const refs: ContentRef[] = [];
  const walk = (item: JSONValue | undefined, depth: number) => {
    if (depth > 8 || !item || typeof item !== "object") return;
    if (Array.isArray(item)) {
      for (const entry of item) walk(entry, depth + 1);
      return;
    }
    if (
      typeof item.content_id === "string" &&
      typeof item.hash === "string" &&
      typeof item.version === "number" &&
      typeof item.media_type === "string" &&
      typeof item.byte_length === "number" &&
      typeof item.owner_id === "string" &&
      typeof item.tenant_id === "string"
    ) {
      refs.push(item as unknown as ContentRef);
      return;
    }
    for (const [key, entry] of Object.entries(item))
      if (
        [
          "goal_ref",
          "artifact_refs",
          "question_ref",
          "preview_refs",
          "candidate_ref",
          "limitations_ref",
          "result",
          "task",
          "ref",
          "content_ref",
          "view",
          "request",
        ].includes(key)
      )
        walk(entry, depth + 1);
  };
  walk(value, 0);
  return refs.filter(
    (ref, index) =>
      refs.findIndex(
        (other) =>
          other.hash === ref.hash &&
          other.content_id === ref.content_id &&
          other.version === ref.version,
      ) === index,
  );
}
export function TaskInspector({
  client,
  selected,
  onControl,
  onPreview,
}: {
  client?: HarnessClient;
  selected?: JSONValue | undefined;
  onControl: (method: string, target: string, revision?: number) => void;
  onPreview: (refs: ContentRef[]) => void;
}) {
  const task = record(selected);
  const taskID = task ? recordID(task) : "";
  const effects = task && isObject(task.open_effects) ? task.open_effects : undefined;
  const result = isObject(selected) && isObject(selected.result) ? selected.result : undefined;
  const facts: Array<[string, string]> = [
    ["接纳", task?.submit_command_id ? "原 Task 已持久接纳" : "等待原服务"],
    ["执行", task ? `启动门禁：${task.control ?? "未返回"}；执行落实需原记录` : "等待原服务"],
    [
      "效果",
      effects
        ? `原关系未结 ${effects.unresolved_count ?? "?"} 项${effects.complete === false ? "；集合有缺口" : ""}`
        : "等待原服务",
    ],
    [
      "任务完成",
      task
        ? `${task.status ?? "未返回"}${result ? ` · 依据 ${result.completion_basis ?? "未返回"}` : ""}`
        : "等待原服务",
    ],
    [
      "Result 发布",
      task?.result_ref
        ? isObject(selected) && selected.export_state === "published"
          ? "准确导出已发布"
          : "权威 Result 已保存；导出另核"
        : "等待原服务",
    ],
    [
      "费用",
      task
        ? task.accounting_open === true
          ? "原费用责任尚未结清"
          : task.accounting_open === false
            ? "原费用责任已结清"
            : "服务未返回费用事实"
        : "等待原服务",
    ],
    [
      "清理",
      isObject(selected) && typeof selected.cleanup_state === "string"
        ? selected.cleanup_state
        : "服务未返回清理事实",
    ],
  ];
  return (
    <section className="panel inspector">
      <h2>原任务事实</h2>
      {taskID && <code className="selected-id">{taskID}</code>}
      <dl className="facts">
        {facts.map(([label, value]) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
      <div className="task-controls">
        {[
          ["task.pause", "暂停"],
          ["task.resume", "恢复"],
          ["task.cancel", "取消"],
        ].map(([method, label]) => (
          <button
            key={method}
            className="button secondary"
            type="button"
            disabled={
              !taskID || !client?.registry.discovery.methods.some((entry) => entry.name === method)
            }
            onClick={() => {
              if (method)
                onControl(
                  method,
                  taskID,
                  typeof task?.revision === "number" ? task.revision : undefined,
                );
            }}
          >
            {label}
          </button>
        ))}
      </div>
      <p className="field-hint">
        接纳、执行和交付分别记录。控制决定不证明目标已停止，终态不代表费用或清理结束。
      </p>
      {task && (
        <>
          <button
            className="text-button"
            type="button"
            onClick={() => onPreview(contentRefs(selected))}
          >
            预览当前准确正文
          </button>
          <details>
            <summary>原服务准确记录</summary>
            <pre>{JSON.stringify(selected, null, 2)}</pre>
          </details>
        </>
      )}
    </section>
  );
}
