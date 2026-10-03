// Called only after the closed machine schema accepts the object shape.
// Structural access here does not create another field validator.
function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value))
    throw new Error('expected object');
  return value as Record<string, unknown>;
}
function binding(value: Record<string, unknown>): boolean {
  const ref = object(value.object_ref);
  return (
    value.revision === undefined ||
    ref.revision === undefined ||
    value.revision === ref.revision
  );
}
export function sameCommandRef(a: unknown, b: unknown): boolean {
  const l = object(a),
    r = object(b),
    lo = object(l.owner),
    ro = object(r.owner);
  return (
    l.command_id === r.command_id &&
    lo.tenant_id === ro.tenant_id &&
    lo.owner_id === ro.owner_id
  );
}
export function responseSemantics(name: string, value: unknown): boolean {
  if (
    ![
      'CommandReceipt',
      'CommandReceiptAccepted',
      'CommandReceiptApplied',
      'CommandReceiptRejected',
      'CommandProgress',
      'CommandProgressTask',
      'CommandGetResponse',
      'CommandGetResponseFound',
      'TransportOutcome',
      'TransportOutcomeReceived',
    ].includes(name)
  )
    return true;
  const v = object(value);
  if (name === 'TransportOutcome' || name === 'TransportOutcomeReceived')
    return (
      v.status !== 'received' || responseSemantics('CommandReceipt', v.receipt)
    );
  if (name.startsWith('CommandReceipt'))
    return v.state === 'rejected' || binding(v);
  if (name.startsWith('CommandProgress'))
    return v.kind !== 'task' || binding(v);
  if (v.status !== 'found') return true;
  const receipt = object(v.receipt),
    progress = object(v.progress);
  if (
    !sameCommandRef(v.command_ref, receipt.command_ref) ||
    !responseSemantics('CommandReceipt', receipt) ||
    !responseSemantics('CommandProgress', progress)
  )
    return false;
  if (progress.kind !== 'task') return true;
  if (receipt.object_ref === undefined) return false;
  const old = object(receipt.object_ref),
    current = object(progress.object_ref);
  if (
    !['tenant_id', 'owner_id', 'kind', 'id'].every((k) => old[k] === current[k])
  )
    return false;
  const baseline = receipt.revision ?? old.revision;
  return (
    baseline === undefined ||
    (typeof baseline === 'string' &&
      typeof progress.revision === 'string' &&
      BigInt(progress.revision) >= BigInt(baseline))
  );
}
