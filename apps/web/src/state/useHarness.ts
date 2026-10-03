import { useCallback, useEffect, useRef, useState } from "react";
import { HarnessClient, parseStrict, readBounded, isObject, RequestError } from "@harness/sdk";
import type { ConnectionState } from "@harness/sdk";
export type Authentication = "loading" | "required" | "authenticated";
export function useHarness() {
  const [client, setClient] = useState<HarnessClient>();
  const [connection, setConnection] = useState<ConnectionState>("disconnected");
  const [auth, setAuth] = useState<Authentication>("loading");
  const [error, setError] = useState("");
  const generation = useRef(0);
  const logoutPending = useRef(false);
  const current = useRef<HarnessClient | undefined>(undefined);
  const unsubscribe = useRef<(() => void) | undefined>(undefined);
  const load = useCallback(async () => {
    if (logoutPending.current) return;
    const original = ++generation.current;
    unsubscribe.current?.();
    if (current.current) void current.current.close().catch(() => undefined);
    current.current = undefined;
    setClient(undefined);
    setConnection("connecting");
    setAuth("loading");
    setError("");
    let next: HarnessClient | undefined;
    try {
      next = await HarnessClient.fromServer();
      if (generation.current !== original) {
        await next.close();
        return;
      }
      current.current = next;
      unsubscribe.current = next.subscribe(setConnection);
      await next.connect();
      if (generation.current !== original) return;
      setClient(next);
      setAuth("authenticated");
    } catch (failure) {
      if (generation.current !== original) return;
      if (next) void next.close().catch(() => undefined);
      setAuth("required");
      setConnection("disconnected");
      if (!(failure instanceof RequestError && failure.error.code === "forbidden"))
        setError(failure instanceof Error ? failure.message : "服务当前不可取得");
    }
  }, []);
  useEffect(() => {
    void load();
    return () => {
      generation.current++;
      unsubscribe.current?.();
      if (current.current) void current.current.close().catch(() => undefined);
    };
  }, [load]);
  const login = useCallback(
    async (token: string) => {
      if (logoutPending.current) throw new Error("当前浏览器会话正在注销，请等待原注销决定");
      const response = await fetch("/auth/session", {
        method: "POST",
        credentials: "same-origin",
        redirect: "error",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token }),
      });
      const body = parseStrict(await readBounded(response, 16384));
      if (!response.ok || !isObject(body) || body.authenticated !== true)
        throw new Error("登录未获服务确认");
      await load();
    },
    [load],
  );
  const disconnect = useCallback(async (nextAuth: Authentication = "required") => {
    generation.current++;
    unsubscribe.current?.();
    const previous = current.current;
    current.current = undefined;
    setClient(undefined);
    setAuth(nextAuth);
    setConnection("closed");
    if (previous) {
      try {
        await previous.close();
      } catch (failure) {
        setError(failure instanceof Error ? failure.message : "浏览器账本关闭未确认");
      }
    }
  }, []);
  const logout = useCallback(async () => {
    if (logoutPending.current) return;
    logoutPending.current = true;
    const closing = disconnect("loading");
    const original = generation.current;
    await closing;
    try {
      const session = await fetch("/auth/session", {
        credentials: "same-origin",
        redirect: "error",
      });
      const body = parseStrict(await readBounded(session, 16384));
      if (!session.ok || !isObject(body) || typeof body.csrf_token !== "string")
        throw new Error("无法取得当前浏览器会话注销凭据");
      const response = await fetch("/auth/logout", {
        method: "POST",
        credentials: "same-origin",
        redirect: "error",
        headers: { "X-CSRF-Token": body.csrf_token },
      });
      if (!response.ok) throw new Error("浏览器会话注销尚未确认");
      setError("");
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : "浏览器会话注销尚未确认");
    } finally {
      logoutPending.current = false;
      if (generation.current === original) setAuth("required");
    }
  }, [disconnect]);
  return { client, connection, auth, error, login, reconnect: load, disconnect, logout };
}
