import { createServer, type Server } from "node:https";
import { readFileSync } from "node:fs";
import type { IncomingMessage, ServerResponse } from "node:http";
import { randomBytes } from "node:crypto";
import { WebSocketServer, type WebSocket } from "ws";
import {
  parseStrict,
  canonical,
  newID,
  PROTOCOL,
  PROFILE,
  TRANSPORT_PROFILE,
  SOCKET_SUBPROTOCOL,
} from "@harness/sdk";
import { authenticate, current } from "./authority";
import { failure, reject } from "./error";
import { object, text, integer, type Principal } from "./types";
import { manifest, controlMethod, type Runtime } from "./runtime";

interface Session {
  principal: Principal;
  active: boolean;
  csrf: string;
  expires: number;
}
function bearer(req: IncomingMessage): string {
  const v = req.headers.authorization;
  return v?.startsWith("Bearer ") ? v.slice(7) : "";
}
function cookieID(req: IncomingMessage): string {
  return (
    req.headers.cookie
      ?.split(";")
      .map((s) => s.trim())
      .find((s) => s.startsWith("alternate_session="))
      ?.slice(18) ?? ""
  );
}
function sameOrigin(req: IncomingMessage, required = false): void {
  if (
    (required && !req.headers.origin) ||
    (req.headers.origin && req.headers.origin !== `https://${req.headers.host}`)
  )
    reject("forbidden", "origin_mismatch");
}
function resolver(runtime: Runtime, req: IncomingMessage, write = false): () => Principal {
  const token = bearer(req);
  if (token) {
    const p = authenticate(runtime.store, token);
    sameOrigin(req);
    return () => current(runtime.store, p);
  }
  const id = cookieID(req);
  if (!id) reject("forbidden", "authentication_required");
  sameOrigin(req, write || req.url === "/connect");
  return () => {
    const s = runtime.store.require<Session>("sessions", id);
    if (
      !s.active ||
      s.expires <= runtime.store.now() ||
      (write && req.headers["x-csrf-token"] !== s.csrf)
    )
      reject("forbidden", "browser_session_not_current");
    return current(runtime.store, s.principal);
  };
}
function control(method: unknown): boolean {
  return typeof method === "string" && controlMethod(method);
}
function send(ws: WebSocket, value: unknown, isControl = false): void {
  const body = canonical(value);
  if (Buffer.byteLength(body) > 1048576 || ws.bufferedAmount > (isControl ? 4 : 3) * 1048576) {
    ws.close(1013, "bounded queue full");
    return;
  }
  ws.send(body);
}

