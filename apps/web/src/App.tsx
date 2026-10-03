import { useMemo, useState } from "react";
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
import { initialPayload } from "./components/RestrictedForm";
import { useHarness } from "./state/useHarness";
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
  const [refresh, setRefresh] = useState(0);
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
  const selectTask = async (value: JSONValue) => {
    if (!client) return;
    const originalIdentity = identity;
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
        setSelected({ identity: originalIdentity, value: response });
      } catch {
        /* 列表仍是原服务事实；不可核验的 read 不伪造 newer 记录。 */
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
  };
  return (
    <Shell
      selected={area}
      onSelect={navigate}
      connection={harness.connection}
      onReconnect={() => void harness.reconnect()}
      onDisconnect={() => {
        setPreview([]);
        setSelected(undefined);
        void harness.disconnect();
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
          />
          {area === "work" ? (
            <>
              <div className="workspace-grid">
                <section className="panel primary-panel">
                  <ReportForm available={false} />
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
                />
              </div>
              {control ? (
                <MethodConsole
                  key={`${control.method}:${control.target}`}
                  client={client}
                  methods={taskMethods.filter((entry) => entry.name === control.method)}
                  preferred={control.method}
                  preset={control}
                  onResult={() => {
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
                    onResult={() => setRefresh((value) => value + 1)}
                  />
                </details>
              )}
            </>
          ) : (
            <ManagementView
              key={`${area}:${identity}`}
              client={client}
              area={area}
              onPreview={setPreview}
              onRequest={() => undefined}
            />
          )}
        </>
      )}
      <TrustedPreview refs={client ? preview : []} generation={`${identity}:${area}`} />
    </Shell>
  );
}
