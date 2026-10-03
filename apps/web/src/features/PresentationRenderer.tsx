import { useEffect, useMemo, useRef, useState } from "react";
import { canonical, isObject, newID, validateRecord } from "@harness/sdk";
import type {
  ContentRef,
  HarnessClient,
  InlineRenderBody,
  InputRequest,
  JSONValue,
  ObjectRef,
  Receipt,
  Schema,
  DevelopmentConfig,
} from "@harness/sdk";
import { TrustedPreview } from "../components/TrustedPreview";
import { TrustedRequest } from "./TrustedRequest";
import type { TrustedSelection } from "./TrustedRequest";
import { FixedApplicationEvent } from "./FixedApplicationEvent";

interface Surface {
  surface_id: string;
  tenant_id: string;
  owner_id: string;
  revision: number;
  state: string;
  binding_ref: ObjectRef;
  snapshot_ref: ContentRef;
  request_refs: ObjectRef[];
}
interface Presentation {
  presentation_id: string;
  revision: number;
  endpoint_id: string;
  instance_id: string;
  surface_ref: ObjectRef;
  state: string;
  intent_revision: number;
  generation: number;
  credential_generation: number;
  presented: boolean;
}
interface RenderRequest {
  request: InputRequest;
  answer_schema: Schema;
  method: string;
}
interface RenderView {
  surface: Surface;
  presentation: Presentation;
  required_refs: ContentRef[];
  bodies: InlineRenderBody[];
  requests: RenderRequest[];
  not_modified: boolean;
}
interface RenderState {
  surface?: Surface;
  presentation?: Presentation;
  view?: RenderView;
}
function surfaceRef(surface: Surface): ObjectRef {
  return validateRecord<ObjectRef>("ObjectRef", {
    tenant_id: surface.tenant_id,
    owner_id: surface.owner_id,
    object_id: surface.surface_id,
    revision: surface.revision,
  });
}
function requestRef(request: InputRequest): ObjectRef {
  return validateRecord<ObjectRef>("ObjectRef", {
    tenant_id: request.tenant_id,
    owner_id: request.owner_id,
    object_id: request.request_id,
    revision: request.revision,
  });
}
function renderKey(identity: string, presentation: Presentation): string {
  return `${identity}:${presentation.presentation_id}:${presentation.generation}:${presentation.intent_revision}:${canonical(presentation.surface_ref)}`;
}
function checkView(client: HarnessClient, value: unknown): RenderView {
  client.registry.validateOutput("presentation.read", value);
  const view = value as RenderView;
  const { surface, presentation } = view;
  if (
    surface.owner_id !== client.registry.discovery.logical_service_id ||
    surface.state !== "open" ||
    presentation.state !== "open" ||
    presentation.generation < 1 ||
    presentation.credential_generation !== client.registry.discovery.identity_revision ||
    canonical(surfaceRef(surface)) !== canonical(presentation.surface_ref) ||
    view.requests.length !== surface.request_refs.length
  )
    throw new Error("呈现主体、Surface 或原请求版本已改变，依赖操作保持关闭");
  const required: ContentRef[] = [surface.snapshot_ref];
  for (const [index, item] of view.requests.entries()) {
    const request = validateRecord<InputRequest>("InputRequest", item.request);
    if (canonical(requestRef(request)) !== canonical(surface.request_refs[index]))
      throw new Error("输入块未绑定 Surface 的准确原请求");
    for (const ref of [request.question_ref, ...request.preview_refs]) {
      if (!required.some((entry) => canonical(entry) === canonical(ref))) required.push(ref);
    }
  }
  if (canonical(required) !== canonical(view.required_refs))
    throw new Error("必需正文列表未覆盖准确快照与完整原请求，不能记录呈现");
  return view;
}
export function PresentationRenderer({
  client,
  selection,
  publish,
  onDone,
  development,
}: {
  client: HarnessClient;
  selection: TrustedSelection;
  publish: (
    value: JSONValue,
    next: (ref: ContentRef) => ReturnType<HarnessClient["makeCommand"]>,
  ) => Promise<{ content_ref: ContentRef; receipt?: Receipt }>;
  onDone: () => void;
  development?: DevelopmentConfig;
}) {
  const [state, setState] = useState<RenderState>({});
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [running, setRunning] = useState(false);
  const [verified, setVerified] = useState<string>();
  const [acknowledged, setAcknowledged] = useState("");
  const [selectedRequest, setSelectedRequest] = useState<string>();
  const epoch = useRef(0);
  const identity = `${client.registry.discovery.identity_scope}:${client.registry.discovery.identity_revision}`;
  const current = selection.identity === identity;
  const { surface, presentation, view } = state;
  const generation = presentation ? renderKey(identity, presentation) : "unopened";
  const refs = useMemo(() => view?.required_refs ?? [], [view]);
  const previewKey = `${generation}:${canonical(refs)}`;
  const bodyReady = current && !!view && verified === previewKey;
  const has = (name: string) =>
    client.registry.discovery.methods.some((method) => method.name === name);
  const active = current && presentation?.state === "open";
  const picked = view?.requests.find((item) => item.request.request_id === selectedRequest);
  const events =
    surface &&
    development?.application_binding_ref &&
    canonical(surface.binding_ref) === canonical(development.application_binding_ref) &&
    has("application_event")
      ? (development.application_events ?? [])
      : [];
  useEffect(() => {
    epoch.current++;
    setError("");
    setNotice("");
    setVerified(undefined);
    setAcknowledged("");
    setSelectedRequest(undefined);
    setRunning(false);
    try {
      let value = selection.value;
      if (isObject(value) && typeof value.stage === "string") {
        if (value.stage !== "applied")
          throw new Error("当前只核验原准备回执；呈现业务决定尚未成立");
        value = value.output ?? null;
      }
      if (selection.method === "presentation.read") {
        const checked = checkView(client, value);
        setState({ surface: checked.surface, presentation: checked.presentation, view: checked });
      } else if (selection.method.startsWith("surface.")) {
        client.registry.validateOutput(selection.method, value);
        const checked = value as unknown as Surface;
        if (surfaceRef(checked).owner_id !== client.registry.discovery.logical_service_id)
          throw new Error("Surface 不属于认证发现的固定 owner");
        setState({ surface: checked });
      } else {
        client.registry.validateOutput(selection.method, value);
        const checked = value as unknown as Presentation;
        if (checked.surface_ref.owner_id !== client.registry.discovery.logical_service_id)
          throw new Error("Presentation 不属于认证发现的固定 owner");
        setState({ presentation: checked });
      }
    } catch (failure) {
      setState({});
      setError(failure instanceof Error ? failure.message : "原呈现合同不可核验");
    }
    return () => {
      epoch.current++;
    };
  }, [client, selection]);
  const perform = async (work: (guard: () => boolean) => Promise<void>, hide = false) => {
    if (!current || running) return;
    const captured = ++epoch.current;
    const guard = () => captured === epoch.current;
    setRunning(true);
    setError("");
    setNotice("");
    if (hide) {
      setVerified(undefined);
      setAcknowledged("");
      setSelectedRequest(undefined);
      setState((previous) => {
        const next = { ...previous };
        delete next.view;
        return next;
      });
    }
    try {
      await work(guard);
    } catch (failure) {
      if (guard()) setError(failure instanceof Error ? failure.message : "原呈现责任未结");
    } finally {
      if (guard()) setRunning(false);
    }
  };
  const submit = async (method: string, target: string, payload: JSONValue, revision?: number) => {
    const receipt = await client.command(
      client.makeCommand(
        method,
        target,
        payload,
        new Date(Date.now() + 60000).toISOString(),
        revision,
      ),
    );
    if (receipt.stage !== "applied")
      throw new Error(
        receipt.error
          ? `${receipt.error.code}: ${receipt.error.reason}`
          : "原呈现业务决定仍未核验，请查询原命令",
      );
    return receipt.output as unknown as Presentation;
  };
  const read = async (source: Presentation, guard: () => boolean) => {
    const output = await client.query(
      client.makeQuery("presentation.read", source.presentation_id, {
        generation: source.generation,
        intent_revision: source.intent_revision,
      }),
    );
    const checked = checkView(client, output);
    if (renderKey(identity, checked.presentation) !== renderKey(identity, source))
      throw new Error("原读取回调已不属于当前窗口代数");
    if (guard())
      setState({ surface: checked.surface, presentation: checked.presentation, view: checked });
  };
  const open = () =>
    perform(async (guard) => {
      if (!surface) return;
      const opened = await submit("presentation.open", newID("presentation"), {
        endpoint_id: newID("endpoint"),
        instance_id: newID("instance"),
        surface_ref: surfaceRef(surface) as unknown as JSONValue,
      });
      if (guard()) setState({ surface, presentation: opened });
    }, true);
  const begin = () =>
    perform(async (guard) => {
      if (!presentation) return;
      const started = await submit(
        "presentation.begin",
        presentation.presentation_id,
        { intent_revision: presentation.intent_revision },
        presentation.revision,
      );
      if (guard()) setState({ presentation: started });
      if (has("presentation.read")) await read(started, guard);
    }, true);
  const acknowledge = () =>
    perform(async (guard) => {
      if (!presentation || !view || !bodyReady) return;
      const updated = await submit("presentation.rendered", presentation.presentation_id, {
        generation: presentation.generation,
        intent_revision: presentation.intent_revision,
        surface_ref: presentation.surface_ref as unknown as JSONValue,
        rendered_refs: view.required_refs as unknown as JSONValue,
        render_success: true,
      });
      if (renderKey(identity, updated) !== generation || !updated.presented)
        throw new Error("owner 未确认当前准确呈现代数");
      if (guard()) {
        setState((previous) => ({ ...previous, presentation: updated }));
        setAcknowledged(generation);
        setNotice(
          "当前准确全文已成功呈现，owner 已记录此代数。输入消费、任务效果和费用仍分别核验。",
        );
      }
    });
  const close = () =>
    perform(async (guard) => {
      if (!presentation) return;
      const closed = await submit(
        "presentation.close",
        presentation.presentation_id,
        { reason: "本人关闭当前受信窗口" },
        presentation.revision,
      );
      if (guard()) {
        setState({ presentation: closed });
        setNotice("原窗口关闭决定已提交。任务不会因此取消。");
      }
    }, true);
  return (
    <section className="trusted-request presentation-renderer">
      <div className="panel">
        <h2>受信 Surface 与当前呈现</h2>
        {surface && (
          <p>
            <code>{surface.surface_id}</code> · r{surface.revision} · {surface.state}
          </p>
        )}
        {presentation && (
          <p>
            <code>{presentation.presentation_id}</code> · r{presentation.revision} ·{" "}
            {presentation.state} · 代数 {presentation.generation} · 意图{" "}
            {presentation.intent_revision}
          </p>
        )}
        <div className="trusted-actions">
          {!presentation && surface && (
            <button
              className="button primary"
              type="button"
              disabled={
                !current || running || surface.state !== "open" || !has("presentation.open")
              }
              onClick={() => void open()}
            >
              打开此准确 Surface
            </button>
          )}
          {presentation && (
            <>
              <button
                className="button secondary"
                type="button"
                disabled={!active || running || !has("presentation.begin")}
                onClick={() => void begin()}
              >
                开始新呈现并读取全文
              </button>
              <button
                className="button secondary"
                type="button"
                disabled={
                  !active || running || presentation.generation < 1 || !has("presentation.read")
                }
                onClick={() => void perform((guard) => read(presentation, guard), true)}
              >
                重新读取当前全文
              </button>
              <button
                className="button primary"
                type="button"
                disabled={
                  !active ||
                  running ||
                  !bodyReady ||
                  acknowledged === generation ||
                  !has("presentation.rendered")
                }
                onClick={() => void acknowledge()}
              >
                记录当前准确呈现
              </button>
              <button
                className="button secondary"
                type="button"
                disabled={!active || running || !has("presentation.close")}
                onClick={() => void close()}
              >
                关闭当前窗口
              </button>
            </>
          )}
        </div>
        <p className="field-hint">
          当前窗口只接受 owner 返回的完整准确字节。无当前缓存的 not_modified
          不满足呈现，须重新读取全文。关闭或换页立即废弃旧正文资格。
        </p>
        {error && (
          <p className="notice error" role="alert">
            {error}
          </p>
        )}
        {notice && (
          <p className="notice" role="status">
            {notice}
          </p>
        )}
        {view && (
          <div className="collection">
            <h3>绑定此 Surface 的原输入请求</h3>
            {!view.requests.length && (
              <p className="field-hint">owner 返回的当前 Surface 没有输入块。</p>
            )}
            {view.requests.map((item) => (
              <button
                key={item.request.request_id}
                className="button secondary"
                type="button"
                disabled={
                  !bodyReady ||
                  acknowledged !== generation ||
                  item.method !==
                    (item.request.purpose === "accept_quality"
                      ? "task.accept_result"
                      : "task.input")
                }
                onClick={() => setSelectedRequest(item.request.request_id)}
              >
                {item.request.request_id} · r{item.request.revision} · {item.request.state}
              </button>
            ))}
          </div>
        )}
        {events.length && active && presentation ? (
          events.map((rule) => (
            <FixedApplicationEvent
              key={`${generation}:${rule.name}`}
              client={client}
              rule={rule}
              target={presentation.presentation_id}
              generation={presentation.generation}
              intent={presentation.intent_revision}
              surfaceRef={presentation.surface_ref}
              rendered={bodyReady && acknowledged === generation}
            />
          ))
        ) : (
          <p className="field-hint">
            当前装配没有返回此 Surface 的固定应用事件配置，事件入口不可用。
          </p>
        )}
      </div>
      {view && (
        <TrustedPreview
          refs={refs}
          generation={generation}
          inlineBodies={view.bodies}
          onVerified={setVerified}
        />
      )}
      {picked && view && acknowledged === generation && (
        <TrustedRequest
          key={`${generation}:${picked.request.request_id}:${picked.request.revision}`}
          client={client}
          selection={{
            identity,
            method: "presentation.read",
            value: {
              request: picked.request as unknown as JSONValue,
              request_ref: requestRef(picked.request) as unknown as JSONValue,
              answer_schema: picked.answer_schema,
            },
          }}
          presentation={{
            generation,
            rendered: bodyReady && acknowledged === generation,
            bodies: view.bodies,
          }}
          publish={publish}
          onDone={onDone}
        />
      )}
    </section>
  );
}
