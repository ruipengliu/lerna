import { createHmac, randomBytes } from "node:crypto";
import { canonical, parseStrict } from "@harness/sdk";
import type { Query } from "@harness/sdk";
import { reject } from "./error";
import type { Store } from "./store";
import { array, integer, text, object, type Principal, type Document } from "./types";

interface List {
  subject: string;
  generation: number;
  revision: number;
  expires: number;
  items: Document[];
}
export function initializeCursor(store: Store): void {
  if (!store.meta("cursor_key"))
    store.tx(() => store.meta("cursor_key", randomBytes(32).toString("hex")));
}
function encode(store: Store, id: string, pos: number): string {
  const body = Buffer.from(canonical([id, pos])).toString("base64url");
  return `${body}.${createHmac("sha256", store.meta("cursor_key")).update(body).digest("hex")}`;
}
export function operationPage(
  store: Store,
  p: Principal,
  q: Query,
  items: Document[],
  revision: number,
): Document {
  const input = object(q.payload),
    limit = integer(input.limit);
  if (limit < 1 || limit > 20) reject("invalid_request", "invalid_page_limit");
  let id = q.query_id,
    pos = 0;
  if (input.cursor) {
    const parts = text(input.cursor).split(".");
    if (
      parts.length !== 2 ||
      !parts[0] ||
      parts[1] !== createHmac("sha256", store.meta("cursor_key")).update(parts[0]).digest("hex")
    )
      reject("invalid_request", "invalid_cursor");
    const c = array(parseStrict(Buffer.from(parts[0], "base64url")));
    id = text(c[0]);
    pos = integer(c[1]);
  }
  return store.tx(() => {
    let snapshot = store.get<List>("operation_lists", id);
    if (!snapshot) {
      if (input.cursor) reject("cursor_expired", "snapshot_not_found");
      snapshot = {
        subject: p.subject_id,
        generation: p.generation,
        revision,
        expires: store.now() + 300000,
        items,
      };
      store.put("operation_lists", id, snapshot);
    }
    if (snapshot.subject !== p.subject_id || snapshot.generation !== p.generation)
      reject("forbidden", "snapshot_principal_mismatch");
    if (snapshot.expires <= store.now()) reject("cursor_expired", "snapshot_expired");
    if (snapshot.revision !== revision) reject("snapshot_required", "operation_collection_changed");
    const selected = snapshot.items.slice(pos, pos + limit),
      next = pos + selected.length,
      exhausted = next >= snapshot.items.length;
    return {
      items: selected,
      collection_revision: revision,
      exhausted,
      partial: false,
      gaps: [],
      ...(!exhausted ? { next_cursor: encode(store, id, next) } : {}),
    };
  });
}
