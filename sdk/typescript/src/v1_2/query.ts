import { decode, encode } from './codec.ts';
import { decodeCommand } from './commands.ts';
import { readCommandFacts, type CommandFactReader } from './readfacts.ts';
import type {
  CommandGetResponse,
  CommandRef,
  OwnerRef,
  SubjectBinding,
} from './generated/values.ts';

/** Host-authenticated principal and exact-reference authorization. No existence lookup is required. */
export interface ReadAuthorizer {
  authorizeCommandRead(
    signal: AbortSignal,
    trusted: SubjectBinding,
    original: CommandRef,
  ): Promise<boolean>;
}
/** A host-injected physical read route; replacing it never changes logical identity. */
export interface ResolvedCommandOwner {
  owner: OwnerRef;
  reader: CommandFactReader;
}
export interface OwnerDirectory {
  resolveCommandOwner(
    signal: AbortSignal,
    owner: OwnerRef,
  ): Promise<ResolvedCommandOwner>;
}

export interface CommandReadOptions {
  /** Host resource bound for the entire read, 1..60000 integer milliseconds. */
  maxReadDurationMs: number;
}

/** Authenticated Application / Component command.get; trusted comes from the host, never the payload.
 * Ports must honor cancellation. The finite host read bound is independent of the start cutoff. */
export async function getCommand(
  signal: AbortSignal,
  wire: string | Uint8Array,
  trusted: SubjectBinding | undefined,
  authorizer: ReadAuthorizer,
  directory: OwnerDirectory,
  now: () => string,
  options: CommandReadOptions,
): Promise<CommandGetResponse> {
  const request = decodeCommand(wire),
    original = request.payload.command_ref;
  // Freeze mutable caller bytes before the first callback/await.
  const frozenWire = encode('CommandGetRequest', request);
  const unavailable = (): CommandGetResponse => ({
    status: 'unavailable',
    command_ref: structuredClone(original),
    reason: 'dependency_unavailable',
  });
  const forbidden: CommandGetResponse = {
    status: 'rejected',
    reason: 'forbidden',
  };
  if (!trusted || !authorizer) return forbidden;
  let principal: SubjectBinding;
  try {
    principal = decode('SubjectBinding', encode('SubjectBinding', trusted));
  } catch {
    return forbidden;
  }
  if (principal.tenant_id !== original.owner.tenant_id) return forbidden;
  let started: string;
  try {
    started = decode('Time', JSON.stringify(now()));
  } catch {
    return unavailable();
  }
  if (started >= request.accept_before)
    return { status: 'rejected', reason: 'expired' };
  if (
    signal.aborted ||
    !options ||
    !Number.isInteger(options.maxReadDurationMs) ||
    options.maxReadDurationMs < 1 ||
    options.maxReadDurationMs > 60_000
  )
    return unavailable();
  const controller = new AbortController();
  const forward = () => controller.abort();
  signal.addEventListener('abort', forward, { once: true });
  const timer = setTimeout(() => controller.abort(), options.maxReadDurationMs);
  let stop: () => void = () => {};
  const canceled = new Promise<CommandGetResponse>((resolve) => {
    stop = () => resolve(unavailable());
    controller.signal.addEventListener('abort', stop, { once: true });
  });
  const read = async (): Promise<CommandGetResponse> => {
    let allowed: boolean;
    try {
      allowed = await authorizer.authorizeCommandRead(
        controller.signal,
        structuredClone(principal),
        structuredClone(original),
      );
    } catch {
      return controller.signal.aborted ? unavailable() : forbidden;
    }
    if (controller.signal.aborted) return unavailable();
    if (allowed !== true) return forbidden;
    if (controller.signal.aborted || !directory) return unavailable();
    try {
      const route = await directory.resolveCommandOwner(
        controller.signal,
        structuredClone(original.owner),
      );
      if (
        controller.signal.aborted ||
        !route ||
        route.owner.tenant_id !== original.owner.tenant_id ||
        route.owner.owner_id !== original.owner.owner_id ||
        !route.reader
      )
        return unavailable();
      return await readCommandFacts(
        controller.signal,
        frozenWire,
        route.reader,
        () => started,
      );
    } catch {
      return unavailable();
    }
  };
  try {
    return await Promise.race([read(), canceled]);
  } finally {
    clearTimeout(timer);
    signal.removeEventListener('abort', forward);
    controller.signal.removeEventListener('abort', stop);
  }
}
