import { useEffect, useState } from "react";
import type { HarnessClient, RecoveryResult, StoredCommand } from "@harness/sdk";
export function RecoveryPanel({
  client,
  onRecovered,
}: {
  client: HarnessClient;
  onRecovered: () => void;
}) {
  const [pending, setPending] = useState<StoredCommand[]>([]);
  const [results, setResults] = useState<RecoveryResult[]>([]);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    void client
      .pending()
      .then((value) => {
        if (active) setPending(value);
      })
      .catch((failure: unknown) => {
        if (active) setError(failure instanceof Error ? failure.message : "本地耐久账本不可用");
      });
    return () => {
      active = false;
    };
  }, [client]);
  const recover = async () => {
    setRunning(true);
    setError("");
    try {
      setResults(await client.recover());
      setPending(await client.pending());
      onRecovered();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "原投递当前无法核验");
    } finally {
      setRunning(false);
    }
  };
  return (
    <details className="recovery">
      <summary>浏览器原投递责任 · {pending.length} 项未结</summary>
      <p className="field-hint">
        恢复先查原 owner 的命令；只有原权威确定尚无决定时，重传准确原命令。关闭窗口不取消 Task。
      </p>
      <button
        className="button secondary"
        type="button"
        disabled={running}
        onClick={() => void recover()}
      >
        {running ? "沿原命令恢复…" : "恢复原投递"}
      </button>
      {error && (
        <p className="notice error" role="alert">
          {error}
        </p>
      )}
      {pending.map((item) => (
        <p key={item.command_id}>
          <code>{item.command_id}</code> · {item.contract.name}
        </p>
      ))}
      {results.map((result) => (
        <p key={result.command_id}>
          <code>{result.command_id}</code> · {result.receipt?.stage ?? result.error ?? "未结"}
        </p>
      ))}
    </details>
  );
}
