import type { LiveData } from '../hooks';
import { fmt } from '../format';
import { Badge, Card, Empty, Metric, TableWrap } from '../ui';

export default function Overview({ data }: { data: LiveData }) {
  const { analytics, cells, readings, escrows } = data;
  const total = analytics?.totalReadings ?? 0;
  const parts = [
    { key: 'accept', label: 'Accepted', n: analytics?.accepted ?? 0 },
    { key: 'escrow', label: 'Escrow', n: analytics?.provisional ?? 0 },
    { key: 'quarantine', label: 'Quarantined', n: analytics?.quarantined ?? 0 },
  ];
  const pending = escrows.filter((e) => e.state === 'PENDING').length;
  return (
    <>
      <Card title="System analytics" note="Calculated by the backend from persisted telemetry and escrow records." actions={<span className="tag">LIVE DATABASE</span>}>
        <div className="metrics">
          <Metric label="Total telemetry readings" value={analytics?.totalReadings ?? '—'} />
          <Metric label="Accepted" value={analytics?.accepted ?? '—'} />
          <Metric label="Provisional" value={analytics?.provisional ?? '—'} />
          <Metric label="Quarantined" value={analytics?.quarantined ?? '—'} />
          <Metric label="Escrow resolution rate" value={analytics?.escrowResolutionRate == null ? '—' : `${Math.round(analytics.escrowResolutionRate * 100)}%`} />
          <Metric label="Active devices" value={analytics?.activeDevices ?? '—'} />
          <Metric label="Spatial cells" value={cells.length} />
          <Metric label="Open escrows" value={pending} />
        </div>
      </Card>

      <div className="grid two">
        <Card title="Decision mix" note="Share of all readings by verdict.">
          {total > 0 ? (
            <>
              <div className="bar" role="img" aria-label="Decision mix">
                {parts.map((p) => p.n > 0 && <span key={p.key} className={p.key} style={{ width: `${(p.n / total) * 100}%` }} />)}
              </div>
              <ul className="legend">
                {parts.map((p) => <li key={p.key}><i className={p.key} />{p.label}<b>{p.n}</b><span>{Math.round((p.n / total) * 100)}%</span></li>)}
              </ul>
            </>
          ) : <Empty>No readings yet. Run a story to create some.</Empty>}
        </Card>

        <Card title="Latest readings" note="Most recent five. Full list on the Readings screen." flush>
          {readings.length ? (
            <TableWrap><table aria-label="Latest readings"><thead><tr><th>Device</th><th>Time</th><th>Decision</th></tr></thead><tbody>
              {[...readings].sort((a, b) => b.timestamp.localeCompare(a.timestamp)).slice(0, 5).map((r) => (
                <tr key={r.id}><td className="mono">{r.deviceId}</td><td>{fmt(r.timestamp)}</td><td><Badge state={r.decision} /></td></tr>
              ))}
            </tbody></table></TableWrap>
          ) : <Empty>No readings returned.</Empty>}
        </Card>
      </div>
    </>
  );
}
