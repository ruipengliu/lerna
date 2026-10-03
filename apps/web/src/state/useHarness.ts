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
  const current = useRef<HarnessClient | undefined>(undefined);
  const unsubscribe = useRef<(() => void) | undefined>(undefined);
  const load = useCallback(async () => {
    const original = ++generation.current;
    unsubscribe.current?.();
    current.current && void current.current.close();
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
      if (next) void next.close();
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
      if (current.current) void current.current.close();
    };
  }, [load]);
  const login = useCallback(
    async (token: string) => {
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
  const disconnect = useCallback(async () => {
    generation.current++;
    unsubscribe.current?.();
    const previous = current.current;
    current.current = undefined;
    setClient(undefined);
    setAuth("required");
    setConnection("closed");
    if (previous) await previous.close();
  }, []);
  return { client, connection, auth, error, login, reconnect: load, disconnect };
}
