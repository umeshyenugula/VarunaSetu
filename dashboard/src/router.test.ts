import { describe, expect, it } from 'vitest';
import { buildHash, parseHash, setQuery } from './router';

describe('hash router', () => {
  it('splits path params and query params', () => {
    const loc = parseHash('#/docs/documentation/verification?at=reason-codes&q=escrow%20ttl');
    expect(loc.segments).toEqual(['docs', 'documentation', 'verification']);
    expect(loc.query.get('at')).toBe('reason-codes');
    expect(loc.query.get('q')).toBe('escrow ttl');
  });

  it('accepts the legacy #name form and an empty hash', () => {
    expect(parseHash('#overview').segments).toEqual(['overview']);
    expect(parseHash('').segments).toEqual([]);
  });

  it('survives malformed escapes', () => {
    expect(parseHash('#/map/%E0%A4%A').segments).toEqual(['map', '%E0%A4%A']);
  });

  it('round-trips and drops empty query values', () => {
    const hash = buildHash(['readings', 'r 1'], { decision: 'QUARANTINE', q: '', page: null, sort: undefined });
    expect(hash).toBe('#/readings/r%201?decision=QUARANTINE');
    expect(parseHash(hash).segments).toEqual(['readings', 'r 1']);
  });

  it('setQuery merges into the current route and removes null keys', () => {
    window.location.hash = '#/readings?q=abc&page=3';
    setQuery({ page: null, decision: 'ACCEPT' });
    expect(window.location.hash).toBe('#/readings?q=abc&decision=ACCEPT');
  });
});
