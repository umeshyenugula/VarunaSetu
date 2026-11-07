import { escrows, getCellsFromReadings, ledgerEntries, readings } from '../mocks/fixtures';
import type { AnalyticsSummary, CellState, EscrowRecord, LedgerEntryView, TelemetryReading } from '../types';

/**
 * API client. Missing backend endpoints are recorded under REQUESTED CHANGES in
 * HANDOFF.md; this client uses clearly labeled SIMULATED mock fixtures when an
 * endpoint is unavailable. It never invents backend functionality.
 */
function base() {
  return import.meta.env.VITE_API_BASE?.replace(/\/$/, '') || '';
}

async function tryFetch<T>(_path: string): Promise<T | null> {
  const b = base();
  if (!b) return null;
  try {
    const res = await fetch(`${b}${_path}`);
    if (!res.ok) return null;
    return (await res.json()) as T;
  } catch {
    return null;
  }
}

export async function listReadings(): Promise<{ items: TelemetryReading[]; source: 'api' | 'simulated' }> {
  const api = await tryFetch<TelemetryReading[]>('/telemetry/readings');
  if (api) return { items: api, source: 'api' };
  return { items: readings, source: 'simulated' };
}

export async function listEscrows(): Promise<{ items: EscrowRecord[]; source: 'api' | 'simulated' }> {
  const api = await tryFetch<EscrowRecord[]>('/escrows');
  if (api) return { items: api, source: 'api' };
  return { items: escrows, source: 'simulated' };
}

export async function listLedger(): Promise<{ items: LedgerEntryView[]; source: 'api' | 'simulated' }> {
  const api = await tryFetch<LedgerEntryView[]>('/ledger/entries');
  if (api) return { items: api, source: 'api' };
  return { items: ledgerEntries, source: 'simulated' };
}

export async function listCells(): Promise<{ items: CellState[]; source: 'api' | 'simulated' }> {
  const api = await tryFetch<CellState[]>('/cells');
  if (api) return { items: api, source: 'api' };
  return { items: getCellsFromReadings(readings), source: 'simulated' };
}

export async function verifyLedgerIntegrity(items: LedgerEntryView[]): Promise<{ ok: boolean; message: string; source: 'client-simulated' | 'api' }> {
  // Phase 4 ledger canonical hashing is implemented in Go; no backend verify endpoint exists.
  // Client-side check here is a structural simulation for the demo UI only.
  for (let i = 1; i < items.length; i++) {
    if (items[i].prevHash !== items[i].hash && items[i - 1].hash !== items[i].prevHash) {
      return { ok: false, message: `SIMULATED structural check failed at seq ${items[i].seq}: prev_hash linkage mismatch.`, source: 'client-simulated' };
    }
  }
  return { ok: true, message: 'SIMULATED structural linkage check passed in browser. Authoritative verification uses Phase 4 Go ledger.', source: 'client-simulated' };
}

export function computeAnalytics(items: TelemetryReading[], escrowItems: EscrowRecord[]): AnalyticsSummary {
  const total = items.length;
  const accepted = items.filter((r) => r.decision === 'ACCEPT').length;
  const provisional = items.filter((r) => r.decision === 'PROVISIONAL_ESCROW').length;
  const quarantined = items.filter((r) => r.decision === 'QUARANTINE').length;
  const resolved = escrowItems.filter((e) => e.state !== 'PENDING').length;
  const devices = new Set(items.map((r) => r.deviceId)).size;
  return {
    totalReadings: total, accepted, provisional, quarantined,
    escrowResolutionRate: escrowItems.length ? resolved / escrowItems.length : null,
    activeDevices: devices,
    simulatedDataPresent: items.some((r) => r.simulated) || escrowItems.some((e) => e.simulated),
  };
}
