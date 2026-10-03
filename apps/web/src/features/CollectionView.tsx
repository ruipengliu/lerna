import { useCallback, useEffect, useState } from "react";
import { canonical, isObject } from "@harness/sdk";
import type { HarnessClient, JSONValue } from "@harness/sdk";
import { initialPayload } from "../components/RestrictedForm";
export function recordID(value: JSONValue): string {
  if (!isObject(value)) return "";
  for (const name of [
    "task_id",
    "session_id",
    "memory_id",
    "grant_id",
    "submission_id",
    "branch_id",
    "run_id",
    "install_lock_id",
    "request_id",
    "object_id",
    "id",
  ])
    if (typeof value[name] === "string") return value[name];
  for (const name of ["ref", "task_ref", "record", "task", "memory", "session", "submission"])
    if (value[name]) {
      const nested = recordID(value[name]);
      if (nested) return nested;
    }
  return "";
}
export function CollectionView({
  client,
  methodName,
  title,
  onSelect,
  refreshKey = 0,
}: {
  client: HarnessClient;
  methodName: string;
  title: string;
  onSelect?: (value: JSONValue) => void;
  refreshKey?: number;
}) {
  const method = client.registry.discovery.methods.find(
    (entry) => entry.name === methodName && entry.kind === "query",
  );
  const [view, setView] = useState<{
    identity: string;
    items: JSONValue[];
    exhausted: boolean;
    partial: boolean;
    gaps: JSONValue[];
    cursor?: string;
    revision?: number;
  }>();
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const identity = `${client.registry.discovery.identity_scope}:${client.registry.discovery.identity_revision}:${methodName}`;
  const load = useCallback(
    async (cursor?: string) => {
      if (!method) return;
      setLoading(true);
      setError("");
      try {
        const payload = initialPayload(method.input_schema);
        if (!isObject(payload)) throw new Error("列表合同不是闭合对象");
        if (cursor) payload.cursor = cursor;
        const response = await client.query(
          client.makeQuery(method.name, client.registry.discovery.logical_service_id, payload),
        );
        if (
          !isObject(response) ||
          !Array.isArray(response.items) ||
          typeof response.exhausted !== "boolean" ||
          typeof response.partial !== "boolean" ||
          !Array.isArray(response.gaps)
        )
          throw new Error("列表缺少集合合同");
        setView({
          identity,
          items: response.items,
          exhausted: response.exhausted,
          partial: response.partial,
          gaps: response.gaps,
          ...(typeof response.next_cursor === "string" ? { cursor: response.next_cursor } : {}),
          ...(typeof response.collection_revision === "number"
            ? { revision: response.collection_revision }
            : {}),
        });
      } catch (failure) {
        setError(failure instanceof Error ? failure.message : "原集合不可核验");
      } finally {
        setLoading(false);
      }
    },
    [client, method, identity],
  );
  useEffect(() => {
    if (refreshKey >= 0) void load();
  }, [load, refreshKey]);
  const current = view?.identity === identity ? view : undefined;
  return (
    <section className="collection">
      <div className="section-head">
        <h2>{title}</h2>
        <button
          className="text-button"
          type="button"
          disabled={!method || loading}
          onClick={() => void load()}
        >
          刷新原集合
        </button>
      </div>
      {!method ? (
        <p className="notice">当前未开放 {methodName}；无法将本地缓存视为完整集合。</p>
      ) : error ? (
        <p className="notice error" role="alert">
          {error}
        </p>
      ) : loading && !current ? (
        <p role="status">读取当前授权集合…</p>
      ) : (
        <>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>原身份</th>
                  <th>状态 / 修订</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {current?.items.length ? (
                  current.items.map((value) => (
                    <tr key={recordID(value) || canonical(value)}>
                      <td>
                        <code>{recordID(value) || "原服务记录"}</code>
                      </td>
                      <td>
                        {isObject(value) && typeof value.state === "string"
                          ? value.state
                          : isObject(value) && typeof value.status === "string"
                            ? value.status
                            : "查看准确事实"}
                        {isObject(value) && typeof value.revision === "number"
                          ? ` · r${value.revision}`
                          : ""}
                      </td>
                      <td>
                        <button
                          type="button"
                          className="text-button"
                          onClick={() => onSelect?.(value)}
                        >
                          查看
                        </button>
                      </td>
                    </tr>
                  ))
                ) : (
                  <tr>
                    <td colSpan={3} className="empty">
                      暂无记录。提交明确输入后，查看原 owner 保存的事实。
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          {current && (
            <div className="collection-footer">
              <span>
                集合修订 {current.revision ?? "未返回"} ·{" "}
                {current.partial
                  ? `存在缺口：${current.gaps.join("、") || "partial"}`
                  : "当前页无披露缺口"}
              </span>
              {current.cursor && (
                <button
                  className="text-button"
                  type="button"
                  disabled={loading}
                  onClick={() => void load(current.cursor)}
                >
                  读取原游标下一页
                </button>
              )}
              <span>{current.exhausted ? "本次枚举已结束" : "集合尚未枚举结束"}</span>
            </div>
          )}
        </>
      )}
    </section>
  );
}
