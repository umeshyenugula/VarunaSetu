import { beforeEach, describe, expect, it, vi } from 'vitest';
import { getAnalytics, listReadings } from './client';

describe('dashboard API client', () => {
  beforeEach(() => {
    vi.stubEnv('VITE_API_BASE', 'http://api.test');
    vi.restoreAllMocks();
  });

  it('requests persisted readings from the configured API', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify([{ id: 'r1' }]), { status: 200 }));
    await expect(listReadings()).resolves.toEqual([{ id: 'r1' }]);
    expect(fetch).toHaveBeenCalledWith('http://api.test/telemetry/readings?limit=500', { headers: { Accept: 'application/json' } });
  });

  it('reports an unavailable API instead of fabricating data', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('', { status: 503 }));
    await expect(listReadings()).rejects.toThrow('HTTP 503');
  });

  it('loads backend analytics without client-side replacement values', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ totalReadings: 4 }), { status: 200 }));
    await expect(getAnalytics()).resolves.toEqual({ totalReadings: 4 });
  });
});
