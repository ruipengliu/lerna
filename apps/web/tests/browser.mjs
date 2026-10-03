import assert from "node:assert/strict";
import { readFile, mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import { execFileSync } from "node:child_process";
import { chromium } from "playwright";

// Uses the real configured Go service; boundary faults drop a reply or corrupt exact body bytes.
const baseURL = process.env.HARNESS_BROWSER_URL ?? "http://127.0.0.1:5173";
const flow = process.env.HARNESS_BROWSER_FLOW ?? "full";
assert(["full", "surface", "control"].includes(flow), "unsupported HARNESS_BROWSER_FLOW");
const expectedEvent = process.env.HARNESS_EXPECT_EVENT ?? "applied";
assert(["applied", "rejected"].includes(expectedEvent), "unsupported HARNESS_EXPECT_EVENT");
const existingControlTask = process.env.HARNESS_CONTROL_TASK;
if (existingControlTask) {
  assert.equal(flow, "control", "HARNESS_CONTROL_TASK only applies to the control slice");
  assert.match(existingControlTask, /^task_[0-9a-f]{32}$/);
}
const implementation = execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim();
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
const sent = [];
const replies = [];
const connections = [];
const pageErrors = [];
const controlDecisions = [];
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
    if (frame.type === "request") {
      requests.set(frame.request_seq, frame);
      sent.push(frame);
    }
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
async function until(check, label, timeout = 60000, interval = 100) {
  const before = Date.now();
  while (Date.now() - before < timeout) {
    const value = await check();
    if (value) return value;
    await new Promise((finish) => setTimeout(finish, interval));
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
  const sentOffset = sent.length;
  await row.getByRole("button", { name: "查看", exact: true }).click();
  const read = await until(() => {
    const fresh = sent
      .slice(sentOffset)
      .find(
        (frame) =>
          frame.kind === "query" &&
          frame.payload.method === "task.read" &&
          frame.payload.target_id === id,
      );
    return replies
      .slice(offset)
      .find(
        ({ request, response }) =>
          fresh &&
          request?.payload.query_id === fresh.payload.query_id &&
          response.result_kind === "query_result",
      );
  }, "fresh task.read response");
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
  for (let attempt = 0; attempt < 4; attempt++) {
    // Each click is a fresh user intent after an authoritative read. The SDK never changes CAS.
    await selectTask(id);
    await page.locator(".inspector").getByRole("button", { name: label, exact: true }).click();
    const offset = replies.length;
    await page
      .locator(".method-console")
      .getByRole("button", { name: "耐久保存并提交", exact: true })
      .click();
    const decided = await until(() => lastReceipt(method, offset), method);
    const command = decided.request.payload;
    const receipt = decided.response.payload;
    assert(!controlDecisions.some((decision) => decision.command_id === command.command_id));
    controlDecisions.push({
      command_id: command.command_id,
      method,
      target_id: id,
      expected_revision: command.expected_revision,
      stage: receipt.stage,
      ...(receipt.error ? { error: receipt.error } : {}),
    });
    if (receipt.stage === "rejected" && receipt.error.code === "revision_conflict") continue;
    assert.equal(receipt.stage, "applied", `${method} must have a real business decision`);
    await until(
      async () => (await page.locator(".inspector").innerText()).includes(gate),
      `${method} original authority state`,
    );
    return;
  }
  throw new Error(`${method}: four explicit fresh control intents met concurrent revisions`);
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
  await until(
    async () => {
      await selectTask(id);
      return (await page.locator(".inspector").innerText()).includes("准确导出已发布");
    },
    "authoritative Result publication",
    90000,
    1000,
  );
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
  const taskIDs = [];
  let waitingID;
  let cancellationID;
  let currentPresentation;
  let eventView;
  let archivedSession;
  const checks = ["complete authenticated manifest and connection identity binding"];
  const config = await page.evaluate(async () => (await fetch("/api/development/config")).json());
  const discovery = await page.evaluate(async () => (await fetch("/api/discovery")).json());
  const methodsBytes = Buffer.byteLength(JSON.stringify(discovery.methods));
  if (process.env.HARNESS_REQUIRE_LARGE_MANIFEST === "1") {
    assert(methodsBytes > 262144);
    assert(methodsBytes <= 1048576);
  }
  if (flow === "full") {
    const body = `真实浏览器端到端验证 ${runID}。\n保留准确正文，并核验独立文件读回。`;
    const before = replies.length;
    await report(`浏览器报告 ${runID}`, body, `reports/browser-${runID}.md`);
    const submitted = await until(() => latestSubmit(before), "real task.submit receipt");
    const taskID = submitted.response.payload.output.task_ref.object_id;
    taskIDs.push(taskID);
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
    process.stdout.write("Actual report Result and exact artifact preview passed.\n");

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
    taskIDs.push(recoveredTaskID);
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
    process.stdout.write("Original applied receipt lookup after reload passed.\n");
    checks.push(
      "actual report file and independent readback",
      "accurate full artifact preview",
      "lost applied receipt + reload + original lookup",
      "single task.submit identity",
    );
  }
  assert(connections.length > 0);
  for (const connection of connections) {
    assert.equal(connection.identity_scope, discovery.identity_scope);
    assert.equal(connection.identity_revision, discovery.identity_revision);
    assert.equal(connection.methods_digest, discovery.methods_digest);
  }

  // A free goal stays incomplete until an exact registered answer is consumed.
  if (flow !== "surface") {
    waitingID = existingControlTask ?? (await freeGoal(runID, config, discovery));
    taskIDs.push(waitingID);
    if (existingControlTask) {
      await selectTask(waitingID);
      const existing = JSON.parse(await page.locator(".inspector details pre").textContent());
      const controlState = (existing.task ?? existing).control;
      if (controlState === "paused") await control(waitingID, "恢复", "task.resume", "running");
      else assert.equal(controlState, "running", "existing control responsibility must be open");
      checks.push("original paused control Task resumed with a new explicit CAS intent");
    }
    await control(waitingID, "暂停", "task.pause", "paused");
    await control(waitingID, "恢复", "task.resume", "running");
    if (flow === "full") {
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
        async () =>
          await trustedInput.getByRole("button", { name: "保存准确回答并提交" }).isEnabled(),
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
      const clarifiedTask = JSON.parse(
        await page.locator(".inspector details pre").textContent(),
      ).task;
      assert(clarifiedTask.goal_revision >= inputView.request.goal_revision + 1);
      assert.equal(clarifiedTask.status, "succeeded");
      await page.locator(".inspector").getByRole("button", { name: "预览当前准确正文" }).click();
      const document = await until(async () => {
        for (const body of await page.locator(".exact-body").allTextContents()) {
          try {
            const value = JSON.parse(body);
            if (value.format_version === 1 && value.initial_goal_ref && value.amendment_refs)
              return value;
          } catch {
            /* Other accurately rendered artifacts need not be JSON. */
          }
        }
      }, "accurate GoalDocument retains initial source and amendment");
      const initial = commands.find(
        (command) => command.method === "task.submit" && command.target_id === waitingID,
      );
      const answer = commands.find(
        (command) => command.method === "task.input" && command.target_id === waitingID,
      );
      assert.deepEqual(document.initial_goal_ref, initial.payload.goal_ref);
      assert.deepEqual(document.amendment_refs, [answer.payload.answer_ref]);
      cancellationID = await freeGoal(`${runID}-cancel`, config, discovery);
      taskIDs.push(cancellationID);
      checks.push(
        "corrupt required body disables input, exact correction consumes original request",
        "clarification preserves exact initial source and amendment and advances goal_revision",
      );
    } else cancellationID = waitingID;
    await control(cancellationID, "取消", "task.cancel", "cancelled");
    checks.push("real pause/resume/cancel decisions");
    process.stdout.write("Actual task control decisions passed.\n");
  }

  // Only an explicitly registered fixed demo binding can issue an application event.
  if (flow !== "control") {
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
    const surfaceOffset = replies.length;
    await surfaceConsole.getByRole("button", { name: "耐久保存并提交", exact: true }).click();
    const createdSurface = await until(
      () => lastReceipt("surface.create", surfaceOffset),
      "actual surface.create business decision",
    );
    assert.equal(
      createdSurface.response.payload.stage,
      "applied",
      `surface.create: ${JSON.stringify(createdSurface.response.payload.error ?? {})}`,
    );
    const renderer = page.locator(".presentation-renderer");
    await renderer.getByRole("button", { name: "打开此准确 Surface" }).click();
    assert.equal(
      await renderer.getByRole("button", { name: "记录当前准确呈现" }).isEnabled(),
      false,
    );
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
    assert.equal(
      await renderer.getByRole("button", { name: "记录当前准确呈现" }).isEnabled(),
      false,
    );
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
    currentPresentation = replies
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
    assert.equal(
      await renderer.getByRole("button", { name: "记录当前准确呈现" }).isEnabled(),
      false,
    );
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
    await until(
      async () => {
        const read = fixedEvent.getByRole("button", { name: "查询原事件消费" });
        if (await read.count()) await read.click();
        const body = await fixedEvent.innerText();
        return body.includes("固定业务已提交 applied 决定") || body.includes("固定业务已拒绝");
      },
      "actual fixed demo session archive decision",
      90000,
      1000,
    );
    eventView = JSON.parse(await fixedEvent.locator("details pre").textContent());
    assert.equal(eventView.command.method, "session.archive");
    assert.equal(eventView.receipt.stage, expectedEvent);
    if (expectedEvent === "rejected")
      assert.equal(eventView.receipt.error.code, "revision_conflict");
    assert.equal(eventView.command.expected_revision, 1);
    assert.match(eventView.command.target_id, /^session_[0-9a-f]{32}$/);
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
    process.stdout.write("Exact Surface gates, fixed event consumption, and close passed.\n");
    await surfaceConsole.getByLabel("公开方法", { exact: true }).selectOption("session.read");
    await surfaceConsole
      .getByLabel("固定目标 ID", { exact: true })
      .fill(eventView.command.target_id);
    const sessionOffset = replies.length;
    await surfaceConsole.getByRole("button", { name: "查询原服务", exact: true }).click();
    const sessionRead = await until(
      () =>
        replies
          .slice(sessionOffset)
          .find(
            ({ request, response }) =>
              request?.payload.method === "session.read" &&
              request.payload.target_id === eventView.command.target_id &&
              response.result_kind === "query_result",
          ),
      "original fixed Session archive fact",
    );
    archivedSession = sessionRead.response.payload.session;
    assert.equal(archivedSession.state, "archived");
    assert(archivedSession.revision >= 2);
    checks.push(
      "Surface exact inline body, corrupt body and uncached not_modified gates",
      expectedEvent === "applied"
        ? "fixed application event consumes registered demo session archive"
        : "fixed application event rejects stale CAS; original registered Session remains archived",
      "close invalidates generation and hides full body",
    );
  }

  await page.getByRole("button", { name: "工作台", exact: true }).click();
  if (cancellationID) await selectTask(cancellationID);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.evaluate(() => scrollTo(0, 0));
  await page.screenshot({ path: resolve(artifacts, "narrow-controls.png"), fullPage: true });
  assert.equal(
    await page.evaluate(() => document.documentElement.scrollWidth > innerWidth),
    false,
    "narrow view must not horizontally overflow",
  );
  for (const name of ["输入与分支", "内容与记忆", "授权与治理", "安装与评测", "工作台"]) {
    const navigationOffset = replies.length;
    await page.getByRole("button", { name, exact: true }).click();
    await page.getByRole("heading", { name, exact: true, level: 1 }).waitFor();
    if (name === "内容与记忆") {
      const listedMemory = await until(
        () =>
          replies
            .slice(navigationOffset)
            .find(({ request }) => request?.payload.method === "memory.list"),
        "authoritative memory list with an explicit read purpose",
      );
      assert.equal(listedMemory.request.payload.payload.purpose, "memory.read");
      assert.equal(listedMemory.request.payload.payload.limit, 20);
      assert.equal(listedMemory.response.result_kind, "query_result");
      checks.push("original authorized Memory collection with a fixed read purpose");
    }
  }
  await page.getByRole("button", { name: "退出登录", exact: true }).click();
  await page.getByLabel("开发凭据").waitFor();
  await until(
    async () => (await page.evaluate(async () => (await fetch("/api/discovery")).status)) === 403,
    "original browser session is actually revoked",
  );
  assert.deepEqual(pageErrors, [], "rendered UI must not throw JavaScript errors");
  checks.push(
    "narrow no overflow",
    "all management navigation",
    "logout preserves business responsibility",
  );
  const reportData = {
    implementation,
    base_url: baseURL,
    flow,
    schema_digest: discovery.schema_digest,
    methods_digest: discovery.methods_digest,
    methods_count: discovery.methods.length,
    methods_bytes: methodsBytes,
    connection_count: connections.length,
    content_security_policy: contentSecurityPolicy,
    profile: discovery.profile,
    task_ids: taskIDs,
    control_decisions: controlDecisions,
    presentation_id: currentPresentation?.presentation_id,
    application_event_id: eventView?.event_id,
    application_event_stage: eventView?.receipt.stage,
    original_fixed_session: archivedSession,
    checks,
    page_errors: pageErrors,
  };
  await writeFile(resolve(artifacts, "report.json"), `${JSON.stringify(reportData, null, 2)}\n`);
  process.stdout.write(`Real browser ${flow} checks passed; artifacts: ${artifacts}\n`);
} catch (failure) {
  await page
    .screenshot({ path: resolve(artifacts, "failure.png"), fullPage: true })
    .catch(() => {});
  await writeFile(
    resolve(artifacts, "failure-trace.json"),
    `${JSON.stringify({ implementation, base_url: baseURL, connections, commands, replies, page_errors: pageErrors }, null, 2).replaceAll(token, "[redacted credential]")}\n`,
  );
  process.stderr.write(`${String(failure).replaceAll(token, "[redacted credential]")}\n`);
  process.exitCode = 1;
} finally {
  await browser.close();
}
