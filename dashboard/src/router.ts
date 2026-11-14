import { useMemo, useSyncExternalStore } from 'react';

/**
 * Tiny hash router with dynamic params.
 *
 *   #/docs/documentation/verification?at=reason-codes
 *     segments = ['docs', 'documentation', 'verification']   (path params, by position)
 *     query    = { at: 'reason-codes' }                       (query params)
 *
 * Hash routing keeps the dashboard deployable on any static host and keeps the
 * simulator's "?dashboard=<origin>" return link working, since the real query string is never touched.
 */
export interface Loc { segments: string[]; query: URLSearchParams }

const listeners = new Set<() => void>();
const notify = () => listeners.forEach((l) => l());

function subscribe(cb: () => void) {
  listeners.add(cb);
  window.addEventListener('hashchange', cb);
  return () => { listeners.delete(cb); window.removeEventListener('hashchange', cb); };
}

const decode = (s: string) => { try { return decodeURIComponent(s); } catch { return s; } };

export function parseHash(hash: string): Loc {
  const raw = hash.replace(/^#\/?/, '');
  const q = raw.indexOf('?');
  const path = q === -1 ? raw : raw.slice(0, q);
  return { segments: path.split('/').filter(Boolean).map(decode), query: new URLSearchParams(q === -1 ? '' : raw.slice(q + 1)) };
}

/** Empty, null and undefined query values are dropped, so defaults never clutter the URL. */
export function buildHash(segments: string[], query: Record<string, string | null | undefined> = {}): string {
  const qs = new URLSearchParams();
  for (const [k, v] of Object.entries(query)) if (v) qs.set(k, v);
  const s = qs.toString();
  return `#/${segments.map(encodeURIComponent).join('/')}${s ? `?${s}` : ''}`;
}

export function navigate(segments: string[], query: Record<string, string | null | undefined> = {}, replace = false) {
  const hash = buildHash(segments, query);
  if (hash === window.location.hash) return;
  if (replace) { window.history.replaceState(null, '', hash); notify(); } else window.location.hash = hash;
}

/** Merge `patch` into the current query string. A null or empty value removes the key. */
export function setQuery(patch: Record<string, string | null>, replace = true) {
  const { segments, query } = parseHash(window.location.hash);
  const next: Record<string, string> = Object.fromEntries(query);
  for (const [k, v] of Object.entries(patch)) { if (v) next[k] = v; else delete next[k]; }
  navigate(segments, next, replace);
}

/** The current location; re-renders on every hash change (including replaceState navigation). */
export function useRoute(): Loc {
  const hash = useSyncExternalStore(subscribe, () => window.location.hash, () => '');
  return useMemo(() => parseHash(hash), [hash]);
}
