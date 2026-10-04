import { readFileSync } from "node:fs";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { expect, it } from "vitest";
import { RestrictedForm } from "./RestrictedForm";

it("the trusted form presents the original modern report and optional reference fields", () => {
  const schema = JSON.parse(
    readFileSync(
      new URL("../../../../sdk/ts/src/modern-goal-schema.fixture.json", import.meta.url),
      "utf8",
    ),
  );
  const ref = {
    tenant_id: "tenant_00000000000000000000000000000001",
    owner_id: "owner_00000000000000000000000000000001",
    content_id: "content_00000000000000000000000000000001",
    version: 1,
    hash: `sha256:${"a".repeat(64)}`,
    media_type: "application/json",
    byte_length: 10,
  };
  const report = {
    kind: "report",
    title: "原报告",
    body: "原正文",
    save_path: "report.md",
    preference: {
      query_ref: ref,
      scope_ref: ref,
      allowed_formats: ["plain", "bullet"],
      default_format: "plain",
    },
  };
  const html = renderToStaticMarkup(
    createElement(RestrictedForm, { schema, value: report, onChange: () => {} }),
  );
  expect(html).toContain("原报告");
  expect(html).toContain("原正文");
  expect(html).toContain("query_ref");
  expect(html).toContain("allowed_formats");
  expect(html).not.toContain("字段类型未开放");
});
