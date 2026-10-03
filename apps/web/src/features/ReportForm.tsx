import { useState } from "react";
import type { Amount } from "@harness/sdk";
export interface ReportInput {
  kind: "report";
  title: string;
  body: string;
  save_path: string;
}
export function ReportForm({
  available,
  onCreate,
  status = "",
  error = "",
  budget,
  deadlineSeconds,
}: {
  available: boolean;
  onCreate?: (input: ReportInput) => Promise<void>;
  status?: string;
  error?: string;
  budget?: readonly Amount[];
  deadlineSeconds?: number;
}) {
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [path, setPath] = useState("reports/result.md");
  const [running, setRunning] = useState(false);
  const submit = async () => {
    if (!onCreate || !available) return;
    setRunning(true);
    try {
      await onCreate({ kind: "report", title, body, save_path: path });
    } finally {
      setRunning(false);
    }
  };
  return (
    <form
      id="new-goal"
      className="goal-form"
      onSubmit={(event) => {
        event.preventDefault();
        void submit();
      }}
    >
      <h2>新建目标</h2>
      <div className="field">
        <label htmlFor="report-title">报告标题</label>
        <input
          id="report-title"
          value={title}
          maxLength={200}
          required
          onChange={(event) => setTitle(event.target.value)}
          placeholder="需要核验的报告名称"
        />
      </div>
      <div className="field">
        <label htmlFor="goal-body">准确目标正文</label>
        <textarea
          id="goal-body"
          rows={4}
          maxLength={65536}
          value={body}
          required
          onChange={(event) => setBody(event.target.value)}
          placeholder="描述需要核验的成果"
        />
      </div>
      <div className="field">
        <label htmlFor="output-file">输出文件</label>
        <input
          id="output-file"
          value={path}
          maxLength={4096}
          required
          onChange={(event) => setPath(event.target.value)}
        />
        <span className="field-hint">
          报告模板将发布准确正文，并通过受管文件写入与独立读回取得条件依据。
        </span>
      </div>
      <div className="field">
        <span className="field-label">任务策略（Task policy）</span>
        <p className="fixed-policy">
          {available ? "原服务显式开发配置 · 固定版本与摘要" : "等待原服务的准确配置"}
        </p>
      </div>
      {budget && (
        <p className="field-hint">
          固定预算上限：{budget.map((amount) => `${amount.unit} ${amount.value}`).join("、")}。
          {deadlineSeconds ? `任务领域截止为提交后 ${deadlineSeconds} 秒。` : ""}
        </p>
      )}
      <button className="button primary" type="submit" disabled={!available || running}>
        {running ? "沿原投递推进…" : "保存并提交"}
      </button>
      {!available && (
        <p className="field-hint">
          当前未开放此报告入口。可通过已发现的准确公开方法提交；不自动近似自由文本。
        </p>
      )}
      {status && (
        <p className="notice" role="status">
          {status}
        </p>
      )}
      {error && (
        <p className="notice error" role="alert">
          {error}
        </p>
      )}
    </form>
  );
}
