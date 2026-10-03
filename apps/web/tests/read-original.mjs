import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { chromium } from "playwright";

// Reads a pre-existing real Task and keeps its native connection across two server heartbeats.
const taskID = process.env.HARNESS_ORIGINAL_TASK;
assert(taskID && /^task_[0-9a-f]{32}$/.test(taskID), "HARNESS_ORIGINAL_TASK is required");
const baseURL = process.env.HARNESS_BROWSER_URL ?? "http://127.0.0.1:8080";
const artifacts = resolve(process.env.HARNESS_BROWSER_ARTIFACTS ?? "/tmp/harness-web-original");
const implementation = execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim();
const token = (
  await readFile(process.env.HARNESS_TOKEN_FILE ?? "/workspace/lerna-dev/.identity-token", "utf8")
).trim();
assert(token.length > 0, "development credential file is empty");
const browser = await chromium.launch({
  executablePath: process.env.HARNESS_CHROMIUM ?? "/usr/bin/chromium",
  headless: true,
});
const context = await browser.newContext({ viewport: { width: 1536, height: 1024 } });
const requests = [];
const responses = [];
const errors = [];
const closes = [];
if (process.env.HARNESS_PROXY_OBSERVE === "1") {
  await context.routeWebSocket("**/connect", (route) => {
    const remote = route.connectToServer();
    route.onMessage((message) => remote.send(message));
    remote.onMessage((message) => route.send(message));
  });
}
const page = await context.newPage();
page.on("pageerror", (error) => errors.push(error.message));
page.on("websocket", (socket) => {
  socket.on("framesent", ({ payload }) => requests.push(JSON.parse(String(payload))));
  socket.on("framereceived", ({ payload }) => responses.push(JSON.parse(String(payload))));
  socket.on("close", () => closes.push({ url: socket.url(), at: Date.now() }));
});
await mkdir(artifacts, { recursive: true });
try {
  const navigation = await page.goto(baseURL);
  const csp = navigation?.headers()["content-security-policy"] ?? "";
  assert.match(csp, /default-src 'self'/);
  assert(!/unsafe-(?:eval|inline)/.test(csp));
  await page.getByLabel("开发凭据").fill(token);
  await page.getByRole("button", { name: "认证并连接" }).click();
  await page.locator(".connection.ready").waitFor();
  const list = page.locator(".primary-panel .collection");
  const row = list.getByRole("row").filter({ hasText: taskID });
  const deadline = Date.now() + 30000;
  while (!(await row.count())) {
    assert(Date.now() < deadline, "original Task absent from current authorized collection");
    const next = list.getByRole("button", { name: "读取原游标下一页" });
    if (await next.count()) await next.click();
    await page.waitForTimeout(250);
  }
  await row.getByRole("button", { name: "查看", exact: true }).click();
  await page.locator(".inspector").getByText("准确导出已发布", { exact: true }).waitFor();
  const value = JSON.parse(await page.locator(".inspector details pre").textContent());
  assert.equal(value.task.task_id, taskID);
  assert.equal(value.task.status, "succeeded");
  assert.equal(value.task.accounting_open, false);
  assert.equal(value.publication, "published");
  await page.locator(".inspector").getByRole("button", { name: "预览当前准确正文" }).click();
  await page.locator(".preview-panel .exact-body").first().waitFor();
  if (process.env.HARNESS_EXPECTED_ARTIFACT_FILE) {
    const expected = await readFile(process.env.HARNESS_EXPECTED_ARTIFACT_FILE, "utf8");
    const end = Date.now() + 15000;
    while (!(await page.locator(".exact-body").allTextContents()).includes(expected)) {
      assert(Date.now() < end, "original published artifact is not exactly rendered");
      await page.waitForTimeout(250);
    }
  }
  assert.equal(
    requests.some((frame) => frame.kind === "command"),
    false,
  );
  process.stdout.write(
    "Original Task Result is published; exact full artifact rendered without a new command.\n",
  );
  await page.evaluate(() => scrollTo(0, 0));
  await page.screenshot({ path: resolve(artifacts, "original-result.png"), fullPage: true });
  for (let heartbeat = 0; heartbeat < 2; heartbeat++) {
    await page.waitForTimeout(35000);
    assert(await page.locator(".connection.ready").count(), "connection lost across heartbeat");
    await list.getByRole("button", { name: "刷新原集合" }).click();
    await page.waitForTimeout(500);
    assert.equal(await list.locator(".notice.error").count(), 0);
    process.stdout.write(`Original connection remains usable after heartbeat ${heartbeat + 1}.\n`);
  }
  const queries = requests.filter((frame) => frame.kind === "query").map((frame) => frame.payload);
  assert.equal(new Set(queries.map((query) => query.query_id)).size, queries.length);
  assert.equal(
    requests.some((frame) => frame.kind === "command"),
    false,
  );
  assert.deepEqual(errors, []);
  await writeFile(
    resolve(artifacts, "original-result.json"),
    `${JSON.stringify({ implementation, base_url: baseURL, proxy: process.env.HARNESS_PROXY_OBSERVE === "1", task_id: taskID, task_revision: value.task.revision, result_ref: value.task.result_ref, publication: value.publication, content_ref: value.content_ref, observed_heartbeats: 2, query_count: queries.length, commands: 0, page_errors: errors }, null, 2)}\n`,
  );
} catch (failure) {
  await page
    .screenshot({ path: resolve(artifacts, "failure.png"), fullPage: true })
    .catch(() => {});
  await writeFile(
    resolve(artifacts, "failure-trace.json"),
    `${JSON.stringify({ implementation, requests, responses, errors, closes }, null, 2).replaceAll(token, "[redacted credential]")}\n`,
  );
  process.stderr.write(`${String(failure).replaceAll(token, "[redacted credential]")}\n`);
  process.exitCode = 1;
} finally {
  await browser.close();
}
