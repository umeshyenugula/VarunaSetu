import { beforeEach, describe, expect, it, vi } from 'vitest';
import { listReadings, verifyLedgerIntegrity } from './client';
import { ledgerEntries } from '../mocks/fixtures';

describe('api client (integration/fallback behavior)', () => {
  beforeEach(() => {
    vi.unstubAllEnvs();
    vi.restoreAllMocks();
  });

  it('falls back to SIMULATED fixtures when no API base is configured', async () => {
    const res = await listReadings();
    expect(res.source).toBe('simulated');
    expect(res.items.length).toBeGreaterThan(0);
    expect(res.items.every((r) => r.simulated)).toBe(true);
  });

  it('falls back to fixtures when fetch fails (missing endpoint)', async () => {
    vi.stubEnv('VITE_API_BASE', 'http://localhost:9');
    vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('unavailable'));
    const res = await listReadings();
    expect(res.source).toBe('simulated');
  });

  it('ledger integrity interface returns explicit simulated structural result', async () => {
    const res = await verifyLedgerIntegrity(ledgerEntries);
    expect(res.source).toBe('client-simulated');
    expect(res.ok).toBe(true);
  });
});
