import type { ErrorCode, PublicError } from './generated/values.ts';
// Only the closed PublicError value crosses the boundary; cause remains local.
export class ContractError extends Error implements PublicError {
  readonly code: ErrorCode;
  toPublicError(): PublicError {
    return { code: this.code };
  }
  constructor(code: ErrorCode, cause?: unknown) {
    super(code, { cause });
    this.name = 'ContractError';
    this.code = code;
  }
}
