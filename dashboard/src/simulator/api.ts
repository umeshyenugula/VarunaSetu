import { signTelemetry, randomNonce, type SignedTelemetryEnvelope } from './crypto';

import { apiBase } from '../config';

export type ApiCall = { at: number; method: string; url: string; status: number; ms: number; ok: boolean };
const listeners = new Set<(call: ApiCall) => void>();
/** Subscribe to every request the story makes (used by the on-screen request log). */
export function onApiCall(fn: (call: ApiCall) => void): () => void {
  listeners.add(fn);
  return () => { listeners.delete(fn); };
}

/** Absolute URL a path will be requested from, so the real target is always visible. */
export function resolvedUrl(path: string): string {
  const url = `${apiBase()}${path}`;
  return /^https?:/.test(url) ? url : `${window.location.origin}${url}`;
}

async function json<T>(path: string, init?: RequestInit): Promise<T> {
  const method = init?.method ?? 'GET';
  const started = performance.now();
  const emit = (status: number, ok: boolean) => listeners.forEach((fn) => fn({ at: Date.now(), method, url: resolvedUrl(path), status, ms: Math.round(performance.now() - started), ok }));
  let res: Response;
  try {
    res = await fetch(`${apiBase()}${path}`, {
      ...init,
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
    });
  } catch {
    emit(0, false);
    throw new Error(`Cannot reach ${resolvedUrl(path)}. Is the backend running on port 8080?`);
  }
  const text = await res.text();
  let data: any = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  emit(res.status, res.ok);
  if (!res.ok) throw new Error(typeof data?.error === 'string' ? `${data.error} (HTTP ${res.status})` : `${method} ${path} returned HTTP ${res.status}`);
  return data as T;
}

export type Health = { status?: string };
export const backend = {
  health: () => json<Health>('/healthz'),
  analytics: () => json<{ totalReadings: number; accepted: number; provisional: number; quarantined: number }>('/analytics/summary'),
  ledger: () => json<unknown[] | null>('/ledger/entries?limit=500'),
  escrows: () => json<unknown[] | null>('/escrows?limit=500'),
};

export type Verdict = {
  id: string;
  decision: { State: 'ACCEPT' | 'PROVISIONAL_ESCROW' | 'QUARANTINE'; Reason: string; RObsMM: number; RMaxMM: number; PeerCount: number };
};

export type ProvisionDevice = {
  deviceId: string; buildingId: string; h3Index: string;
  publicKeyBase64: string; privateSeedBase64: string;
  roofAreaM2: number; runoffCoeff: number; capacityLitres: number;
};
export type SimulatorCaseDef = {
  id: string; title: string; category: string; scenario: string; expected: string;
  existingGap: string; howHydroLedger: string; h3Role: string; apiCalls: string[];
  notes?: string; autoRunnable: boolean; requiresTimeLapse?: boolean;
};
export type ProvisionResponse = {
  simulated: boolean; cityName: string; h3Resolution: number; cells: string[];
  buildings: Array<{ buildingId: string; name: string; h3Index: string; roofAreaM2: number; runoffCoeff: number; capacityLitres: number }>;
  devices: ProvisionDevice[]; cases: SimulatorCaseDef[];
};

export const simulatorApi = {
  config: () => json<any>('/simulator/config'),
  cases: () => json<{ cases: SimulatorCaseDef[] }>('/simulator/cases'),
  provision: () => json<ProvisionResponse>('/simulator/provision', { method: 'POST', body: '{}' }),
  reset: () => json<any>('/simulator/reset', { method: 'POST', body: '{}' }),
  expireEscrows: (advanceMinutes = 20) => json<any>('/simulator/expire-escrows', { method: 'POST', body: JSON.stringify({ advanceMinutes }) }),
};

export type TelemetryPayload = {
  building_id: string;
  claimed_litres: number;
  roof_area_m2: number;
  runoff_coeff: number;
  stored_litres: number;
  capacity_litres: number;
  radar_mm: number;
  h3_index: string;
};

export async function submitSignedTelemetry(device: ProvisionDevice, payload: TelemetryPayload, at = new Date()): Promise<Verdict> {
  const envelope: SignedTelemetryEnvelope = await signTelemetry({
    deviceId: device.deviceId,
    privateSeedBase64: device.privateSeedBase64,
    timestamp: at,
    nonce: randomNonce(),
    payload: payload as any,
  });
  return json<Verdict>('/telemetry/readings', { method: 'POST', body: JSON.stringify(envelope) });
}
