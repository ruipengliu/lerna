import { useEffect, useId, useRef, useState } from "react";
import { canonical, fetchContent } from "@harness/sdk";
import type { ContentRef } from "@harness/sdk";
interface Body {
  ref: ContentRef;
  text?: string;
  image?: string;
}
export function TrustedPreview({
  refs,
  generation,
  onVerified,
}: {
  refs: ContentRef[];
  generation: string;
  onVerified?: (key: string | undefined) => void;
}) {
  const titleID = useId();
  const [state, setState] = useState<{
    key: string;
    bodies: Body[];
    status: "empty" | "loading" | "ready" | "error";
    error: string;
  }>({ key: "", bodies: [], status: "empty", error: "" });
  const [images, setImages] = useState<Set<string>>(new Set());
  const callback = useRef(onVerified);
  callback.current = onVerified;
  const referenceKey = canonical(refs);
  const key = `${generation}:${referenceKey}`;
  useEffect(() => {
    const controller = new AbortController();
    callback.current?.(undefined);
    setImages(new Set());
    if (!refs.length) {
      setState({ key, bodies: [], status: "empty", error: "" });
      return () => controller.abort();
    }
    if (refs.length > 8) {
      setState({
        key,
        bodies: [],
        status: "error",
        error: "必须正文超过此受信界面的有界预览上限；依赖操作保持关闭",
      });
      return () => controller.abort();
    }
    setState({ key, bodies: [], status: "loading", error: "" });
    void Promise.all(
      refs.map(async (ref): Promise<Body> => {
        const bytes = await fetchContent(ref, { signal: controller.signal });
        if (["text/plain", "text/markdown", "application/json"].includes(ref.media_type))
          return { ref, text: new TextDecoder("utf-8", { fatal: true }).decode(bytes) };
        if (["image/png", "image/jpeg"].includes(ref.media_type)) {
          const encoded = btoa(Array.from(bytes, (entry) => String.fromCharCode(entry)).join(""));
          const image = `data:${ref.media_type};base64,${encoded}`;
          return { ref, image };
        }
        throw new Error("该媒体类型尚无受信 Renderer，不能据此确认");
      }),
    )
      .then((bodies) => {
        if (!controller.signal.aborted) setState({ key, bodies, status: "ready", error: "" });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted)
          setState({
            key,
            bodies: [],
            status: "error",
            error: error instanceof Error ? error.message : "正文取回失败",
          });
      });
    return () => {
      controller.abort();
      callback.current?.(undefined);
    };
  }, [key, refs]);
  useEffect(() => {
    const ready =
      state.key === key &&
      state.status === "ready" &&
      state.bodies.every((body) => !body.image || images.has(body.image));
    callback.current?.(ready ? key : undefined);
  }, [key, state, images]);
  const current = state.key === key;
  return (
    <section className="panel preview-panel" aria-labelledby={titleID}>
      <h2 id={titleID}>准确正文预览</h2>
      {!refs.length ? (
        <div className="preview-empty">选择正文引用后核对准确版本与摘要。</div>
      ) : !current || state.status === "loading" ? (
        <p role="status">正在取得完整正文并校验摘要…</p>
      ) : state.status === "error" ? (
        <p className="notice error" role="alert">
          {state.error}
        </p>
      ) : (
        state.bodies.map((body) => (
          <article
            className="preview-body"
            key={`${body.ref.content_id}:${body.ref.version}:${body.ref.hash}`}
          >
            <div className="preview-meta">
              <span>
                版本 {body.ref.version} · {body.ref.byte_length} 字节
              </span>
              <code>{body.ref.hash}</code>
            </div>
            {body.text !== undefined ? (
              <pre className="exact-body">{body.text}</pre>
            ) : (
              <img
                src={body.image}
                alt="准确版本的完整正文图像"
                onLoad={() => {
                  if (body.image) setImages((previous) => new Set(previous).add(body.image ?? ""));
                }}
                onError={() => {
                  callback.current?.(undefined);
                  setState({
                    key,
                    bodies: [],
                    status: "error",
                    error: "准确字节已取得，但图像未成功呈现；依赖操作保持关闭",
                  });
                }}
              />
            )}
          </article>
        ))
      )}
      {current && state.status === "ready" && (
        <p className="field-hint">
          准确字节与声明摘要一致。预览关联只说明提交引用，不证明用户阅读或理解。
        </p>
      )}
    </section>
  );
}
