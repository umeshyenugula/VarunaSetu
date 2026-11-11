import type { AnalyticsSummary, CellState, EscrowRecord, LedgerEntryView, TelemetryReading } from '../types';

import { apiBase } from '../config';

const base = apiBase;

async function fetchJSON<T>(path: string): Promise<T> {
  const response = await fetch(`${base()}${path}`, {
    headers: { Accept: 'application/json' },
  });
  if (!response.ok) {
    throw new Error(`${path} returned HTTP ${response.status}`);
  }
  return (await response.json()) as T;
}

async function fetchList<T>(path: string): Promise<T[]> {
  const value = await fetchJSON<T[] | null>(path);
  return value ?? [];
}

export function listReadings(): Promise<TelemetryReading[]> {
  return fetchList('/telemetry/readings?limit=500');
}

export function listEscrows(): Promise<EscrowRecord[]> {
  return fetchList('/escrows?limit=500');
}

export function listLedger(): Promise<LedgerEntryView[]> {
  return fetchList('/ledger/entries?limit=500');
}

export function listCells(): Promise<CellState[]> {
  return fetchList('/cells?limit=500');
}

export function getAnalytics(): Promise<AnalyticsSummary> {
  return fetchJSON('/analytics/summary');
}
