import { useState } from "react";
import { parseStrict } from "@harness/sdk";
import type { ContentRef, Receipt } from "@harness/sdk";
const media = ["text/plain", "text/markdown", "application/json", "image/png", "image/jpeg"];
export function ContentPublisher({
  available,
  publish,
  status,
}: {
  available: boolean;
  publish: (
    bytes: Uint8Array,
    mediaType: string,
  ) => Promise<{ content_ref: ContentRef; receipt?: Receipt }>;
  status: string;
}) {
  const [text, setText] = useState("");
  const [mediaType, setMediaType] = useState("text/plain");
  const [file, setFile] = useState<File>();
  const [ref, setRef] = useState<ContentRef>();
  const [error, setError] = useState("");
  const [running, setRunning] = useState(false);
  const send = async () => {
    if (!available || running) return;
    setRunning(true);
    setError("");
    try {
      if (file && file.size > 262144) throw new Error("此浏览器出版入口最多支持 256 KiB 准确正文");
      if (!file && mediaType.startsWith("image/")) throw new Error("图像需要选择原文件");
      const bytes = file
        ? new Uint8Array(await file.arrayBuffer())
        : new TextEncoder().encode(text);
      if (!bytes.byteLength || bytes.byteLength > 262144)
        throw new Error("正文须在 1 字节至 256 KiB 之间");
      if (mediaType === "application/json") parseStrict(bytes);
      const result = await publish(bytes, mediaType);
      setRef(result.content_ref);
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "原正文尚未取得出版决定");
    } finally {
      setRunning(false);
    }
  };
  return (
    <section className="panel content-publisher">
      <h2>发布准确正文</h2>
      <p className="field-hint">
        原字节先保存，再上传并核验发布决定。正文发布后可用于任务、回答或记忆；本入口上限 256 KiB。
      </p>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void send();
        }}
      >
        <div className="field">
          <label htmlFor="content-media">媒体类型</label>
          <select
            id="content-media"
            value={mediaType}
            onChange={(event) => setMediaType(event.target.value)}
          >
            {media.map((type) => (
              <option key={type} value={type}>
                {type}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="content-original">准确原文</label>
          <textarea
            id="content-original"
            rows={4}
            value={text}
            maxLength={65536}
            disabled={!!file}
            onChange={(event) => setText(event.target.value)}
          />
        </div>
        <div className="field">
          <label htmlFor="content-file">或选择原文件</label>
          <input
            id="content-file"
            type="file"
            accept=".txt,.md,.json,.png,.jpg,.jpeg"
            onChange={(event) => {
              const chosen = event.target.files?.[0];
              setFile(chosen);
              if (chosen && media.includes(chosen.type)) setMediaType(chosen.type);
            }}
          />
        </div>
        <button className="button primary" type="submit" disabled={!available || running}>
          {running ? "沿原出版推进…" : "保存并发布正文"}
        </button>
        {!available && <p className="notice">当前服务未提供此入口的准确出版策略。</p>}
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
        {ref && (
          <details open>
            <summary>已发布的准确引用</summary>
            <pre>{JSON.stringify(ref, null, 2)}</pre>
          </details>
        )}
      </form>
    </section>
  );
}
