// Initial development diagnostics; this module does not implement harness/1 WSS or recovery.
export interface HostStatus {
  protocol: 'lerna-dev-status/1';
  role: string;
  instance_id: string;
  boot_id: string;
  phase: 'skeleton' | 'draining';
  accepting_tasks: boolean;
  dependencies: Record<string, boolean>;
  missing: string[];
}

export async function getHostStatus(baseURL: string, signal?: AbortSignal): Promise<HostStatus> {
  const response = await fetch(`${baseURL.replace(/\/$/, '')}/status`, { signal, cache: 'no-store' });
  if (!response.ok) throw new Error(`Host diagnostic HTTP status ${response.status}`);
  const value: unknown = await response.json();
  if (!isHostStatus(value)) throw new Error('Unsupported development host diagnostic response');
  return value;
}

function isHostStatus(value: unknown): value is HostStatus {
  if (typeof value !== 'object' || value === null) return false;
  const status = value as Record<string, unknown>;
  return status.protocol === 'lerna-dev-status/1'
    && typeof status.role === 'string'
    && typeof status.instance_id === 'string'
    && typeof status.boot_id === 'string'
    && (status.phase === 'skeleton' || status.phase === 'draining')
    && typeof status.accepting_tasks === 'boolean'
    && typeof status.dependencies === 'object' && status.dependencies !== null
    && !Array.isArray(status.dependencies)
    && Object.values(status.dependencies).every(value => typeof value === 'boolean')
    && Array.isArray(status.missing) && status.missing.every(value => typeof value === 'string');
}
