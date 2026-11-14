// Single source of truth for where the browser sends API requests.
//
// - VITE_API_BASE set  -> use it as-is (e.g. http://localhost:8080 in Docker/prod).
// - VITE_API_BASE empty -> use same-origin "/api", which the Vite dev/preview
//   server proxies to the backend (default http://localhost:8080, see vite.config.ts).
//
// The backend (cmd/server) listens on 8080. 8090 is only the standalone
// citysim demo; nothing in the dashboard should ever point there.
export function apiBase(): string {
  const value = import.meta.env.VITE_API_BASE?.trim().replace(/\/+$/, '');
  return value || '/api';
}

/** The backend address a request path really reaches (the proxy target when base is "/api"). */
export function backendUrl(path = ''): string {
  const base = apiBase();
  if (!base.startsWith('/')) return `${base}${path}`;
  const target = (import.meta.env.VITE_PROXY_TARGET?.trim() || 'http://localhost:8080').replace(/\/+$/, '');
  return `${target}${path}`;
}
