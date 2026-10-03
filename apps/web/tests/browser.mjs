import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { execFileSync } from "node:child_process";
import { chromium } from "playwright";

// Uses the real configured Go service; boundary faults drop a reply or corrupt exact body bytes.
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
const connections = [];
const pageErrors = [];
let dropNextSubmit = false;
let dropped;
let corruptNextRender = false;
let corruptedRender = false;
let corruptedContent = "";
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
    if (frame.type === "ready") connections.push(frame);
    const request = requests.get(frame.request_seq);
    if (frame.type === "response") replies.push({ request, response: frame });
    if (
      corruptNextRender &&
      request?.payload.method === "presentation.read" &&
      frame.result_kind === "query_result" &&
      frame.payload.bodies.length
    ) {
      corruptNextRender = false;
      const fault = structuredClone(frame);
      const body = Buffer.from(fault.payload.bodies[0].base64, "base64");
      body[0] ^= 1;
      fault.payload.bodies[0].base64 = body.toString("base64");
      corruptedRender = true;
      route.send(JSON.stringify(fault));
      return;
    }
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
await context.route("**/api/content?**", async (route) => {
  const encoded = new URL(route.request().url()).searchParams.get("ref");
  const ref = encoded ? JSON.parse(Buffer.from(encoded, "base64url").toString("utf8")) : undefined;
  if (corruptedContent && ref?.content_id === corruptedContent) {
    corruptedContent = "";
    const response = await route.fetch();
    const body = Buffer.from(await response.body());
    assert(body.length > 0);
    body[0] ^= 1;
    await route.fulfill({ response, body });
  } else await route.continue();
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
  await until(async () => {
    if (await row.count()) return true;
    const next = list.getByRole("button", { name: "读取原游标下一页" });
    if (await next.count()) await next.click();
    return false;
  }, "task in authoritative paginated collection");
  const offset = replies.length;
  await row.getByRole("button", { name: "查看", exact: true }).click();
  const read = await until(
    () =>
      replies
        .slice(offset)
        .find(
          ({ request, response }) =>
            request?.payload.method === "task.read" &&
            request.payload.target_id === id &&
            response.result_kind === "query_result",
        ),
    "fresh task.read response",
  );
  await until(async () => {
    if ((await page.locator(".inspector .selected-id").textContent()) !== id) return false;
    const content = await page.locator(".inspector details pre").textContent();
    if (!content) return false;
    const displayed = JSON.parse(content);
    return (displayed.task ?? displayed).revision === read.response.payload.revision;
  }, "selected task");
}
function lastReceipt(method, offset = 0) {
  return replies
    .slice(offset)
    .find(
      ({ request, response }) =>
        request?.kind === "command" &&
        request.payload.method === method &&
        response.result_kind === "receipt",
    );
}
async function control(id, label, method, gate) {
  await selectTask(id);
  await page.locator(".inspector").getByRole("button", { name: label, exact: true }).click();
  const offset = replies.length;
  await page
    .locator(".method-console")
    .getByRole("button", { name: "耐久保存并提交", exact: true })
    .click();
  const decided = await until(() => lastReceipt(method, offset), method);
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
async function publishText(value, media = "text/plain") {
  await page.getByRole("button", { name: "内容与记忆", exact: true }).click();
  await page.getByLabel("媒体类型", { exact: true }).selectOption(media);
  await page.getByLabel("准确原文", { exact: true }).fill(value);
  await page.getByRole("button", { name: "保存并发布正文", exact: true }).click();
  const output = page.locator(".content-publisher details pre");
  await output.waitFor();
  return JSON.parse(await output.innerText());
}
async function freeGoal(runID, config, discovery) {
  const goalRef = await publishText(
    JSON.stringify({ request: "请先向本人澄清，再制定报告条件。", run_id: runID }),
    "application/json",
  );
  await page.getByRole("button", { name: "工作台", exact: true }).click();
  await page.locator(".advanced summary").click();
  const console = page.locator(".advanced .method-console");
  await console.getByLabel("公开方法", { exact: true }).selectOption("task.submit");
  await console
    .getByLabel("固定目标 ID", { exact: true })
    .fill(`task_${crypto.randomUUID().replaceAll("-", "")}`);
  await console.getByLabel("orchestrator_id", { exact: true }).fill(discovery.logical_service_id);
  await console.getByLabel("准确目标正文引用", { exact: true }).fill(JSON.stringify(goalRef));
  await console
    .getByLabel("固定策略引用", { exact: true })
    .fill(JSON.stringify(config.task_policy_ref));
  await console.getByLabel("预算金额", { exact: true }).fill(JSON.stringify(config.budget));
  await console
    .getByLabel("领域截止（UTC）", { exact: true })
    .fill(new Date(Date.now() + 600000).toISOString());
  await console
    .getByLabel("首次接纳截止（UTC）", { exact: true })
    .fill(new Date(Date.now() + 60000).toISOString());
  const offset = replies.length;
  await console.getByRole("button", { name: "耐久保存并提交", exact: true }).click();
  const submitted = await until(() => latestSubmit(offset), "free goal admitted");
  return submitted.response.payload.output.task_ref.object_id;
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
  const navigation = await page.goto(baseURL);
  const contentSecurityPolicy = navigation?.headers()["content-security-policy"] ?? "";
  if (process.env.HARNESS_REQUIRE_CSP === "1") {
    assert.match(contentSecurityPolicy, /default-src 'self'/);
    assert(!contentSecurityPolicy.includes("unsafe-eval"));
    assert(!contentSecurityPolicy.includes("unsafe-inline"));
  }
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
  await page.evaluate(() => scrollTo(0, 0));
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

  const config = await page.evaluate(async () => (await fetch("/api/development/config")).json());
  const discovery = await page.evaluate(async () => (await fetch("/api/discovery")).json());
  const methodsBytes = Buffer.byteLength(JSON.stringify(discovery.methods));
  if (process.env.HARNESS_REQUIRE_LARGE_MANIFEST === "1") {
    assert(methodsBytes > 262144);
    assert(methodsBytes <= 1048576);
  }
  assert(connections.length > 0);
  for (const connection of connections) {
    assert.equal(connection.identity_scope, discovery.identity_scope);
    assert.equal(connection.identity_revision, discovery.identity_revision);
    assert.equal(connection.methods_digest, discovery.methods_digest);
  }

  // A free goal stays incomplete until an exact registered answer is consumed.
  const waitingID = await freeGoal(runID, config, discovery);
  await control(waitingID, "暂停", "task.pause", "paused");
  await control(waitingID, "恢复", "task.resume", "running");
  const inputView = await until(async () => {
    await selectTask(waitingID);
    const inputList = page.locator(".task-inputs .collection");
    await inputList.getByRole("button", { name: "刷新原集合" }).click();
    const found = replies
      .toReversed()
      .find(
        ({ request, response }) =>
          request?.payload.method === "task.input_requests.list" &&
          request.payload.target_id === waitingID &&
          response.result_kind === "query_result" &&
          response.payload.items.some((item) => item.request.state === "pending"),
      );
    return found?.response.payload.items.find((item) => item.request.state === "pending");
  }, "original pending clarify_goal InputRequest");
  assert.equal(inputView.request.purpose, "clarify_goal");
  const inputRow = page
    .locator(".task-inputs")
    .getByRole("row")
    .filter({ hasText: inputView.request.request_id });
  corruptedContent = inputView.request.question_ref.content_id;
  await inputRow.getByRole("button", { name: "查看", exact: true }).click();
  const trustedInput = page
    .locator(".trusted-request")
    .filter({ has: page.getByRole("heading", { name: "原输入请求 · 精确版本" }) });
  await until(
    async () => (await trustedInput.innerText()).includes("digest_mismatch"),
    "corrupt original question is rejected",
  );
  assert.equal(
    await trustedInput.getByRole("button", { name: "保存准确回答并提交" }).isEnabled(),
    false,
  );
  await page.locator(".task-inputs").getByRole("button", { name: "刷新原集合" }).click();
  await inputRow.getByRole("button", { name: "查看", exact: true }).click();
  await trustedInput.getByLabel("回答格式").selectOption({ label: "report" });
  await trustedInput.getByLabel("报告标题", { exact: true }).fill(`澄清报告 ${runID}`);
  await trustedInput
    .getByLabel("报告正文", { exact: true })
    .fill("保留初始目标与准确补充，随后实际写入及独立读回。");
  await trustedInput
    .getByLabel("输出文件", { exact: true })
    .fill(`reports/clarification-${runID}.md`);
  await until(
    async () => await trustedInput.getByRole("button", { name: "保存准确回答并提交" }).isEnabled(),
    "accurate question and answer Schema enable input",
  );
  const inputOffset = replies.length;
  await trustedInput.getByRole("button", { name: "保存准确回答并提交" }).click();
  const answerReceipt = await until(
    () => lastReceipt("task.input", inputOffset),
    "actual task.input admission",
  );
  assert.notEqual(answerReceipt.response.payload.stage, "rejected");
  await awaitResult(waitingID);
  const clarifiedTask = JSON.parse(await page.locator(".inspector details pre").innerText()).task;
  assert.equal(clarifiedTask.goal_revision, 2);
  assert.equal(clarifiedTask.status, "succeeded");
  const cancellationID = await freeGoal(`${runID}-cancel`, config, discovery);
  await control(cancellationID, "取消", "task.cancel", "cancelled");

  // Only an explicitly registered fixed demo binding can issue an application event.
  assert(
    config.application_binding_ref,
    "real development fixture must register an application binding",
  );
  const snapshotText = `受信 Surface 准确快照 ${runID}。固定示例会话归档与任务事实独立记录。`;
  const snapshot = await publishText(snapshotText);
  await page.getByRole("button", { name: "输入与分支", exact: true }).click();
  const surfaceConsole = page.locator(".management-grid .method-console");
  await surfaceConsole.getByLabel("公开方法", { exact: true }).selectOption("surface.create");
  await surfaceConsole
    .getByLabel("固定目标 ID", { exact: true })
    .fill(`surface_${crypto.randomUUID().replaceAll("-", "")}`);
  await surfaceConsole
    .getByLabel("binding_ref", { exact: true })
    .fill(JSON.stringify(config.application_binding_ref));
  await surfaceConsole.getByLabel("snapshot_ref", { exact: true }).fill(JSON.stringify(snapshot));
  await surfaceConsole.getByLabel("request_refs", { exact: true }).fill("[]");
  await surfaceConsole.getByRole("button", { name: "耐久保存并提交", exact: true }).click();
  const renderer = page.locator(".presentation-renderer");
  await renderer.getByRole("button", { name: "打开此准确 Surface" }).click();
  assert.equal(await renderer.getByRole("button", { name: "记录当前准确呈现" }).isEnabled(), false);
  corruptNextRender = true;
  await renderer.getByRole("button", { name: "开始新呈现并读取全文" }).click();
  await until(
    async () =>
      corruptedRender && (await renderer.innerText()).includes("render_body_digest_mismatch"),
    "corrupt exact inline snapshot remains unrendered",
  );
  const fixedEvent = renderer.locator(".fixed-application-event");
  await fixedEvent
    .getByLabel("理由", { exact: true })
    .fill("本人通过当前准确呈现归档明确登记的示例会话。");
  assert.equal(await renderer.getByRole("button", { name: "记录当前准确呈现" }).isEnabled(), false);
  assert.equal(
    await renderer.getByRole("button", { name: "保存并提交固定事件" }).isEnabled(),
    false,
  );
  await renderer.getByRole("button", { name: "重新读取当前全文" }).click();
  await until(
    async () => await renderer.getByRole("button", { name: "记录当前准确呈现" }).isEnabled(),
    "full accurate snapshot is rendered",
  );
  assert.equal(await renderer.locator(".exact-body").innerText(), snapshotText);
  await renderer.getByRole("button", { name: "记录当前准确呈现" }).click();
  await until(
    async () => await renderer.getByRole("button", { name: "保存并提交固定事件" }).isEnabled(),
    "owner records current rendered generation",
  );
  const currentPresentation = replies
    .toReversed()
    .find(
      ({ request, response }) =>
        request?.payload.method === "presentation.rendered" &&
        response.result_kind === "receipt" &&
        response.payload.stage === "applied",
    ).response.payload.output;

  // A valid owner not_modified reply cannot stand in for this window's missing cache.
  await surfaceConsole.getByLabel("公开方法", { exact: true }).selectOption("presentation.read");
  await surfaceConsole
    .getByLabel("固定目标 ID", { exact: true })
    .fill(currentPresentation.presentation_id);
  await surfaceConsole
    .getByLabel("generation", { exact: true })
    .fill(String(currentPresentation.generation));
  await surfaceConsole
    .getByLabel("intent_revision", { exact: true })
    .fill(String(currentPresentation.intent_revision));
  await surfaceConsole
    .locator(".field")
    .filter({ has: page.locator("label", { hasText: "known_hash" }) })
    .getByRole("button", { name: "填写此字段" })
    .click();
  await surfaceConsole.getByLabel("known_hash", { exact: true }).fill(snapshot.hash);
  await surfaceConsole.getByRole("button", { name: "查询原服务", exact: true }).click();
  await until(
    async () => (await renderer.innerText()).includes("render_bodies_incomplete"),
    "not_modified without full cached bytes is rejected",
  );
  assert.equal(await renderer.getByRole("button", { name: "记录当前准确呈现" }).isEnabled(), false);
  assert.equal(
    await renderer.getByRole("button", { name: "保存并提交固定事件" }).isEnabled(),
    false,
  );
  await renderer.getByRole("button", { name: "重新读取当前全文" }).click();
  await until(
    async () => await renderer.getByRole("button", { name: "记录当前准确呈现" }).isEnabled(),
    "fresh full read restores presentation eligibility",
  );
  await renderer.getByRole("button", { name: "记录当前准确呈现" }).click();
  await fixedEvent.getByRole("button", { name: "保存并提交固定事件" }).click();
  await until(async () => {
    const read = fixedEvent.getByRole("button", { name: "查询原事件消费" });
    if (await read.count()) await read.click();
    return (await fixedEvent.innerText()).includes("固定业务已提交 applied 决定");
  }, "actual fixed demo session archive consumption");
  const eventView = JSON.parse(await fixedEvent.locator("details pre").innerText());
  assert.equal(eventView.command.method, "session.archive");
  assert.equal(eventView.receipt.stage, "applied");
  assert.equal(eventView.command.expected_revision, 1);
  assert.notEqual(eventView.command.target_id, waitingID);
  await page.evaluate(() => scrollTo(0, 0));
  await page.screenshot({ path: resolve(artifacts, "desktop-surface.png"), fullPage: true });
  await renderer.getByRole("button", { name: "关闭当前窗口" }).click();
  await until(
    async () => (await renderer.innerText()).includes("原窗口关闭决定已提交"),
    "original presentation close decision",
  );
  assert.equal(await renderer.locator(".exact-body").count(), 0);
  await surfaceConsole.getByRole("button", { name: "查询原服务", exact: true }).click();
  await until(
    async () => (await surfaceConsole.innerText()).includes("render_generation_stale"),
    "closed generation cannot read cached body",
  );

  await page.getByRole("button", { name: "工作台", exact: true }).click();
  await selectTask(cancellationID);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.evaluate(() => scrollTo(0, 0));
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
  await until(
    async () => (await page.evaluate(async () => (await fetch("/api/discovery")).status)) === 403,
    "original browser session is actually revoked",
  );
  assert.deepEqual(pageErrors, [], "rendered UI must not throw JavaScript errors");
  const reportData = {
    implementation: execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim(),
    base_url: baseURL,
    schema_digest: discovery.schema_digest,
    methods_digest: discovery.methods_digest,
    methods_count: discovery.methods.length,
    methods_bytes: methodsBytes,
    connection_count: connections.length,
    content_security_policy: contentSecurityPolicy,
    profile: discovery.profile,
    task_ids: [taskID, recoveredTaskID, waitingID, cancellationID],
    presentation_id: currentPresentation.presentation_id,
    application_event_id: eventView.event_id,
    checks: [
      "actual report file and independent readback",
      "complete authenticated manifest and connection identity binding",
      "accurate full artifact preview",
      "lost applied receipt + reload + original lookup",
      "single task.submit identity",
      "real pause/resume/cancel decisions",
      "corrupt required body disables input, exact correction consumes original request",
      "clarification preserves initial goal and advances goal_revision",
      "Surface exact inline body, corrupt body and uncached not_modified gates",
      "fixed application event consumes registered demo session archive",
      "close invalidates generation and hides full body",
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
  await writeFile(
    resolve(artifacts, "failure-trace.json"),
    `${JSON.stringify({ implementation: execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim(), base_url: baseURL, commands, replies, page_errors: pageErrors }, null, 2).replaceAll(token, "[redacted credential]")}\n`,
  );
  process.stderr.write(`${String(failure).replaceAll(token, "[redacted credential]")}\n`);
  process.exitCode = 1;
} finally {
  await browser.close();
}
