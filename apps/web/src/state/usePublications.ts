import { useCallback, useEffect, useRef, useState } from "react";
import {
  getJSON,
  endpoint,
  isObject,
  jsonBytes,
  IndexedDBPublications,
  preparePublication,
  publishOriginal,
  validateRecord,
  validateSchema,
} from "@harness/sdk";
import type {
  Amount,
  ComponentRef,
  ContentRef,
  HarnessClient,
  JSONValue,
  Receipt,
  Schema,
} from "@harness/sdk";
import type { ReportInput } from "../features/ReportForm";
export interface DevelopmentConfig {
  tenant_id: string;
  content_policy_ref: ComponentRef;
  task_policy_ref: ComponentRef;
  budget: Amount[];
  goal_schema: Schema;
  retention_seconds: number;
  task_deadline_seconds: number;
}
function configView(value: JSONValue): DevelopmentConfig {
  if (
    !isObject(value) ||
    typeof value.tenant_id !== "string" ||
    !Number.isSafeInteger(value.retention_seconds) ||
    typeof value.retention_seconds !== "number" ||
    value.retention_seconds < 60 ||
    value.retention_seconds > 31536000 ||
    !Number.isSafeInteger(value.task_deadline_seconds) ||
    typeof value.task_deadline_seconds !== "number" ||
    value.task_deadline_seconds < 1 ||
    value.task_deadline_seconds > 31536000 ||
    !Array.isArray(value.budget) ||
    value.budget.length < 1 ||
    value.budget.length > 100 ||
    !isObject(value.goal_schema)
  )
    throw new Error("开发配置缺少准确策略、预算或有界期限");
  validateRecord("Id", value.tenant_id);
  validateRecord("ComponentRef", value.content_policy_ref);
  validateRecord("ComponentRef", value.task_policy_ref);
  for (const amount of value.budget) validateRecord("Amount", amount);
  return value as unknown as DevelopmentConfig;
}
export function usePublications(
  client: HarnessClient | undefined,
  onGoal: (receipt: Receipt | undefined, ref: ContentRef) => void,
) {
  const [config, setConfig] = useState<{ identity: string; value: DevelopmentConfig }>();
  const [status, setStatus] = useState("");
  const [error, setError] = useState("");
  const [pending, setPending] = useState(0);
  const identity = client
    ? `${client.registry.discovery.identity_scope}:${client.registry.discovery.identity_revision}`
    : "";
  const [store, setStore] = useState<IndexedDBPublications>();
  const callback = useRef(onGoal);
  callback.current = onGoal;
  const identityRef = useRef(identity);
  identityRef.current = identity;
  useEffect(() => {
    let active = true;
    setError("");
    setStatus("");
    setConfig(undefined);
    if (!client) {
      setStore(undefined);
      return;
    }
    const next = new IndexedDBPublications(client.registry.discovery.identity_scope);
    setStore(next);
    void getJSON(fetch, endpoint(new URL(location.origin), "/api/development/config"), 262144)
      .then((value) => {
        if (active) setConfig({ identity, value: configView(value) });
      })
      .catch(() => {
        if (active) setConfig(undefined);
      });
    void next
      .pending()
      .then((items) => {
        if (active) setPending(items.length);
      })
      .catch((failure: unknown) => {
        if (active)
          setError(failure instanceof Error ? failure.message : "浏览器耐久出版账本不可用");
      });
    return () => {
      active = false;
      void next.close();
    };
  }, [client, identity]);
  const current = config?.identity === identity ? config.value : undefined;
  const publishRaw = useCallback(
    async (
      bytes: Uint8Array,
      mediaType: string,
      next?: (ref: ContentRef) => ReturnType<HarnessClient["makeCommand"]>,
    ) => {
      if (!client || !store || !current) throw new Error("当前未开放准确出版配置");
      const originalIdentity = identity;
      setError("");
      setStatus("");
      const now = Date.now();
      const expiration = new Date(now + 60000).toISOString();
      const intent = await preparePublication(client, {
        bytes,
        tenantID: current.tenant_id,
        mediaType,
        policyRef: current.content_policy_ref,
        retentionUntil: new Date(now + current.retention_seconds * 1000).toISOString(),
        transferDeadline: new Date(
          now + Math.min(120, current.retention_seconds) * 1000,
        ).toISOString(),
        expiresAt: expiration,
        ...(next ? { next } : {}),
      });
      try {
        const result = await publishOriginal(client, store, intent, {
          onStage: (stage) => {
            if (identityRef.current === originalIdentity) setStatus(stage);
          },
        });
        if (identityRef.current === originalIdentity) {
          setPending((await store.pending()).length);
          if (identityRef.current === originalIdentity)
            callback.current(result.receipt, result.content_ref);
        }
        return result;
      } catch (failure) {
        if (identityRef.current === originalIdentity) {
          setError(failure instanceof Error ? failure.message : "原出版责任未结，沿原身份恢复");
          setPending((await store.pending()).length);
        }
        throw failure;
      }
    },
    [client, current, store, identity],
  );
  const publish = useCallback(
    (value: JSONValue, next?: (ref: ContentRef) => ReturnType<HarnessClient["makeCommand"]>) =>
      publishRaw(jsonBytes(value), "application/json", next),
    [publishRaw],
  );
  const submitReport = useCallback(
    async (input: ReportInput) => {
      if (!client || !current) return;
      const originalIdentity = identity;
      try {
        validateSchema(current.goal_schema, input);
        const now = Date.now();
        const taskDeadline = new Date(now + current.task_deadline_seconds * 1000).toISOString();
        const expiresAt = new Date(now + 60000).toISOString();
        await publish(input as unknown as JSONValue, (ref) =>
          client.makeCommand(
            "task.submit",
            client.registry.discovery.logical_service_id,
            {
              orchestrator_id: client.registry.discovery.logical_service_id,
              goal_ref: ref as unknown as JSONValue,
              policy_ref: current.task_policy_ref as unknown as JSONValue,
              budget: current.budget as unknown as JSONValue,
              deadline: taskDeadline,
            },
            expiresAt,
          ),
        );
      } catch (failure) {
        if (identityRef.current !== originalIdentity) return;
        setError(failure instanceof Error ? failure.message : "原报告提交未取得决定");
      }
    },
    [client, current, publish, identity],
  );
  const recover = useCallback(async () => {
    if (!client || !store) return;
    const originalIdentity = identity;
    setError("");
    for (const intent of await store.pending()) {
      if (identityRef.current !== originalIdentity) return;
      try {
        const result = await publishOriginal(client, store, intent, {
          onStage: (stage) => {
            if (identityRef.current === originalIdentity) setStatus(stage);
          },
        });
        if (identityRef.current === originalIdentity)
          callback.current(result.receipt, result.content_ref);
      } catch (failure) {
        if (identityRef.current !== originalIdentity) return;
        setError(failure instanceof Error ? failure.message : "原出版当前不能核验");
      }
    }
    if (identityRef.current === originalIdentity) setPending((await store.pending()).length);
  }, [client, store, identity]);
  return {
    config: current,
    available:
      !!current &&
      !!client?.registry.discovery.methods.some((entry) => entry.name === "task.submit") &&
      !!client.registry.discovery.methods.some(
        (entry) => entry.name === "content.upload_reserve",
      ) &&
      !!client.registry.discovery.methods.some((entry) => entry.name === "content.put"),
    status,
    error,
    pending,
    publish,
    publishRaw,
    submitReport,
    recover,
  };
}
