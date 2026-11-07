import type { AnalyticsSummary, CellState, EscrowRecord, LedgerEntryView, TelemetryReading } from '../types';

function base(): string {
  const value = import.meta.env.VITE_API_BASE?.trim().replace(/\/$/, '');
  if (!value) {
    throw new Error('VITE_API_BASE is required');
  }
  return value;
}

async function fetchJSON<T>(path: string): Promise<T> {
  const response = await fetch(`${base()}${path}`, {
    headers: { Accept: 'application/json' },
  });
  if (!response.ok) {
    throw new Error(`${path} returned HTTP ${response.status}`);
  }
  return (await response.json()) as T;
}

export function listReadings(): Promise<TelemetryReading[]> {
  return fetchJSON('/telemetry/readings?limit=500');
}

export function listEscrows(): Promise<EscrowRecord[]> {
  return fetchJSON('/escrows?limit=500');
}

export function listLedger(): Promise<LedgerEntryView[]> {
  return fetchJSON('/ledger/entries?limit=500');
}

export function listCells(): Promise<CellState[]> {
  return fetchJSON('/cells?limit=500');
}

export function getAnalytics(): Promise<AnalyticsSummary> {
  return fetchJSON('/analytics/summary');
}
