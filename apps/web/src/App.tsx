import { useMemo, useRef, useState } from "react";
import { isObject } from "@harness/sdk";
import type { ContentRef, JSONValue } from "@harness/sdk";
import { Shell } from "./components/Shell";
import type { NavigationID } from "./components/Shell";
import { TrustedPreview } from "./components/TrustedPreview";
import { CollectionView, recordID } from "./features/CollectionView";
import { ManagementView } from "./features/ManagementView";
import { MethodConsole } from "./features/MethodConsole";
import { RecoveryPanel } from "./features/RecoveryPanel";
import { ReportForm } from "./features/ReportForm";
import { TaskInspector } from "./features/TaskInspector";
import { TrustedRequest } from "./features/TrustedRequest";
import { PresentationRenderer } from "./features/PresentationRenderer";
import { ContentPublisher } from "./features/ContentPublisher";
import type { TrustedSelection } from "./features/TrustedRequest";
import { initialPayload } from "./components/RestrictedForm";
import { useHarness } from "./state/useHarness";
import { usePublications } from "./state/usePublications";
function Login({
  login,
  loading,
  error,
}: {
  login: (token: string) => Promise<void>;
  loading: boolean;
  error: string;
}) {
  const [token, setToken] = useState("");
  const [failure, setFailure] = useState("");
  const [pending, setPending] = useState(false);
  return (
    <section className="panel login-panel">
      <h2>连接原服务</h2>
      <p>认证成功后读取精确方法、Schema 与固定 owner。凭据只用于同源登录，不写入浏览器持久账本。</p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          setPending(true);
          setFailure("");
          void login(token)
            .then(() => setToken(""))
            .catch((reason: unknown) =>
              setFailure(reason instanceof Error ? reason.message : "认证未确认"),
            )
            .finally(() => setPending(false));
        }}
      >
        <div className="field">
          <label htmlFor="login-token">开发凭据</label>
          <input
            id="login-token"
            type="password"
            value={token}
            autoComplete="off"
            required
            onChange={(event) => setToken(event.target.value)}
          />
        </div>
        <button className="button primary" type="submit" disabled={loading || pending}>
          {pending || loading ? "连接中…" : "认证并连接"}
        </button>
      </form>
      {(failure || error) && (
        <p className="notice error" role="alert">
          {failure || error}
        </p>
      )}
    </section>
  );
}
export function App() {
  const harness = useHarness();
  const { client } = harness;
  const [area, setArea] = useState<NavigationID>("work");
  const [selected, setSelected] = useState<{ identity: string; value: JSONValue }>();
  const [preview, setPreview] = useState<ContentRef[]>([]);
  const [taskError, setTaskError] = useState("");
  const [refresh, setRefresh] = useState(0);
  const [trusted, setTrusted] = useState<TrustedSelection>();
  const [presentation, setPresentation] = useState<TrustedSelection>();
  const selectionGeneration = useRef(0);
  const [control, setControl] = useState<{
    method: string;
    target: string;
    revision?: number;
    payload: JSONValue;
  }>();
  const taskMethods = useMemo(
    () =>
      client?.registry.discovery.methods.filter((entry) => entry.name.startsWith("task.")) ?? [],
    [client],
  );
  const identity = client
    ? `${client.registry.discovery.identity_scope}:${client.registry.discovery.identity_revision}`
    : "unauthenticated";
  const task = selected?.identity === identity ? selected.value : undefined;
  const identityRef = useRef(identity);
  identityRef.current = identity;
  const contextGeneration = selectionGeneration.current;
  const taskID = task ? recordID(task) : "";
  const inputQuery = useMemo<JSONValue | undefined>(
    () => (taskID ? { task_id: taskID, limit: 20 } : undefined),
    [taskID],
  );
  const publications = usePublications(client, (receipt, ref) => {
    if (contextGeneration !== selectionGeneration.current || identityRef.current !== identity)
      return;
    setPreview([ref]);
    setRefresh((value) => value + 1);
    if (
      receipt?.stage === "applied" &&
      isObject(receipt.output) &&
      isObject(receipt.output.task_ref)
    ) {
      void selectTask(receipt.output.task_ref);
    }
  });
  const selectTask = async (value: JSONValue) => {
    if (!client) return;
    if (task && recordID(task) !== recordID(value)) setTrusted(undefined);
    const originalIdentity = identity;
    const generation = ++selectionGeneration.current;
    setTaskError("");
    setSelected({ identity: originalIdentity, value });
    setControl(undefined);
    setPreview([]);
    const method = client.registry.discovery.methods.find((entry) => entry.name === "task.read");
    if (method) {
      try {
        const input = initialPayload(method.input_schema);
        if (
          isObject(input) &&
          isObject(method.input_schema.properties) &&
          Object.hasOwn(method.input_schema.properties, "task_id")
        )
          input.task_id = recordID(value);
        const response = await client.query(client.makeQuery(method.name, recordID(value), input));
        let view: JSONValue = response;
        const resultMethod = client.registry.discovery.methods.find(
          (entry) => entry.name === "task.result",
        );
        if (isObject(response) && isObject(response.result_ref) && resultMethod) {
          const result = await client.query(
            client.makeQuery(resultMethod.name, recordID(value), {
              result_ref: response.result_ref,
            }),
          );
          if (isObject(result)) view = { task: response, ...result };
        }
        if (identityRef.current === originalIdentity && selectionGeneration.current === generation)
          setSelected({ identity: originalIdentity, value: view });
      } catch (failure) {
        if (identityRef.current !== originalIdentity || selectionGeneration.current !== generation)
          return;
        setTaskError(
          failure instanceof Error
            ? `Task 详情当前不可核验：${failure.message}`
            : "Task 详情当前不可核验",
        );
      }
    }
  };
  const showRequest = async (method: string, value: JSONValue) => {
    if (!client) return;
    const originalIdentity = identity;
    const generation = selectionGeneration.current;
    if (method === "session.branch.select" && isObject(value) && value.stage === "applied") {
      selectionGeneration.current++;
      setTrusted(undefined);
      setPreview([]);
      setPresentation(undefined);
      return;
    }
    if (method.startsWith("surface.") || method.startsWith("presentation.")) {
      setPresentation({ identity: originalIdentity, method, value });
      setTrusted(undefined);
      setPreview([]);
      return;
    }
    if (["confirmation.read", "input_request.read", "task.input_requests.list"].includes(method)) {
      setTrusted({ identity: originalIdentity, method, value });
      return;
    }
    if (
      isObject(value) &&
      value.stage === "accepted" &&
      isObject(value.output) &&
      isObject(value.output.confirmation_ref) &&
      typeof value.output.confirmation_ref.object_id === "string" &&
      client.registry.discovery.methods.some((entry) => entry.name === "confirmation.read")
    ) {
      try {
        const id = value.output.confirmation_ref.object_id;
        const result = await client.query(client.makeQuery("confirmation.read", id, { id }));
        if (identityRef.current === originalIdentity && generation === selectionGeneration.current)
          setTrusted({ identity: originalIdentity, method: "confirmation.read", value: result });
      } catch (failure) {
        if (identityRef.current === originalIdentity && generation === selectionGeneration.current)
          setTaskError(failure instanceof Error ? failure.message : "原本人确认当前不能读取");
      }
    }
  };
  const showControl = (methodName: string, target: string, revision?: number) => {
    const method = taskMethods.find((entry) => entry.name === methodName);
    if (!method) return;
    let payload: JSONValue;
    try {
      payload = initialPayload(method.input_schema);
    } catch {
      payload = {};
    }
    if (isObject(payload) && isObject(method.input_schema.properties)) {
      if (Object.hasOwn(method.input_schema.properties, "task_id")) payload.task_id = target;
      if (Object.hasOwn(method.input_schema.properties, "reason"))
        payload.reason = "本人通过受信界面提出原任务控制";
    }
    setControl({ method: methodName, target, ...(revision ? { revision } : {}), payload });
  };
  const navigate = (next: NavigationID) => {
    setArea(next);
    setPreview([]);
    setControl(undefined);
    setTrusted(undefined);
    setPresentation(undefined);
    selectionGeneration.current++;
  };
  return (
    <Shell
      selected={area}
      onSelect={navigate}
      connection={harness.connection}
      onReconnect={() => {
        selectionGeneration.current++;
        setTrusted(undefined);
        setPresentation(undefined);
        void harness.reconnect();
      }}
      onDisconnect={() => {
        selectionGeneration.current++;
        setPreview([]);
        setSelected(undefined);
        setTrusted(undefined);
        setPresentation(undefined);
        void harness.disconnect();
      }}
      onLogout={() => {
        selectionGeneration.current++;
        setPreview([]);
        setSelected(undefined);
        setTrusted(undefined);
        setPresentation(undefined);
        void harness.logout();
      }}
    >
      {!client ? (
        <Login login={harness.login} loading={harness.auth === "loading"} error={harness.error} />
      ) : (
        <>
          <RecoveryPanel
            key={identity}
            client={client}
            onRecovered={() => setRefresh((value) => value + 1)}
            onClearContent={publications.clearCompleted}
          />
          {area === "work" ? (
            <>
              <div className="workspace-grid">
                <section className="panel primary-panel">
                  <ReportForm
                    available={publications.available}
                    onCreate={publications.submitReport}
                    status={publications.status}
                    error={publications.error}
                    {...(publications.config
                      ? {
                          budget: publications.config.budget,
                          deadlineSeconds: publications.config.task_deadline_seconds,
                        }
                      : {})}
                  />
                  {publications.pending > 0 && (
                    <p className="notice">
                      准确正文出版仍有 {publications.pending} 项原责任。
                      <button
                        type="button"
                        className="text-button"
                        onClick={() => void publications.recover()}
                      >
                        恢复原出版与后续命令
                      </button>
                    </p>
                  )}
                  <div className="section-divider" />
                  <CollectionView
                    client={client}
                    methodName="task.list"
                    title="任务列表"
                    refreshKey={refresh}
                    onSelect={(value) => void selectTask(value)}
                  />
                </section>
                <TaskInspector
                  client={client}
                  selected={task}
                  onControl={showControl}
                  onPreview={setPreview}
                  readError={taskError}
                />
              </div>
              {taskID &&
                inputQuery &&
                client.registry.discovery.methods.some(
                  (entry) => entry.name === "task.input_requests.list",
                ) && (
                  <section className="panel task-inputs">
                    <CollectionView
                      client={client}
                      methodName="task.input_requests.list"
                      title="此任务的原输入请求"
                      targetID={taskID}
                      input={inputQuery}
                      refreshKey={refresh}
                      onSelect={(value) => void showRequest("task.input_requests.list", value)}
                    />
                  </section>
                )}
              {control ? (
                <MethodConsole
                  key={`${control.method}:${control.target}`}
                  client={client}
                  methods={taskMethods.filter((entry) => entry.name === control.method)}
                  preferred={control.method}
                  preset={control}
                  onResult={() => {
                    if (
                      contextGeneration !== selectionGeneration.current ||
                      identityRef.current !== identity
                    )
                      return;
                    setRefresh((value) => value + 1);
                    if (task) void selectTask(task);
                  }}
                />
              ) : (
                <details className="advanced">
                  <summary>准确公开方法</summary>
                  <MethodConsole
                    client={client}
                    methods={taskMethods}
                    onResult={(method, value) => {
                      if (
                        contextGeneration !== selectionGeneration.current ||
                        identityRef.current !== identity
                      )
                        return;
                      setRefresh((item) => item + 1);
                      void showRequest(method, value as JSONValue);
                    }}
                  />
                </details>
              )}
            </>
          ) : (
            <>
              {area === "memory" && (
                <ContentPublisher
                  key={identity}
                  available={!!publications.config}
                  publish={publications.publishRaw}
                  status={publications.status}
                />
              )}
              <ManagementView
                key={`${area}:${identity}`}
                client={client}
                area={area}
                onPreview={setPreview}
                onRequest={(method, value) => {
                  if (
                    contextGeneration !== selectionGeneration.current ||
                    identityRef.current !== identity
                  )
                    return;
                  void showRequest(method, value);
                }}
              />
            </>
          )}
          {trusted?.identity === identity && (
            <TrustedRequest
              key={`${trusted.identity}:${trusted.method}:${recordID(trusted.value)}`}
              client={client}
              selection={trusted}
              publish={publications.publish}
              onDone={() => {
                if (
                  contextGeneration !== selectionGeneration.current ||
                  identityRef.current !== identity
                )
                  return;
                setRefresh((value) => value + 1);
                if (task) void selectTask(task);
              }}
            />
          )}
          {presentation?.identity === identity && (
            <PresentationRenderer
              key={`${presentation.identity}:${area}`}
              client={client}
              selection={presentation}
              publish={publications.publish}
              {...(publications.config ? { development: publications.config } : {})}
              onDone={() => setRefresh((value) => value + 1)}
            />
          )}
        </>
      )}
      <TrustedPreview refs={client ? preview : []} generation={`${identity}:${area}`} />
    </Shell>
  );
}
