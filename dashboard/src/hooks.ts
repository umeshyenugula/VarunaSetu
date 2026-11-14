import { useCallback, useEffect, useState } from 'react';
import { getAnalytics, listCells, listEscrows, listLedger, listReadings } from './api/client';
import type { AnalyticsSummary, CellState, EscrowRecord, LedgerEntryView, TelemetryReading } from './types';

export interface LiveData {
  readings: TelemetryReading[];
  escrows: EscrowRecord[];
  ledger: LedgerEntryView[];
  cells: CellState[];
  analytics: AnalyticsSummary | null;
  loading: boolean;
  error: string;
  refresh: () => void;
}

/**
 * Loads every monitoring projection. Pass the open screen's id as `screen` to reload
 * each time a monitoring screen is opened (so data created by a story is never stale),
 * or null while a non-monitoring screen is showing.
 */
export function useLiveData(screen: string | null): LiveData {
  const [state, setState] = useState({
    readings: [] as TelemetryReading[], escrows: [] as EscrowRecord[], ledger: [] as LedgerEntryView[],
    cells: [] as CellState[], analytics: null as AnalyticsSummary | null,
  });
  const [loadedKey, setLoadedKey] = useState('');
  const [error, setError] = useState('');
  const [tick, setTick] = useState(0);
  const key = screen === null ? '' : `${screen}:${tick}`;

  useEffect(() => {
    if (!key) return;
    let active = true;
    Promise.all([listReadings(), listEscrows(), listLedger(), listCells(), getAnalytics()])
      .then(([readings, escrows, ledger, cells, analytics]) => {
        if (!active) return;
        setState({ readings, escrows, ledger, cells, analytics });
        setError('');
      })
      .catch((cause: unknown) => { if (active) setError(cause instanceof Error ? cause.message : 'Dashboard data could not be loaded'); })
      .finally(() => { if (active) setLoadedKey(key); });
    return () => { active = false; };
  }, [key]);

  const refresh = useCallback(() => setTick((n) => n + 1), []);
  return { ...state, loading: key !== '' && loadedKey !== key, error, refresh };
}

/** Human readable time left until `target`, ticking every second. */
export function useCountdown(target?: string): string {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  if (!target) return '—';
  const remaining = new Date(target).getTime() - now;
  if (Number.isNaN(remaining)) return '—';
  if (remaining <= 0) return 'Expired';
  const seconds = Math.floor(remaining / 1000);
  return `${Math.floor(seconds / 60)}m ${String(seconds % 60).padStart(2, '0')}s`;
}
