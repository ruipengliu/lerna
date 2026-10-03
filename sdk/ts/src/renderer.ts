import type { ContentRef } from "./contracts.gen";
import { canonical, ProtocolError, sha256 } from "./json";
import { validateRecord } from "./schema";

export interface InlineRenderBody {
  content_ref: ContentRef;
  base64: string;
}

/** 正文属于当前 read 的准确引用；缺失缓存不得用 not_modified 猜测完整呈现。 */
export async function verifyRenderBodies(
  refs: readonly ContentRef[],
  bodies: readonly InlineRenderBody[],
): Promise<Array<{ ref: ContentRef; bytes: Uint8Array }>> {
  if (!refs.length || refs.length > 8 || bodies.length !== refs.length)
    throw new ProtocolError("render_bodies_incomplete");
  const required = refs.map((ref) => validateRecord<ContentRef>("ContentRef", ref));
  const keys = required.map((ref) => canonical(ref));
  if (new Set(keys).size !== keys.length) throw new ProtocolError("render_refs_duplicate");
  if (required.reduce((count, ref) => count + ref.byte_length, 0) > 128 * 1024)
    throw new ProtocolError("render_body_budget");
  const indexed = new Map<string, InlineRenderBody>();
  for (const body of bodies) {
    const ref = validateRecord<ContentRef>("ContentRef", body.content_ref);
    const key = canonical(ref);
    if (!keys.includes(key) || indexed.has(key))
      throw new ProtocolError("render_body_ref_mismatch");
    indexed.set(key, body);
  }
  return Promise.all(
    required.map(async (ref, index) => {
      const encoded = indexed.get(keys[index] ?? "")?.base64;
      if (
        typeof encoded !== "string" ||
        encoded.length !== 4 * Math.ceil(ref.byte_length / 3) ||
        !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(encoded)
      )
        throw new ProtocolError("render_base64_invalid");
      const decoded = atob(encoded);
      if (btoa(decoded) !== encoded) throw new ProtocolError("render_base64_invalid");
      const bytes = Uint8Array.from(decoded, (entry) => entry.charCodeAt(0));
      if (bytes.byteLength !== ref.byte_length || (await sha256(bytes)) !== ref.hash)
        throw new ProtocolError("render_body_digest_mismatch");
      return { ref, bytes };
    }),
  );
}
