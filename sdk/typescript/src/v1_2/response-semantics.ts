import { canonical } from './digest.ts';
type ObjectValue = Record<string, unknown>;
function object(v: unknown): ObjectValue {
  return v as ObjectValue;
}
function equal(a: unknown, b: unknown): boolean {
  return canonical(a) === canonical(b);
}
export function responseSemantics(name: string, value: unknown): boolean {
  if (value === null || typeof value !== 'object') return true;
  const o = object(value);
  if (name.startsWith('TransportOutcome') && o.status === 'received')
    return responseSemantics('CommandReceipt', o.receipt);
  if (name.startsWith('CommandReceipt') && o.state === 'accepted') {
    const t = object(o.object_ref),
      r = object(o.content_ref),
      owner = object(r.owner);
    return (
      t.kind === 'content' &&
      t.tenant_id === owner.tenant_id &&
      t.owner_id === owner.owner_id &&
      t.id === r.content_id
    );
  }
  if (name.startsWith('CommandGetResponse') && o.status === 'found') {
    const receipt = object(o.receipt),
      progress = object(o.progress);
    if (
      !equal(o.command_ref, receipt.command_ref) ||
      !responseSemantics('CommandReceipt', receipt)
    )
      return false;
    if (receipt.state === 'rejected' && progress.kind !== 'none') return false;
    if (receipt.state === 'accepted' && progress.kind === 'none') return false;
    if (
      progress.kind === 'content' &&
      !equal(receipt.content_ref, progress.content_ref)
    )
      return false;
  }
  return true;
}
export function sameCommandRef(left: unknown, right: unknown): boolean {
  return equal(left, right);
}