export async function serve(runtime: Runtime): Promise<Server> {
  const config = runtime.store.config,
    inflight = new Set<Promise<unknown>>();
  let stopping = false;
  const track = (promise: Promise<unknown>) => {
    inflight.add(promise);
    void promise.finally(() => inflight.delete(promise));
  };
  const server = createServer(
    {
      cert: readFileSync(config.tls_cert_file),
      key: readFileSync(config.tls_key_file),
      minVersion: "TLSv1.2",
    },
    (req, res) => {
      track(http(runtime, req, res));
    },
  );
  server.maxConnections = 64;
  server.requestTimeout = 5000;
  server.headersTimeout = 5000;
  const wss = new WebSocketServer({
    noServer: true,
    maxPayload: 1048576,
    perMessageDeflate: false,
    handleProtocols: (p) => (p.has(SOCKET_SUBPROTOCOL) ? SOCKET_SUBPROTOCOL : false),
  });
  server.on("upgrade", (req, socket, head) => {
    try {
      if (
        stopping ||
        req.url !== "/connect" ||
        !req.headers["sec-websocket-protocol"]
          ?.split(",")
          .map((p) => p.trim())
          .includes(SOCKET_SUBPROTOCOL)
      )
        reject("unsupported", "endpoint_or_subprotocol_not_supported");
      const resolve = resolver(runtime, req),
        auth = resolve();
      wss.handleUpgrade(req, socket, head, (ws) => {
        let pending = 0,
          normal = 0,
          sequence = 0,
          nonce = "",
          pingAt = 0,
          lastPing = Date.now();
        const d = runtime.discovery(auth);
        send(
          ws,
          {
            type: "ready",
            connection_id: newID("connection"),
            logical_service_id: config.owner_id,
            profile: PROFILE,
            transport_profile: TRANSPORT_PROFILE,
            methods_digest: d.methods_digest,
            limits: d.limits,
            identity_scope: d.identity_scope,
            identity_revision: auth.generation,
          },
          true,
        );
        const timer = setInterval(() => {
          try {
            resolve();
            if (nonce && Date.now() - pingAt >= 30000) {
              ws.close(1008, "heartbeat expired");
              return;
            }
            if (!nonce && Date.now() - lastPing >= 15000) {
              nonce = randomBytes(16).toString("hex");
              pingAt = Date.now();
              lastPing = pingAt;
              send(ws, { type: "ping", nonce }, true);
            }
          } catch {
            ws.close(1008, "authority changed");
          }
        }, 250);
        ws.once("close", () => clearInterval(timer));
        ws.on("message", (bytes, binary) => {
          track(
            (async () => {
              try {
                const p = resolve();
                if (binary) reject("invalid_request", "binary_frame");
                const frame = object(parseStrict(Buffer.from(bytes as Buffer), 1048576)),
                  tag = text(frame.type);
                if (tag === "pong" || tag === "ping") {
                  if (
                    Object.keys(frame).length !== 2 ||
                    typeof frame.nonce !== "string" ||
                    Buffer.byteLength(frame.nonce) > 256
                  )
                    reject("invalid_request", "invalid_ping");
                  if (tag === "ping") send(ws, { type: "pong", nonce: frame.nonce }, true);
                  else {
                    if (!nonce || frame.nonce !== nonce)
                      reject("invalid_request", "pong_nonce_mismatch");
                    nonce = "";
                  }
                  return;
                }
                if (tag !== "request" || Object.keys(frame).length !== 4)
                  reject("invalid_request", "invalid_frame");
                const seq = integer(frame.request_seq),
                  kind = text(frame.kind),
                  isControl = kind === "receipt_lookup" || control(object(frame.payload).method);
                if (seq <= sequence || pending >= 32 || (!isControl && normal >= 28))
                  reject("overloaded", "sequence_or_capacity");
                sequence = seq;
                pending++;
                if (!isControl) normal++;
                try {
                  let resultKind = "error",
                    payload: unknown;
                  try {
                    if (kind === "command") {
                      payload = await runtime.command(p, frame.payload);
                      resultKind = "receipt";
                    } else if (kind === "query") {
                      payload = await runtime.query(p, frame.payload);
                      resultKind = "query_result";
                    } else if (kind === "receipt_lookup") {
                      payload = runtime.lookup(p, frame.payload);
                      resultKind = "receipt";
                    } else reject("invalid_request", "invalid_request_kind");
                  } catch (error) {
                    payload = failure(error);
                  }
                  resolve();
                  if (
                    config.fault?.drop_response_method &&
                    object(frame.payload).method === config.fault.drop_response_method
                  ) {
                    delete config.fault.drop_response_method;
                    ws.terminate();
                    return;
                  }
                  send(
                    ws,
                    { type: "response", request_seq: seq, result_kind: resultKind, payload },
                    isControl,
                  );
                } finally {
                  pending--;
                  if (!isControl) normal--;
                }
              } catch {
                ws.close(1008, "request rejected");
              }
            })(),
          );
        });
      });
    } catch {
      socket.write("HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n");
      socket.destroy();
    }
  });
  await new Promise<void>((resolve) => server.listen(config.port, "127.0.0.1", resolve));
  process.stdout.write(
    `${canonical({ ready: true, port: (server.address() as { port: number }).port, system: config.system, owner_id: config.owner_id })}\n`,
  );
  const stop = () => {
    if (stopping) return;
    stopping = true;
    for (const ws of wss.clients) ws.terminate();
    wss.close();
    server.close(() => {
      void Promise.allSettled([...inflight, runtime.handler.stop?.()]).then(() =>
        runtime.store.close(),
      );
    });
  };
  process.once("SIGINT", stop);
  process.once("SIGTERM", stop);
  return server;
}
async function readBody(req: IncomingMessage, max = 262144): Promise<Buffer> {
  const chunks: Buffer[] = [];
  let n = 0;
  for await (const chunk of req) {
    const b = Buffer.from(chunk as Uint8Array);
    n += b.length;
    if (n > max) reject("invalid_request", "body_too_large");
    chunks.push(b);
  }
  return Buffer.concat(chunks);
}
function json(res: ServerResponse, value: unknown): void {
  res.writeHead(200, { "Content-Type": "application/json", "Cache-Control": "no-store" });
  res.end(canonical(value));
}
async function http(runtime: Runtime, req: IncomingMessage, res: ServerResponse): Promise<void> {
  try {
    const url = new URL(req.url ?? "/", "https://127.0.0.1");
    if (req.method === "GET" && url.pathname === "/.well-known/harness") {
      json(res, {
        protocol: PROTOCOL,
        transport_profile: TRANSPORT_PROFILE,
        login_path: "/auth/session",
        connect_path: "/connect",
      });
      return;
    }
    if (req.method === "GET" && url.pathname === "/api/schema/core") {
      res.writeHead(200, { "Content-Type": "application/schema+json" });
      res.end(manifest.core_source);
      return;
    }
    if (req.method === "POST" && url.pathname === "/auth/session") {
      sameOrigin(req);
      const input = object(parseStrict(await readBody(req, 8192)));
      if (Object.keys(input).length !== 1) reject("invalid_request", "invalid_login");
      const p = authenticate(runtime.store, text(input.token)),
        id = randomBytes(32).toString("hex"),
        csrf = randomBytes(32).toString("hex");
      runtime.store.tx(() =>
        runtime.store.put("sessions", id, {
          principal: p,
          csrf,
          active: true,
          expires: Math.min(Date.parse(p.expires_at), runtime.store.now() + 1800000),
        }),
      );
      res.setHeader(
        "Set-Cookie",
        `alternate_session=${id}; HttpOnly; Secure; SameSite=Strict; Path=/`,
      );
      json(res, { authenticated: true, csrf_token: csrf });
      return;
    }
    const resolve = resolver(runtime, req, req.method !== "GET"),
      p = resolve();
    if (req.method === "GET" && url.pathname === "/auth/session") {
      const s = runtime.store.require<Session>("sessions", cookieID(req));
      json(res, { authenticated: true, csrf_token: s.csrf });
      return;
    }
    if (req.method === "POST" && url.pathname === "/auth/logout") {
      const id = cookieID(req);
      if (!id) reject("invalid_request", "cookie_session_required");
      runtime.store.tx(() => {
        const s = runtime.store.require<Session>("sessions", id);
        s.active = false;
        runtime.store.put("sessions", id, s);
      });
      res.setHeader(
        "Set-Cookie",
        "alternate_session=; HttpOnly; Secure; SameSite=Strict; Path=/; Max-Age=0",
      );
      json(res, { authenticated: false });
      return;
    }
    if (req.method === "GET" && url.pathname === "/api/discovery") {
      json(res, runtime.discovery(p));
      return;
    }
    if (req.method === "POST" && url.pathname === "/api/call") {
      const i = object(parseStrict(await readBody(req)));
      if (Object.keys(i).length !== 2) reject("invalid_request", "invalid_call");
      let result: unknown;
      if (i.kind === "command") result = await runtime.command(p, i.payload);
      else if (i.kind === "query") result = await runtime.query(p, i.payload);
      else if (i.kind === "receipt_lookup") result = runtime.lookup(p, i.payload);
      else reject("invalid_request", "invalid_request_kind");
      resolve();
      json(res, result);
      return;
    }
    if (
      req.method === "POST" &&
      /^\/api\/transfers\/[a-z][a-z0-9_]*_[0-9a-f]{32}$/.test(url.pathname) &&
      runtime.handler.receive
    ) {
      const body = await readBody(req);
      resolve();
      json(res, runtime.handler.receive(p, url.pathname.slice(15), body));
      return;
    }
    if (req.method === "GET" && url.pathname === "/api/content" && runtime.handler.bytes) {
      if (url.searchParams.get("location") !== "local" || !url.searchParams.get("purpose"))
        reject("invalid_request", "read_scope_required");
      const ref = parseStrict(Buffer.from(url.searchParams.get("ref") ?? "", "base64url")),
        body = runtime.store.tx(() =>
          runtime.handler.bytes?.(p, ref, url.searchParams.get("purpose") ?? ""),
        );
      resolve();
      res.writeHead(200, {
        "Content-Type": "application/octet-stream",
        "Cache-Control": "no-store",
      });
      res.end(body);
      return;
    }
    reject("unsupported", "endpoint_not_supported");
  } catch (error) {
    const body = failure(error);
    res.writeHead(
      body.code === "forbidden" ? 403 : body.code === "dependency_unavailable" ? 503 : 400,
      { "Content-Type": "application/json" },
    );
    res.end(canonical(body));
  }
}
