import { useEffect, useState } from "react";
import { canonical, isObject, RequestError } from "@harness/sdk";
import type { HarnessClient, JSONValue, MethodContract, Receipt } from "@harness/sdk";
import { RestrictedForm, initialPayload } from "../components/RestrictedForm";
export function MethodConsole({
  client,
  methods,
  preferred,
  preset,
  onResult,
}: {
  client: HarnessClient;
  methods: MethodContract[];
  preferred?: string;
  preset?: { target: string; revision?: number; payload?: JSONValue };
  onResult?: (method: string, value: JSONValue | Receipt) => void;
}) {
  const [name, setName] = useState(preferred ?? methods[0]?.name ?? "");
  const method = methods.find((item) => item.name === name) ?? methods[0];
  const [payload, setPayload] = useState<JSONValue>({});
  const [target, setTarget] = useState(
    preset?.target ?? client.registry.discovery.logical_service_id,
  );
  const [revision, setRevision] = useState(preset?.revision?.toString() ?? "");
  const [expires, setExpires] = useState(() => new Date(Date.now() + 60000).toISOString());
  const [valid, setValid] = useState(true);
  const [error, setError] = useState("");
  const [running, setRunning] = useState("");
  const [result, setResult] = useState<{ binding: string; value: JSONValue | Receipt }>();
  const binding = `${client.registry.discovery.identity_scope}:${client.registry.discovery.identity_revision}:${method?.name ?? ""}:${target}:${revision}:${expires}:${JSON.stringify(payload)}`;
  useEffect(() => {
    try {
      setPayload(preset?.payload ?? (method ? initialPayload(method.input_schema) : {}));
      setValid(true);
      setError("");
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "此合同不能用受限字段呈现");
      setValid(false);
    }
    setTarget(preset?.target ?? client.registry.discovery.logical_service_id);
    setRevision(preset?.revision?.toString() ?? "");
    setResult(undefined);
  }, [method, preset, client]);
  if (!method)
    return <div className="notice">当前认证发现未开放此能力。不会发送近似请求或分配业务对象。</div>;
  const requiresTrustedView = [
    "confirmation.decide",
    "task.input",
    "task.accept_result",
    "interaction.input",
    "presentation.rendered",
    "application_event",
  ].includes(method.name);
  const invoke = async () => {
    if (!valid || requiresTrustedView) return;
    const originalBinding = binding;
    setRunning(originalBinding);
    setError("");
    try {
      let value: JSONValue | Receipt;
      if (method.kind === "query")
        value = await client.query(client.makeQuery(method.name, target, payload));
      else {
        const expected = method.cas ? Number(revision) : undefined;
        if (method.cas && (!revision || !Number.isSafeInteger(expected) || (expected ?? 0) < 1))
          throw new Error("必须固定原对象修订，冲突后不得自动刷新");
        value = await client.command(
          client.makeCommand(method.name, target, payload, expires, expected),
        );
      }
      setResult({ binding: originalBinding, value });
      onResult?.(method.name, value);
    } catch (failure) {
      setError(
        failure instanceof RequestError
          ? `${failure.error.code} · ${failure.error.reason}（${failure.error.retry}）`
          : failure instanceof Error
            ? failure.message
            : "请求未取得原服务决定",
      );
    } finally {
      setRunning("");
    }
  };
  return (
    <section className="panel method-console">
      <div className="section-head">
        <h2>原方法与准确输入</h2>
        <span className="field-hint">以当前认证发现为准</span>
      </div>
      <div className="field">
        <label htmlFor="method-select">公开方法</label>
        <select
          id="method-select"
          value={method.name}
          onChange={(event) => {
            setName(event.target.value);
            setResult(undefined);
          }}
        >
          {methods.map((item) => (
            <option key={item.name} value={item.name}>
              {item.name} · {item.kind === "command" ? "原命令" : "查询"}
            </option>
          ))}
        </select>
      </div>
      <div className="field">
        <label htmlFor="method-target">固定目标 ID</label>
        <input
          id="method-target"
          value={target}
          onChange={(event) => setTarget(event.target.value)}
        />
      </div>
      {method.cas && (
        <div className="field">
          <label htmlFor="method-revision">原对象修订（CAS）</label>
          <input
            id="method-revision"
            type="number"
            min="1"
            value={revision}
            onChange={(event) => setRevision(event.target.value)}
          />
        </div>
      )}
      {method.kind === "command" && (
        <div className="field">
          <label htmlFor="method-expires">首次接纳截止（UTC）</label>
          <input
            id="method-expires"
            value={expires}
            onChange={(event) => setExpires(event.target.value)}
          />
          <span className="field-hint">保存后恢复沿原值发送，不延长此期限。</span>
        </div>
      )}
      <RestrictedForm
        key={method.name}
        schema={method.input_schema}
        value={payload}
        onChange={setPayload}
        onValidity={setValid}
      />
      <button
        className="button primary"
        type="button"
        disabled={!valid || requiresTrustedView || running === binding}
        onClick={() => void invoke()}
      >
        {running === binding
          ? "等待原服务…"
          : method.kind === "query"
            ? "查询原服务"
            : "耐久保存并提交"}
      </button>
      {requiresTrustedView && (
        <p className="notice">
          此方法需要完整原请求、必需正文或已登记事件绑定。请先查询 confirmation.read、
          input_request.read 或打开 Surface，在受信视图中核验准确版本后提交。
        </p>
      )}
      {error && (
        <p className="notice error" role="alert">
          {error}
        </p>
      )}
      {result?.binding === binding && (
        <div className="response">
          <h3>原服务返回</h3>
          {isObject(result.value) && typeof result.value.stage === "string" && (
            <p className="notice">
              回执阶段：{result.value.stage}。
              {result.value.stage === "accepted"
                ? "原准备责任已保存，继续查询此命令。"
                : "此方法的业务决定已提交；后续效果、费用和清理分别核对。"}
            </p>
          )}
          <pre>{JSON.stringify(result.value, null, 2)}</pre>
          <details>
            <summary>同版 Schema 摘要</summary>
            <code>{method.schema_digest}</code>
            <p>{method.recovery}</p>
            <code>{canonical(method.input_schema)}</code>
          </details>
        </div>
      )}
    </section>
  );
}
