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
