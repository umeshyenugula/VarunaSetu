import { signTelemetry, randomNonce, type SignedTelemetryEnvelope } from './crypto';

function base(): string {
  const value = import.meta.env.VITE_API_BASE?.trim().replace(/\/$/, '');
  if (!value) throw new Error('VITE_API_BASE is required');
  return value;
}

async function json<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${base()}${path}`, {
    ...init,
    headers: { Accept: 'application/json', 'Content-Type': 'application/json', ...(init?.headers ?? {}) },
  });
  const text = await res.text();
  let data: any = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = text; }
  if (!res.ok) throw new Error(typeof data?.error === 'string' ? `${data.error} (HTTP ${res.status})` : `${path} returned HTTP ${res.status}`);
  return data as T;
}

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

export async function submitSignedTelemetry(device: ProvisionDevice, payload: TelemetryPayload, at = new Date()): Promise<any> {
  const envelope: SignedTelemetryEnvelope = await signTelemetry({
    deviceId: device.deviceId,
    privateSeedBase64: device.privateSeedBase64,
    timestamp: at,
    nonce: randomNonce(),
    payload: payload as any,
  });
  return json<any>('/telemetry/readings', { method: 'POST', body: JSON.stringify(envelope) });
}
