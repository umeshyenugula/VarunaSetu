import { useMemo } from 'react';
import type { LiveData } from '../hooks';
import { fmt } from '../format';
import { buildHash, setQuery, useRoute } from '../router';
import type { TelemetryReading } from '../types';
import { Badge, Banner, Button, Card, Empty, Facts, TableWrap } from '../ui';

type SortKey = 'deviceId' | 'timestamp' | 'rainfallMm' | 'radarRainfallMm' | 'peerConsensusMm' | 'decision' | 'reason';
const SORTABLE: SortKey[] = ['deviceId', 'timestamp', 'rainfallMm', 'radarRainfallMm', 'peerConsensusMm', 'decision', 'reason'];
const PAGE = 10;

/**
 * URL contract:  #/readings/:id?q=&decision=&sort=&dir=&page=
 * Every filter lives in the URL, so a filtered view can be bookmarked or shared; defaults are never written.
 */
export default function Readings({ data }: { data: LiveData }) {
  const { segments, query: params } = useRoute();
  const selectedId = segments[1];
  const query = params.get('q') ?? '';
  const decision = params.get('decision') ?? 'all';
  const sortParam = params.get('sort') as SortKey;
  const sortKey: SortKey = SORTABLE.includes(sortParam) ? sortParam : 'timestamp';
  const dir = params.get('dir') === 'asc' ? 'asc' : 'desc';
  const page = Math.max(1, Number(params.get('page')) || 1);

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
  const goTo = (n: number) => setQuery({ page: n > 1 ? String(n) : null });
  const sortBy = (key: SortKey) => {
    const next = sortKey === key && dir === 'asc' ? 'desc' : 'asc';
    setQuery({ sort: key === 'timestamp' ? null : key, dir: next === 'asc' ? 'asc' : null, page: null });
  };
  const th = (key: SortKey, label: string) => (
    <th scope="col" className="sortable" aria-sort={sortKey === key ? (dir === 'asc' ? 'ascending' : 'descending') : 'none'} onClick={() => sortBy(key)}>
      {label}<span className="arrow">{sortKey === key ? (dir === 'asc' ? '▲' : '▼') : ''}</span>
    </th>
  );
  const picked: TelemetryReading | undefined = selectedId ? data.readings.find((r) => r.id === selectedId) : undefined;
  const rowHref = (id: string) => buildHash(['readings', id], Object.fromEntries(params));

  return (
    <>
      {selectedId && picked && (
        <Card title="Reading detail" note="Opened from the URL: #/readings/:id" actions={<a className="btn" href={buildHash(['readings'], Object.fromEntries(params))}>Close</a>}>
          <Facts items={[
            { k: 'Reading', v: <span className="mono">{picked.id}</span> },
            { k: 'Device', v: <span className="mono">{picked.deviceId}</span> },
            { k: 'H3 cell', v: <a className="mono" href={buildHash(['map', picked.h3Index])}>{picked.h3Index}</a> },
            { k: 'Timestamp', v: fmt(picked.timestamp) },
            { k: 'Rainfall', v: `${picked.rainfallMm} mm` },
            { k: 'Radar', v: `${picked.radarRainfallMm} mm` },
            { k: 'Peer consensus', v: picked.peerConsensusMm == null ? '—' : `${picked.peerConsensusMm} mm (n=${picked.peerCount})` },
            { k: 'Decision', v: <Badge state={picked.decision} /> },
            { k: 'Reason', v: <span className="mono">{picked.reason}</span> },
          ]} />
        </Card>
      )}
      {selectedId && !picked && data.readings.length > 0 && <Banner tone="info">No reading has the id “{selectedId}”.</Banner>}

      <Card title="Telemetry readings" note="Filter, sort and page through persisted readings. The view is kept in the URL." flush>
        <div className="toolbar pad">
          <input aria-label="Filter readings" placeholder="Filter device, cell or reason" value={query} onChange={(e) => setQuery({ q: e.target.value || null, page: null })} />
          <select aria-label="Filter decision" value={decision} onChange={(e) => setQuery({ decision: e.target.value === 'all' ? null : e.target.value, page: null })}>
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
              <tr key={r.id} className={r.id === selectedId ? 'sel' : undefined}>
                <td className="mono"><a href={rowHref(r.id)}>{r.deviceId}</a></td><td>{fmt(r.timestamp)}</td><td>{r.rainfallMm}</td><td>{r.radarRainfallMm}</td>
                <td>{r.peerConsensusMm ?? '—'} <span className="muted">(n={r.peerCount})</span></td><td><Badge state={r.decision} /></td><td className="mono">{r.reason}</td>
              </tr>
            ))}
          </tbody></table></TableWrap>
        ) : <Empty>No readings match.</Empty>}
        <div className="pager pad">
          <Button disabled={current <= 1} onClick={() => goTo(current - 1)}>Previous</Button>
          <span>Page {current} of {pages} · {rows.length} rows</span>
          <Button disabled={current >= pages} onClick={() => goTo(current + 1)}>Next</Button>
        </div>
      </Card>
    </>
  );
}
