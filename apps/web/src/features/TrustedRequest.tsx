import { useEffect, useState } from "react";
import {
  canonical,
  digest,
  isObject,
  parseStrict,
  validateRecord,
  validateSchema,
} from "@harness/sdk";
import type {
  ContentRef,
  HarnessClient,
  InputRequest,
  JSONValue,
  ObjectRef,
  Receipt,
  Schema,
} from "@harness/sdk";
import { RestrictedForm, initialPayload } from "../components/RestrictedForm";
import { TrustedPreview } from "../components/TrustedPreview";
import { contentRefs } from "./TaskInspector";
export interface TrustedSelection {
  identity: string;
  method: string;
  value: JSONValue;
}
export function TrustedRequest({
  client,
  selection,
  publish,
  onDone,
}: {
  client: HarnessClient;
  selection: TrustedSelection;
  publish: (
    value: JSONValue,
    next: (ref: ContentRef) => ReturnType<HarnessClient["makeCommand"]>,
  ) => Promise<{ content_ref: ContentRef; receipt?: Receipt }>;
  onDone: () => void;
}) {
  const confirmation =
    selection.method === "confirmation.read" &&
    isObject(selection.value) &&
    typeof selection.value.original_command_id === "string" &&
    typeof selection.value.intent_hash === "string"
      ? selection.value
      : undefined;
  const inputView =
    ["input_request.read", "task.input_requests.list"].includes(selection.method) &&
    isObject(selection.value) &&
    isObject(selection.value.request) &&
    isObject(selection.value.answer_schema)
      ? selection.value
      : undefined;
  let request: InputRequest | undefined;
  let requestRef: ObjectRef | undefined;
  let invalidRequest = false;
  try {
    if (inputView) {
      request = validateRecord<InputRequest>("InputRequest", inputView.request);
      requestRef = validateRecord<ObjectRef>("ObjectRef", inputView.request_ref);
    }
  } catch {
    invalidRequest = true;
  }
  const requestID =
    confirmation && typeof confirmation.request_id === "string"
      ? confirmation.request_id
      : request?.request_id;
  const revision =
    confirmation && typeof confirmation.revision === "number"
      ? confirmation.revision
      : request?.revision;
  const state =
    confirmation && typeof confirmation.state === "string" ? confirmation.state : request?.state;
  const expiresAt =
    confirmation && typeof confirmation.expires_at === "string"
      ? confirmation.expires_at
      : request?.expires_at;
  const key = `${selection.identity}:${requestID}:${revision}:${confirmation?.intent_hash ?? request?.goal_revision ?? ""}`;
  const [verified, setVerified] = useState<string>();
  const [original, setOriginal] = useState<{ key: string; value: JSONValue }>();
  const [answer, setAnswer] = useState<JSONValue>({});
  const [valid, setValid] = useState(true);
  const [error, setError] = useState("");
  const [running, setRunning] = useState(false);
  const [decided, setDecided] = useState("");
  const [outcome, setOutcome] = useState("");
  const [refs, setRefs] = useState<ContentRef[]>([]);
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    let active = true;
    setVerified(undefined);
    setOriginal(undefined);
    setError("");
    setOutcome("");
    setDecided("");
    setRunning(false);
    setValid(true);
    try {
      const needed =
        confirmation && Array.isArray(confirmation.preview_refs)
          ? confirmation.preview_refs.map((ref) => validateRecord<ContentRef>("ContentRef", ref))
          : contentRefs(request as unknown as JSONValue);
      setRefs(needed);
      if (inputView) setAnswer(initialPayload(inputView.answer_schema));
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "原受信表单不可呈现");
      setRefs([]);
      setValid(false);
    }
    if (confirmation) {
      void (async () => {
        let command: JSONValue;
        if (confirmation.original_command) command = confirmation.original_command;
        else {
          const stored = await client.store.get(
            client.registry.discovery.logical_service_id,
            String(confirmation.original_command_id),
          );
          if (!stored)
            throw new Error("本浏览器没有完整原命令，原 owner 未提供其准确正文；不能批准");
          command = parseStrict(stored.command_json);
        }
        if (
          !isObject(command) ||
          command.command_id !== confirmation.original_command_id ||
          command.logical_service_id !== client.registry.discovery.logical_service_id ||
          (await digest(command)) !== confirmation.intent_hash
        )
          throw new Error("原命令身份或准确摘要不符，不能批准");
        if (active) setOriginal({ key, value: command });
      })().catch((failure: unknown) => {
        if (active) setError(failure instanceof Error ? failure.message : "完整原命令不可核验");
      });
    }
    return () => {
      active = false;
    };
  }, [client, key, confirmation, inputView, request]);
  if (invalidRequest)
    return <p className="notice error">原输入请求或引用不符合准确同版合同，不能提交。</p>;
  if (
    !requestID ||
    !revision ||
    !expiresAt ||
    (!confirmation && (!request || !requestRef || !inputView))
  )
    return null;
  const currentIdentity = `${client.registry.discovery.identity_scope}:${client.registry.discovery.identity_revision}`;
  const current = selection.identity === currentIdentity;
  const previewKey = `${key}:${canonical(refs)}`;
  const bodyReady = refs.length > 0 && verified === previewKey;
  const allowed =
    current && state === "pending" && Date.parse(expiresAt) > now && decided !== key && !running;
  const expires = () => new Date(Math.min(Date.now() + 60000, Date.parse(expiresAt))).toISOString();
  const decide = async (decision: "approved" | "denied") => {
    if (
      !confirmation ||
      !allowed ||
      (decision === "approved" && (!bodyReady || original?.key !== key))
    )
      return;
    setRunning(true);
    setError("");
    try {
      const receipt = await client.command(
        client.makeCommand(
          "confirmation.decide",
          requestID,
          {
            request_id: requestID,
            request_revision: revision,
            decision,
            challenge: confirmation.challenge ?? "",
            preview_refs: refs as unknown as JSONValue,
          },
          expires(),
        ),
      );
      if (receipt.stage === "rejected")
        throw new Error(
          receipt.error ? `${receipt.error.code}: ${receipt.error.reason}` : "原确认被拒绝",
        );
      setDecided(key);
      setOutcome("本人决定已提交。原业务仍须重新核验并一次消费，效果与费用尚未因此成立。");
      onDone();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "原确认决定未核验");
    } finally {
      setRunning(false);
    }
  };
  const submit = async () => {
    if (!request || !requestRef || !inputView || !allowed || !bodyReady || !valid) return;
    const boundRequest = request;
    const boundRef = requestRef;
    setRunning(true);
    setError("");
    try {
      let receipt: Receipt | undefined;
      if (boundRequest.purpose === "accept_quality") {
        if (
          !boundRequest.candidate_ref ||
          !boundRequest.limitations_ref ||
          !boundRequest.goal_revision
        )
          throw new Error("原成果验收请求缺少准确成果、限制或目标修订");
        receipt = await client.command(
          client.makeCommand(
            "task.accept_result",
            boundRequest.target_ref.object_id,
            {
              task_id: boundRequest.target_ref.object_id,
              request_ref: boundRef as unknown as JSONValue,
              goal_revision: boundRequest.goal_revision,
              candidate_ref: boundRequest.candidate_ref as unknown as JSONValue,
              limitations_ref: boundRequest.limitations_ref as unknown as JSONValue,
            },
            expires(),
          ),
        );
      } else {
        validateSchema(inputView.answer_schema, answer);
        if (!boundRequest.goal_revision)
          throw new Error("此业务请求未提供 Task 目标版本，不能虚构 Task 型消费");
        const result = await publish(answer, (ref) =>
          client.makeCommand(
            "task.input",
            boundRequest.target_ref.object_id,
            {
              task_id: boundRequest.target_ref.object_id,
              request_ref: boundRef as unknown as JSONValue,
              goal_revision: boundRequest.goal_revision ?? 0,
              answer_ref: ref as unknown as JSONValue,
            },
            expires(),
          ),
        );
        receipt = result.receipt;
      }
      if (receipt?.stage === "rejected")
        throw new Error(
          receipt.error ? `${receipt.error.code}: ${receipt.error.reason}` : "原消费拒绝",
        );
      setDecided(key);
      setOutcome(
        receipt?.stage === "accepted"
          ? "准确回答已进入原准备，仍等待原业务一次消费决定。"
          : "原业务消费决定已提交；Task 完成与交付继续独立核验。",
      );
      onDone();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "原输入责任未结");
    } finally {
      setRunning(false);
    }
  };
  return (
    <section className="trusted-request">
      <div className="panel">
        <h2>{confirmation ? "本人确认 · 精确原命令" : "原输入请求 · 精确版本"}</h2>
        <div className="request-facts">
          <p>
            <code>{requestID}</code> · r{revision} · {state}
          </p>
          <p className="field-hint">
            固定截止 {expiresAt}。改版、过期或权限变化由原 owner
            拒绝；本界面不自动改写答案或请求版本。
          </p>
        </div>
        {confirmation && original?.key === key && (
          <details open>
            <summary>完整准确原命令 · 摘要已核对</summary>
            <pre>{JSON.stringify(original.value, null, 2)}</pre>
          </details>
        )}
        {inputView && request?.purpose !== "accept_quality" && (
          <RestrictedForm
            schema={inputView.answer_schema as Schema}
            value={answer}
            onChange={setAnswer}
            onValidity={setValid}
          />
        )}
        {error && (
          <p className="notice error" role="alert">
            {error}
          </p>
        )}
        {outcome && (
          <p className="notice" role="status">
            {outcome}
          </p>
        )}
      </div>
      <TrustedPreview refs={refs} generation={key} onVerified={setVerified} />
      <div className="trusted-actions">
        {confirmation ? (
          <>
            <button
              className="button secondary"
              type="button"
              disabled={!allowed}
              onClick={() => void decide("denied")}
            >
              拒绝原命令
            </button>
            <button
              className="button primary"
              type="button"
              disabled={!allowed || !bodyReady || original?.key !== key}
              onClick={() => void decide("approved")}
            >
              确认准确原命令
            </button>
          </>
        ) : (
          <button
            className="button primary"
            type="button"
            disabled={!allowed || !bodyReady || !valid}
            onClick={() => void submit()}
          >
            {request?.purpose === "accept_quality" ? "确认此准确成果及限制" : "保存准确回答并提交"}
          </button>
        )}
        <span className="field-hint">
          {bodyReady
            ? "必需全文已准确呈现，业务 owner 仍会重新校验。"
            : "完整必需正文尚未成功呈现，依赖按钮保持关闭。"}
        </span>
      </div>
    </section>
  );
}
