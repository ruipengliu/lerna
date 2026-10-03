import { expect, it } from "vitest";
import type { ContentRef } from "./contracts.gen";
import { verifyRenderBodies } from "./renderer";

const ref: ContentRef = {
  tenant_id: "tenant_00000000000000000000000000000001",
  owner_id: "service_00000000000000000000000000000001",
  content_id: "content_00000000000000000000000000000001",
  version: 1,
  hash: "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
  media_type: "text/plain",
  byte_length: 3,
};
it("只接受所有准确引用的完整原字节，不因 not_modified 或同内容 ID 省略核对", async () => {
  const result = await verifyRenderBodies([ref], [{ content_ref: ref, base64: "YWJj" }]);
  expect(new TextDecoder().decode(result[0]?.bytes)).toBe("abc");
  await expect(verifyRenderBodies([ref], [])).rejects.toThrow(/incomplete/);
  await expect(
    verifyRenderBodies([ref], [{ content_ref: { ...ref, version: 2 }, base64: "YWJj" }]),
  ).rejects.toThrow(/ref_mismatch/);
  await expect(verifyRenderBodies([ref], [{ content_ref: ref, base64: "YWJk" }])).rejects.toThrow(
    /digest_mismatch/,
  );
});
it("拒绝重复引用、别名 Base64 和无界正文", async () => {
  const body = { content_ref: ref, base64: "YWJj" };
  await expect(verifyRenderBodies([ref, ref], [body, body])).rejects.toThrow(/duplicate/);
  await expect(verifyRenderBodies([ref], [{ ...body, base64: "YWJj\n" }])).rejects.toThrow(
    /base64_invalid/,
  );
  const tooLarge = { ...ref, byte_length: 128 * 1024 + 1 };
  await expect(
    verifyRenderBodies([tooLarge], [{ content_ref: tooLarge, base64: "" }]),
  ).rejects.toThrow(/body_budget/);
});
