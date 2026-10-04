import { decode, encode, version } from './codec.ts';
import { parseCommand, ContractError } from './commands.ts';
import { canonical } from './digest.ts';
import type {
  DecisionDecideRequest,
  DecisionGetRequest,
  DecisionCancelRequest,
  SubjectBinding,
} from './generated/values.ts';

function method(wire: string | Uint8Array, expected: string): void {
  const envelope = parseCommand(wire);
  if (envelope.contract_version !== version)
    throw new ContractError('version_unsupported');
  if (envelope.profile !== 'decision_engine' || envelope.method !== expected)
    throw new ContractError('unsupported');
}
export function decodeDecide(wire: string | Uint8Array): DecisionDecideRequest {
  method(wire, 'decision_engine.decide');
  return decode('DecisionDecideRequest', wire);
}
export function decodeGet(wire: string | Uint8Array): DecisionGetRequest {
  method(wire, 'decision_engine.get');
  return decode('DecisionGetRequest', wire);
}
export function decodeCancel(wire: string | Uint8Array): DecisionCancelRequest {
  method(wire, 'decision_engine.cancel');
  return decode('DecisionCancelRequest', wire);
}
export const decisionInputDigestAlgorithm = 'lerna-decision-input-1';
export async function decisionInputDigest(
  request: DecisionDecideRequest,
  subject: SubjectBinding,
): Promise<string> {
  const fixed = decodeDecide(encode('DecisionDecideRequest', request));
  const principal = decode('SubjectBinding', encode('SubjectBinding', subject));
  const content = {
    contract_version: fixed.contract_version,
    profile: fixed.profile,
    task_ref: fixed.payload.task_ref,
    snapshot_ref: fixed.payload.snapshot_ref,
    component_ref: fixed.payload.component_ref,
    use_refs: fixed.payload.use_refs,
    limits: fixed.payload.limits,
    deadline: fixed.payload.deadline,
    subject_binding: principal,
  };
  const hash = await globalThis.crypto.subtle.digest(
    'SHA-256',
    new TextEncoder().encode(
      decisionInputDigestAlgorithm + '\n' + canonical(content),
    ),
  );
  return (
    'sha256:' +
    Array.from(new Uint8Array(hash), (b) =>
      b.toString(16).padStart(2, '0'),
    ).join('')
  );
}
function object(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? Object.fromEntries(Object.entries(value))
    : {};
}
function list(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}
function identity(a: unknown, b: unknown): boolean {
  const x = object(a),
    y = object(b);
  return ['tenant_id', 'owner_id', 'kind', 'id'].every((k) => x[k] === y[k]);
}
function unique(value: unknown, key?: string): boolean {
  const items = list(value).map((x) =>
    canonical(
      key === 'requirement_ref' || key === 'replaces_ref'
        ? ['tenant_id', 'owner_id', 'kind', 'id'].map(
            (k) => object(object(x)[key])[k],
          )
        : key
          ? object(x)[key]
          : x,
    ),
  );
  return new Set(items).size === items.length;
}
export function decisionSemantics(name: string, value: unknown): boolean {
  const v = object(value);
  switch (name) {
    case 'DecisionLimits':
      return (
        Object.entries({
          max_input_bytes: 1048576n,
          max_output_bytes: 1048576n,
          max_rule_steps: 1024n,
          max_actions: 4n,
        }).every(
          ([key, max]) => typeof v[key] === 'string' && BigInt(v[key]) <= max,
        ) && String(object(v.max_cost).unit).startsWith('fixture')
      );
    case 'DecisionUsage':
      return (
        v.model_requests === '0' &&
        String(object(v.cost).unit).startsWith('fixture')
      );
    case 'DecisionDecidePayload':
      return (
        decisionSemantics('DecisionLimits', v.limits) && unique(v.use_refs)
      );
    case 'DecisionDecideRequest':
      return (
        object(v.target).id === object(v.payload).decision_id &&
        decisionSemantics('DecisionDecidePayload', v.payload)
      );
    case 'DecisionGetRequest':
    case 'DecisionCancelRequest':
      return identity(v.target, object(v.payload).decision_ref);
    case 'ProposalEvidence':
      return unique(v.evidence_refs);
    case 'RequirementDelta':
    case 'ProposalAction':
      return unique(v.source_refs);
    case 'Proposal':
      return (
        unique(v.processed_source_refs) &&
        unique(v.requirement_delta, 'local_key') &&
        (v.disclosed_source_refs === undefined ||
          unique(v.disclosed_source_refs)) &&
        unique(
          list(v.requirement_delta).filter(
            (x) => object(x).replaces_ref !== undefined,
          ),
          'replaces_ref',
        ) &&
        list(v.requirement_delta).every((x) =>
          decisionSemantics('RequirementDelta', x),
        ) &&
        decisionSemantics('ProposalAdvance', v.advance)
      );
    case 'ProposalAdvance':
    case 'ProposalAdvanceActions':
    case 'ProposalAdvanceCandidateResult':
    case 'ProposalAdvanceInputRequest':
    case 'ProposalAdvanceCannotContinue':
      switch (v.kind) {
        case 'actions':
          return (
            unique(v.actions, 'local_key') &&
            list(v.actions).every((x) => decisionSemantics('ProposalAction', x))
          );
        case 'candidate_result':
          return (
            unique(v.artifact_refs) &&
            unique(v.evidence, 'requirement_ref') &&
            list(v.evidence).every((x) => unique(object(x).evidence_refs))
          );
        case 'input_request':
          return unique(v.preview_refs);
        case 'cannot_continue':
          return (
            unique(v.missing_requirements) &&
            (v.artifact_refs === undefined || unique(v.artifact_refs))
          );
      }
      return true;
    case 'Decision':
    case 'DecisionAccepted':
    case 'DecisionRunning':
    case 'DecisionWaiting':
    case 'DecisionCompleted':
    case 'DecisionFailed':
    case 'DecisionCancelled':
      if (!decisionSemantics('DecisionUsage', v.usage)) return false;
      if (
        v.input !== undefined &&
        (object(v.decision_ref).id !== object(v.input).decision_id ||
          !decisionSemantics('DecisionDecidePayload', v.input))
      )
        return false;
      if (
        v.status === 'cancelled' &&
        v.input !== undefined &&
        canonical(v.task_ref) !== canonical(object(v.input).task_ref)
      )
        return false;
      if (v.status === 'completed')
        return (
          unique(v.artifact_refs) &&
          identity(v.decision_ref, object(v.proposal).decision_ref) &&
          canonical(object(v.input).snapshot_ref) ===
            canonical(object(v.proposal).snapshot_ref) &&
          decisionSemantics('Proposal', v.proposal)
        );
      return true;
    case 'DecisionGetResponse':
    case 'DecisionGetResponseFound':
      return (
        v.status !== 'found' ||
        (identity(v.decision_ref, object(v.decision).decision_ref) &&
          (v.current_control === undefined ||
            (object(v.current_control).decision_input_digest ===
              object(v.decision).input_digest &&
              canonical(object(v.current_control).task_ref) ===
                canonical(
                  object(v.decision).input === undefined
                    ? object(v.decision).task_ref
                    : object(object(v.decision).input).task_ref,
                ))) &&
          decisionSemantics('Decision', v.decision))
      );
  }
  return true;
}
