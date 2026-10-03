import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { execFileSync } from "node:child_process";
import { chromium } from "playwright";

// Tests the real configured Go service. Only the WebSocket boundary drops one reply.
const baseURL = process.env.HARNESS_BROWSER_URL ?? "http://127.0.0.1:5173";
const artifacts = resolve(process.env.HARNESS_BROWSER_ARTIFACTS ?? "/tmp/harness-web-browser");
const token = (
  await readFile(process.env.HARNESS_TOKEN_FILE ?? "/workspace/lerna-dev/.identity-token", "utf8")
).trim();
assert(token.length > 0, "development credential file is empty");
await mkdir(artifacts, { recursive: true });
const browser = await chromium.launch({
  executablePath: process.env.HARNESS_CHROMIUM ?? "/usr/bin/chromium",
  headless: true,
});
const context = await browser.newContext({ viewport: { width: 1536, height: 1024 } });
const commands = [];
const replies = [];
const pageErrors = [];
let dropNextSubmit = false;
let dropped;
await context.routeWebSocket("**/connect", (route) => {
  const remote = route.connectToServer();
  const requests = new Map();
  route.onMessage((message) => {
    const frame = JSON.parse(String(message));
    if (frame.type === "request") requests.set(frame.request_seq, frame);
    if (frame.type === "request" && frame.kind === "command") commands.push(frame.payload);
    remote.send(message);
  });
  remote.onMessage((message) => {
    const frame = JSON.parse(String(message));
    const request = requests.get(frame.request_seq);
    if (frame.type === "response") replies.push({ request, response: frame });
    if (
      dropNextSubmit &&
      request?.kind === "command" &&
      request.payload.method === "task.submit" &&
      frame.result_kind === "receipt" &&
      frame.payload.stage === "applied"
    ) {
      dropNextSubmit = false;
      dropped = { command: request.payload, receipt: frame.payload };
      route.close({ code: 1001, reason: "test lost receipt after business decision" });
      remote.close();
      return;
    }
    route.send(message);
  });
});
const page = await context.newPage();
page.on("pageerror", (error) => pageErrors.push(error.message));
async function until(check, label, timeout = 60000) {
  const before = Date.now();
  while (Date.now() - before < timeout) {
    const value = await check();
    if (value) return value;
    await new Promise((finish) => setTimeout(finish, 100));
  }
  throw new Error(`timed out: ${label}`);
}
async function selectTask(id) {
  const list = page.locator(".primary-panel .collection");
  await list.getByRole("button", { name: "刷新原集合" }).click();
  const row = list.getByRole("row").filter({ hasText: id });
  await row.waitFor();
  await row.getByRole("button", { name: "查看", exact: true }).click();
  await until(
    async () => (await page.locator(".inspector .selected-id").textContent()) === id,
    "selected task",
  );
}
async function report(title, body, path) {
  await page.getByLabel("报告标题", { exact: true }).fill(title);
  await page.getByLabel("准确目标正文", { exact: true }).fill(body);
  await page.getByLabel("输出文件", { exact: true }).fill(path);
  await page.getByRole("button", { name: "保存并提交", exact: true }).click();
}
function latestSubmit(before) {
  return replies
    .slice(before)
    .find(
      ({ request, response }) =>
        request?.kind === "command" &&
        request.payload.method === "task.submit" &&
        response.result_kind === "receipt" &&
        response.payload.stage === "applied",
    );
}
async function awaitResult(id) {
  await until(async () => {
    await selectTask(id);
    return (await page.locator(".inspector").innerText()).includes("准确导出已发布");
  }, "authoritative Result publication");
  assert.match(await page.locator(".inspector").innerText(), /succeeded/);
}
try {
  await page.goto(baseURL);
  await page.getByLabel("开发凭据").fill(token);
  await page.getByRole("button", { name: "认证并连接" }).click();
  await page.locator(".connection.ready").waitFor();
  await until(
    async () => await page.getByRole("button", { name: "保存并提交", exact: true }).isEnabled(),
    "trusted development publication config",
  );
  const runID = crypto.randomUUID().slice(0, 8);
  const body = `真实浏览器端到端验证 ${runID}。\n保留准确正文，并核验独立文件读回。`;
  const before = replies.length;
  await report(`浏览器报告 ${runID}`, body, `reports/browser-${runID}.md`);
  const submitted = await until(() => latestSubmit(before), "real task.submit receipt");
  const taskID = submitted.response.payload.output.task_ref.object_id;
  await awaitResult(taskID);
  await page.locator(".inspector").getByRole("button", { name: "预览当前准确正文" }).click();
  await until(
    async () =>
      (await page.locator(".exact-body").allTextContents()).some(
        (text) => text === `# 浏览器报告 ${runID}\n\n${body}\n`,
      ),
    "accurate artifact bytes and rendered full body",
  );
  await page.screenshot({ path: resolve(artifacts, "desktop-report.png"), fullPage: true });

  // A real backend decision exists, but its first receipt never reaches the browser.
  dropNextSubmit = true;
  await report(
    `恢复报告 ${runID}`,
    "验证原命令恢复，禁止重复 Task。",
    `reports/recovery-${runID}.md`,
  );
  await until(() => dropped, "lost real applied receipt");
  const original = JSON.stringify(dropped.command);
  const recoveredTaskID = dropped.receipt.output.task_ref.object_id;
  await page.reload();
  await page.locator(".connection.ready").waitFor();
  await page.getByRole("button", { name: "恢复原出版与后续命令" }).click();
  await until(
    () =>
      replies.some(
        ({ request, response }) =>
          request?.kind === "receipt_lookup" &&
          request.payload.command_id === dropped.command.command_id &&
          response.payload.stage === "applied",
      ),
    "original receipt lookup after reload",
  );
  assert.equal(
    commands.filter((command) => command.command_id === dropped.command.command_id).length,
    1,
  );
  assert.equal(JSON.stringify(dropped.command), original);
  await awaitResult(recoveredTaskID);

  // Publish a free goal via the actual content form; the Brain must request input.
  await page.getByRole("button", { name: "内容与记忆", exact: true }).click();
  await page.getByLabel("媒体类型", { exact: true }).selectOption("application/json");
  await page
    .getByLabel("准确原文", { exact: true })
    .fill(JSON.stringify({ request: "请先向本人澄清，再制定报告条件。", run_id: runID }));
  await page.getByRole("button", { name: "保存并发布正文", exact: true }).click();
  const contentView = page.locator(".content-publisher details pre");
  await contentView.waitFor();
  const goalRef = JSON.parse(await contentView.innerText());
  const config = await page.evaluate(async () => (await fetch("/api/development/config")).json());
  const discovery = await page.evaluate(async () => (await fetch("/api/discovery")).json());
  await page.getByRole("button", { name: "工作台", exact: true }).click();
  await page.locator(".advanced summary").click();
  await page.getByLabel("公开方法", { exact: true }).selectOption("task.submit");
  await page
    .getByLabel("固定目标 ID", { exact: true })
    .fill(`task_${crypto.randomUUID().replaceAll("-", "")}`);
  await page.getByLabel("orchestrator_id", { exact: true }).fill(discovery.logical_service_id);
  await page.getByLabel("准确目标正文引用", { exact: true }).fill(JSON.stringify(goalRef));
  await page
    .getByLabel("固定策略引用", { exact: true })
    .fill(JSON.stringify(config.task_policy_ref));
  await page.getByLabel("分单位预算", { exact: true }).fill(JSON.stringify(config.budget));
  await page
    .getByLabel("领域截止（UTC）", { exact: true })
    .fill(new Date(Date.now() + 600000).toISOString());
  await page
    .getByLabel("首次接纳截止（UTC）", { exact: true })
    .fill(new Date(Date.now() + 60000).toISOString());
  const controlBefore = replies.length;
  await page.getByRole("button", { name: "耐久保存并提交", exact: true }).click();
  const waiting = await until(() => latestSubmit(controlBefore), "free goal admitted");
  const waitingID = waiting.response.payload.output.task_ref.object_id;
  for (const [label, method, gate] of [
    ["暂停", "task.pause", "paused"],
    ["恢复", "task.resume", "running"],
    ["取消", "task.cancel", "cancelled"],
  ]) {
    await selectTask(waitingID);
    await page.locator(".inspector").getByRole("button", { name: label, exact: true }).click();
    const offset = replies.length;
    await page
      .locator(".method-console")
      .getByRole("button", { name: "耐久保存并提交", exact: true })
      .click();
    const decided = await until(
      () =>
        replies
          .slice(offset)
          .find(
            ({ request, response }) =>
              request?.payload.method === method && response.result_kind === "receipt",
          ),
      method,
    );
    assert.equal(
      decided.response.payload.stage,
      "applied",
      `${method} must have a real business decision`,
    );
    await until(
      async () => (await page.locator(".inspector").innerText()).includes(gate),
      `${method} original authority state`,
    );
  }
  await page.setViewportSize({ width: 390, height: 844 });
  await page.screenshot({ path: resolve(artifacts, "narrow-controls.png"), fullPage: true });
  assert.equal(
    await page.evaluate(() => document.documentElement.scrollWidth > innerWidth),
    false,
    "narrow view must not horizontally overflow",
  );
  for (const name of ["输入与分支", "内容与记忆", "授权与治理", "安装与评测", "工作台"]) {
    await page.getByRole("button", { name, exact: true }).click();
    await page.getByRole("heading", { name, exact: true, level: 1 }).waitFor();
  }
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await page.getByLabel("开发凭据").waitFor();
  assert.equal(await page.evaluate(async () => (await fetch("/api/discovery")).status), 403);
  assert.deepEqual(pageErrors, [], "rendered UI must not throw JavaScript errors");
  const reportData = {
    implementation: execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim(),
    base_url: baseURL,
    schema_digest: discovery.schema_digest,
    profile: discovery.profile,
    task_ids: [taskID, recoveredTaskID, waitingID],
    checks: [
      "actual report file and independent readback",
      "accurate full artifact preview",
      "lost applied receipt + reload + original lookup",
      "single task.submit identity",
      "real pause/resume/cancel decisions",
      "narrow no overflow",
      "all management navigation",
      "logout preserves business responsibility",
    ],
    page_errors: pageErrors,
  };
  await writeFile(resolve(artifacts, "report.json"), `${JSON.stringify(reportData, null, 2)}\n`);
  process.stdout.write(`Real browser checks passed; artifacts: ${artifacts}\n`);
} catch (failure) {
  await page
    .screenshot({ path: resolve(artifacts, "failure.png"), fullPage: true })
    .catch(() => {});
  process.stderr.write(`${String(failure).replaceAll(token, "[redacted credential]")}\n`);
  process.exitCode = 1;
} finally {
  await browser.close();
}
