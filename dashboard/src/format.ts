export function fmt(ts?: string | null): string {
  if (!ts) return '—';
  const date = new Date(ts);
  return Number.isNaN(date.getTime()) ? ts : date.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit', second: '2-digit' });
}

export function shortId(id: string, keep = 8): string {
  return id.length > keep + 1 ? `${id.slice(0, keep)}…` : id;
}
