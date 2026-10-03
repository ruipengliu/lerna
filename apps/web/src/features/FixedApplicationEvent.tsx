import { useEffect, useRef, useState } from "react";
import { isObject, validateRecord, validateSchema } from "@harness/sdk";
import type { ApplicationEventContract, HarnessClient, JSONValue, ObjectRef } from "@harness/sdk";
import { initialPayload, RestrictedForm } from "../components/RestrictedForm";

export function FixedApplicationEvent({
  client,
  rule,
  target,
  generation,
  intent,
  surfaceRef,
  rendered,
}: {
  client: HarnessClient;
  rule: ApplicationEventContract;
  target: string;
  generation: number;
  intent: number;
  surfaceRef: ObjectRef;
  rendered: boolean;
}) {
  const [payload, setPayload] = useState<JSONValue>(() => initialPayload(rule.schema));
  const [valid, setValid] = useState(true);
  const [running, setRunning] = useState(false);
  const [event, setEvent] = useState<{ ref: ObjectRef; view?: JSONValue }>();
  const [error, setError] = useState("");
  const epoch = useRef(0);
  useEffect(
    () => () => {
      epoch.current++;
    },
    [],
  );
  const canRead = client.registry.discovery.methods.some(
    (method) => method.name === "application_event.read",
  );
  const perform = async (work: () => Promise<void>) => {
    if (running) return;
    const captured = epoch.current;
    setRunning(true);
    setError("");
    try {
      await work();
    } catch (failure) {
      if (captured === epoch.current)
        setError(failure instanceof Error ? failure.message : "固定事件责任当前未核验");
    } finally {
      if (captured === epoch.current) setRunning(false);
    }
  };
  const send = () =>
    perform(async () => {
      if (!valid || (rule.requires_rendered && !rendered)) return;
      validateSchema(rule.schema, payload);
      const captured = epoch.current;
      const receipt = await client.command(
        client.makeCommand(
          "application_event",
          target,
          {
            generation,
            intent_revision: intent,
            surface_ref: surfaceRef as unknown as JSONValue,
            name: rule.name,
            payload,
          },
          new Date(Date.now() + 60000).toISOString(),
        ),
      );
      if (
        receipt.stage !== "applied" ||
        !isObject(receipt.output) ||
        receipt.output.query_method !== "application_event.read"
      )
        throw new Error(
          receipt.error
            ? `${receipt.error.code}: ${receipt.error.reason}`
            : "事件进入原准备，仍未形成可查询事件决定",
        );
      const ref = validateRecord<ObjectRef>("ObjectRef", receipt.output.event_ref);
      if (ref.tenant_id !== surfaceRef.tenant_id || ref.owner_id !== surfaceRef.owner_id)
        throw new Error("事件未归属当前准确 Surface owner");
      if (captured === epoch.current) setEvent({ ref });
    });
  const read = () =>
    perform(async () => {
      if (!event) return;
      const captured = epoch.current;
      const value = await client.query(
        client.makeQuery("application_event.read", event.ref.object_id, {}),
      );
      if (captured === epoch.current) setEvent({ ref: event.ref, view: value });
    });
  const receipt =
    isObject(event?.view) && isObject(event.view.receipt) ? event.view.receipt : undefined;
  return (
    <section className="fixed-application-event">
      <h3>
        {rule.name === "archive_demo_session" ? "归档固定示例会话" : `固定应用事件 · ${rule.name}`}
      </h3>
      <p className="field-hint">
        事件 Schema 与绑定来自认证后的有限配置。owner 固定业务方法及目标，当前界面不会替换。
        {rule.name === "archive_demo_session"
          ? "此事件只归档明确登记的示例会话，任务不会因此取消。"
          : ""}
      </p>
      <RestrictedForm
        schema={rule.schema}
        value={payload}
        onChange={setPayload}
        onValidity={setValid}
      />
      <div className="trusted-actions">
        <button
          className="button primary"
          type="button"
          disabled={running || !valid || !!event || (rule.requires_rendered && !rendered)}
          onClick={() => void send()}
        >
          保存并提交固定事件
        </button>
        {event && (
          <button
            className="button secondary"
            type="button"
            disabled={running || !canRead}
            onClick={() => void read()}
          >
            查询原事件消费
          </button>
        )}
      </div>
      {event && (
        <p className="notice" role="status">
          事件已耐久接纳 <code>{event.ref.object_id}</code>。
          {receipt?.stage === "applied"
            ? "固定业务已提交 applied 决定。"
            : receipt?.stage === "rejected"
              ? "固定业务已拒绝，不能当作效果成功。"
              : "仍等待固定业务消费；排队与呈现不证明消费。"}
        </p>
      )}
      {error && (
        <p className="notice error" role="alert">
          {error}
        </p>
      )}
      {event?.view !== undefined && (
        <details open>
          <summary>原事件、固定命令与消费回执</summary>
          <pre>{JSON.stringify(event.view, null, 2)}</pre>
        </details>
      )}
    </section>
  );
}
