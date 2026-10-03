import { expect, it } from "vitest";
import { fetchContent } from "./client";
import type { ContentRef } from "./contracts.gen";
const ref: ContentRef = {
  tenant_id: "tenant_00000000000000000000000000000001",
  owner_id: "service_00000000000000000000000000000001",
  content_id: "content_00000000000000000000000000000001",
  version: 1,
  hash: "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
  media_type: "text/plain",
  byte_length: 3,
};
it("只返回准确长度和摘要全文，摘要不符、截断与超限均拒绝", async () => {
  const bytes = await fetchContent(ref, {
    baseURL: "http://127.0.0.1:5173",
    fetcher: async () => new Response("abc"),
  });
  expect(new TextDecoder().decode(bytes)).toBe("abc");
  await expect(
    fetchContent(ref, {
      baseURL: "http://127.0.0.1:5173",
      fetcher: async () => new Response("abd"),
    }),
  ).rejects.toThrow(/digest_mismatch/);
  await expect(
    fetchContent(ref, {
      baseURL: "http://127.0.0.1:5173",
      fetcher: async () => new Response("ab"),
    }),
  ).rejects.toThrow(/digest_mismatch/);
  await expect(
    fetchContent(ref, {
      baseURL: "http://127.0.0.1:5173",
      fetcher: async () => new Response("abcdef"),
    }),
  ).rejects.toThrow(/too_large/);
});
