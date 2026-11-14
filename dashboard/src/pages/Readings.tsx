import { useMemo, useState } from 'react';
import type { LiveData } from '../hooks';
import { fmt } from '../format';
import type { TelemetryReading } from '../types';
import { Badge, Button, Card, Empty, TableWrap } from '../ui';

type SortKey = keyof TelemetryReading;
const PAGE = 10;

export default function Readings({ data }: { data: LiveData }) {
  const [query, setQuery] = useState('');
  const [decision, setDecision] = useState('all');
  const [sortKey, setSortKey] = useState<SortKey>('timestamp');
  const [dir, setDir] = useState<'asc' | 'desc'>('desc');
  const [page, setPage] = useState(1);

  const rows = useMemo(() => {
    const q = query.toLowerCase();
    const filtered = data.readings.filter((r) => (decision === 'all' || r.decision === decision) && (!q || [r.id, r.deviceId, r.h3Index, r.reason].join(' ').toLowerCase().includes(q)));
    return filtered.sort((a, b) => {
      const c = String(a[sortKey] ?? '').localeCompare(String(b[sortKey] ?? ''), undefined, { numeric: true });
      return dir === 'asc' ? c : -c;
    });
  }, [data.readings, query, decision, sortKey, dir]);

  const pages = Math.max(1, Math.ceil(rows.length / PAGE));
  const current = Math.min(page, pages);
  const visible = rows.slice((current - 1) * PAGE, current * PAGE);
  const sortBy = (key: SortKey) => { setDir(sortKey === key && dir === 'asc' ? 'desc' : 'asc'); setSortKey(key); };
  const th = (key: SortKey, label: string) => (
    <th scope="col" className="sortable" aria-sort={sortKey === key ? (dir === 'asc' ? 'ascending' : 'descending') : 'none'} onClick={() => sortBy(key)}>
      {label}<span className="arrow">{sortKey === key ? (dir === 'asc' ? '▲' : '▼') : ''}</span>
    </th>
  );

  return (
    <Card title="Telemetry readings" note="Filter, sort and page through persisted readings." flush>
      <div className="toolbar pad">
        <input aria-label="Filter readings" placeholder="Filter device, cell or reason" value={query} onChange={(e) => { setQuery(e.target.value); setPage(1); }} />
        <select aria-label="Filter decision" value={decision} onChange={(e) => { setDecision(e.target.value); setPage(1); }}>
          <option value="all">All decisions</option>
          <option value="ACCEPT">Accepted</option>
          <option value="PROVISIONAL_ESCROW">Escrow</option>
          <option value="QUARANTINE">Quarantined</option>
        </select>
      </div>
      {visible.length ? (
        <TableWrap><table aria-label="Telemetry readings table"><thead><tr>
          {th('deviceId', 'Device ID')}{th('timestamp', 'Timestamp')}{th('rainfallMm', 'Rainfall (mm)')}{th('radarRainfallMm', 'Radar (mm)')}{th('peerConsensusMm', 'Peer consensus (mm)')}{th('decision', 'Decision')}{th('reason', 'Reason code')}
        </tr></thead><tbody>
          {visible.map((r) => (
            <tr key={r.id}><td className="mono">{r.deviceId}</td><td>{fmt(r.timestamp)}</td><td>{r.rainfallMm}</td><td>{r.radarRainfallMm}</td>
              <td>{r.peerConsensusMm ?? '—'} <span className="muted">(n={r.peerCount})</span></td><td><Badge state={r.decision} /></td><td className="mono">{r.reason}</td></tr>
          ))}
        </tbody></table></TableWrap>
      ) : <Empty>No readings match.</Empty>}
      <div className="pager pad">
        <Button disabled={current <= 1} onClick={() => setPage(current - 1)}>Previous</Button>
        <span>Page {current} of {pages} · {rows.length} rows</span>
        <Button disabled={current >= pages} onClick={() => setPage(current + 1)}>Next</Button>
      </div>
    </Card>
  );
}
