import { describe, expect, it } from "vitest";
import { canonical, parseStrict, sha256 } from "./json";

describe("同版严格 JSON 与 JCS", () => {
  it("与 Go 与 RFC8785 固定向量一致", async () => {
    expect(canonical(parseStrict('{"b":2,"a":1}'))).toBe('{"a":1,"b":2}');
    expect(canonical(parseStrict('{"s":"<>&\\u2028","n":-0,"e":0.00000001}'))).toBe(
      '{"e":1e-8,"n":0,"s":"<>&\u2028"}',
    );
    expect(canonical(parseStrict('{"😀":1,"\\ufffd":2}'))).toBe('{"😀":1,"�":2}');
    expect(canonical(parseStrict("[0.000001,4.50,1e3]"))).toBe("[0.000001,4.5,1000]");
    expect(await sha256(new TextEncoder().encode("abc"))).toBe(
      "sha256:ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
    );
  });
});

it("在摘要前拒绝稀疏数组与非 JSON 值，不隐式转换", () => {
  expect(() => canonical(new Array(2))).toThrow();
  expect(() => canonical({ a: undefined })).toThrow();
  expect(() => canonical(new Date())).toThrow();
  expect(() => canonical({ secret: "\ud800" })).toThrow();
});

it("原键别名、重复键、孤立 Unicode 与不安全数字不能进入 Schema 解码", () => {
  for (const text of [
    '{"a":1,"a":2}',
    '{"a":1,"\\u0061":2}',
    '"\\ud800"',
    '"\\udc00"',
    "9007199254740992",
    "NaN",
    "{} {}",
    "[01]",
    '{"bad":"\u0000"}',
  ])
    expect(() => parseStrict(text)).toThrow();
  expect(() => parseStrict(new Uint8Array([34, 255, 34]))).toThrow();
  expect(canonical(parseStrict('{"__proto__":{"safe":true}}'))).toBe('{"__proto__":{"safe":true}}');
});
it("UTF-8 原字节不能静默丢弃 BOM；与 Go 一致拒绝 JSON 文件前缀", () => {
  const bytes = new Uint8Array([0xef, 0xbb, 0xbf, 0x7b, 0x7d]);
  expect(() => parseStrict(bytes)).toThrow(/invalid_json/);
  expect(parseStrict(new TextEncoder().encode('"\ufeff正文"'))).toBe("\ufeff正文");
});
