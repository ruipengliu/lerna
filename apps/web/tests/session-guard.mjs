import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { chromium } from "playwright";

// Reuses one real completed Task; revokes the original cookie while its native socket stays open.
const taskID = process.env.HARNESS_ORIGINAL_TASK;
assert(taskID && /^task_[0-9a-f]{32}$/.test(taskID), "HARNESS_ORIGINAL_TASK is required");
const baseURL = process.env.HARNESS_BROWSER_URL ?? "http://127.0.0.1:8080";
const artifacts = resolve(process.env.HARNESS_BROWSER_ARTIFACTS ?? "/tmp/harness-session-guard");
const implementation = execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim();
const token = (await readFile(process.env.HARNESS_TOKEN_FILE, "utf8")).trim();
assert(token.length > 0, "development credential file is empty");
await mkdir(artifacts, { recursive: true });
const browser = await chromium.launch({ executablePath: "/usr/bin/chromium", headless: true });
const context = await browser.newContext({ viewport: { width: 1536, height: 1024 } });
const page = await context.newPage();
const sockets = [];
const errors = [];
page.on("pageerror", (error) => errors.push(error.message));
page.on("websocket", (socket) => {
  const record = { sent: [], received: [], closed: false };
  sockets.push(record);
  socket.on("framesent", ({ payload }) => record.sent.push(JSON.parse(String(payload))));
  socket.on("framereceived", ({ payload }) => record.received.push(JSON.parse(String(payload))));
  socket.on("close", () => {
    record.closed = true;
  });
});
async function until(check, description) {
  const deadline = Date.now() + 15000;
  while (Date.now() < deadline) {
    const value = await check();
    if (value) return value;
    await page.waitForTimeout(100);
  }
  throw new Error(`timed out: ${description}`);
}
async function authenticate() {
  await page.getByLabel("开发凭据").fill(token);
  await page.getByRole("button", { name: "认证并连接" }).click();
  await page.locator(".connection.ready").waitFor();
}
async function original() {
  const collection = page.locator(".primary-panel .collection");
  const row = collection.getByRole("row").filter({ hasText: taskID });
  await until(async () => {
    if (await row.count()) return true;
    const next = collection.getByRole("button", { name: "读取原游标下一页" });
    if (await next.count()) await next.click();
    return false;
  }, "original Task in current authorized collection");
  await row.getByRole("button", { name: "查看", exact: true }).click();
  await page.locator(".inspector").getByText("准确导出已发布", { exact: true }).waitFor();
  const value = JSON.parse(await page.locator(".inspector details pre").textContent());
  assert.equal(value.task.task_id, taskID);
  assert.equal(value.task.status, "succeeded");
  assert.equal(value.task.accounting_open, false);
  assert.equal(value.publication, "published");
  return value;
}
try {
  const navigation = await page.goto(baseURL);
  const csp = navigation.headers()["content-security-policy"] ?? "";
  assert.match(csp, /default-src 'self'/);
  assert(!/unsafe-(?:eval|inline)/.test(csp));
  await authenticate();
  const before = await original();
  const socket = sockets[0];
  assert(socket && !socket.closed);
  const logout = await page.evaluate(async () => {
    const session = await (await fetch("/auth/session")).json();
    return (
      await fetch("/auth/logout", {
        method: "POST",
        headers: { "X-CSRF-Token": session.csrf_token },
      })
    ).status;
  });
  assert.equal(logout, 200);
  assert.equal(socket.closed, false, "the browser must not close the socket before server checks");
  const offset = socket.sent.length;
  await page
    .locator(".primary-panel .collection")
    .getByRole("button", { name: "刷新原集合" })
    .click();
  const denied = await until(
    () => socket.sent.slice(offset).find((frame) => frame.kind === "query"),
    "original authenticated socket sends a read after cookie revocation",
  );
  await until(() => socket.closed, "server rejects the revoked original cookie connection");
  assert.equal(
    socket.received.some(
      (frame) => frame.type === "response" && frame.request_seq === denied.request_seq,
    ),
    false,
    "revoked original session must not receive a late domain response",
  );
  assert.equal(await page.evaluate(async () => (await fetch("/api/discovery")).status), 403);
  await page.reload();
  await page.getByLabel("开发凭据").waitFor();
  await authenticate();
  const after = await original();
  assert.deepEqual(after.task.result_ref, before.task.result_ref);
  assert.deepEqual(after.content_ref, before.content_ref);
  assert.deepEqual(after.result, before.result);
  assert.equal(sockets.length, 2, "reauthentication creates a distinct original cookie socket");
  assert.equal(
    sockets.flatMap((item) => item.sent).filter((frame) => frame.kind === "command").length,
    0,
  );
  assert.deepEqual(errors, []);
  await page.screenshot({ path: resolve(artifacts, "original-session-guard.png"), fullPage: true });
  await writeFile(
    resolve(artifacts, "report.json"),
    `${JSON.stringify({ implementation, declared_backend_commit: process.env.HARNESS_BACKEND_COMMIT ?? null, base_url: baseURL, content_security_policy: csp, task_id: taskID, task_status: after.task.status, accounting_open: after.task.accounting_open, result_ref: after.task.result_ref, content_ref: after.content_ref, revoked_query_id: denied.payload.query_id, original_cookie_connection_closed: socket.closed, fresh_cookie_connection_count: sockets.length, commands: 0, page_errors: errors }, null, 2)}\n`,
  );
  process.stdout.write(`Original cookie revocation and unchanged Result passed; ${artifacts}\n`);
} catch (failure) {
  await page
    .screenshot({ path: resolve(artifacts, "failure.png"), fullPage: true })
    .catch(() => {});
  await writeFile(
    resolve(artifacts, "failure-trace.json"),
    JSON.stringify({ sockets, errors }, null, 2).replaceAll(token, "[redacted credential]"),
  );
  process.stderr.write(`${String(failure).replaceAll(token, "[redacted credential]")}\n`);
  process.exitCode = 1;
} finally {
  await browser.close();
}
