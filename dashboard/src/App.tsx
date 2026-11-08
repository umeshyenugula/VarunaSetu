import { useEffect, useMemo, useState } from 'react';
import { MapContainer, Polygon, Popup, TileLayer } from 'react-leaflet';
import { cellToBoundary } from 'h3-js';
import 'leaflet/dist/leaflet.css';
import { getAnalytics, listCells, listEscrows, listLedger, listReadings } from './api/client';
import type { AnalyticsSummary, CellState, DecisionState, EscrowRecord, LedgerEntryView, TelemetryReading } from './types';
import Simulator from './simulator/Simulator';

function stateBadge(state: DecisionState | string) {
  const cls = state === 'ACCEPT' ? 'accept' : state === 'PROVISIONAL_ESCROW' ? 'escrow' : state === 'QUARANTINE' ? 'quarantine' : '';
  return <span className={`badge ${cls}`}>{state}</span>;
}

function fmt(ts?: string | null) {
  if (!ts) return '—';
  const date = new Date(ts);
  return Number.isNaN(date.getTime()) ? ts : date.toLocaleString();
}

function useCountdown(target?: string) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  if (!target) return '—';
  const remaining = new Date(target).getTime() - now;
  if (Number.isNaN(remaining)) return '—';
  if (remaining <= 0) return 'Expired';
  const seconds = Math.floor(remaining / 1000);
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
}

export default function App() {
  const [view, setView] = useState(() => window.location.hash === '#simulator' ? 'simulator' : 'dashboard');
  const [readings, setReadings] = useState<TelemetryReading[]>([]);
  const [escrows, setEscrows] = useState<EscrowRecord[]>([]);
  const [ledger, setLedger] = useState<LedgerEntryView[]>([]);
  const [cells, setCells] = useState<CellState[]>([]);
  const [analytics, setAnalytics] = useState<AnalyticsSummary | null>(null);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let active = true;
    Promise.all([listReadings(), listEscrows(), listLedger(), listCells(), getAnalytics()])
      .then(([readingItems, escrowItems, ledgerItems, cellItems, summary]) => {
        if (!active) return;
        setReadings(readingItems);
        setEscrows(escrowItems);
        setLedger(ledgerItems);
        setCells(cellItems);
        setAnalytics(summary);
      })
      .catch((cause: unknown) => {
        if (active) setError(cause instanceof Error ? cause.message : 'Dashboard data could not be loaded');
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => { active = false; };
  }, []);

  if (view === 'simulator') return <Simulator onBack={() => { window.location.hash = ''; setView('dashboard'); }} />;

  return (
    <div className="app">
      <header className="header">
        <div>
          <h1>HydroLedger Dashboard</h1>
          <p>Operational telemetry verification, escrow workflow, spatial status, and audit records.</p>
        </div>
        <div className="header-actions"><span className="badge">LIVE DATABASE</span><button onClick={() => { window.location.hash = 'simulator'; setView('simulator'); }}>Open urban simulator</button></div>
      </header>

      {error && <div className="banner error" role="alert">Live data unavailable: {error}. Configure the API and database before using this dashboard.</div>}
      {loading && <div className="banner" role="status">Loading live data…</div>}

      <section className="card" style={{ marginBottom: 16 }}>
        <h2>System analytics</h2>
        <p className="section-note">Metrics are calculated by the backend from persisted telemetry and escrow records.</p>
        <div className="metrics">
          <Metric label="Total telemetry readings" value={analytics?.totalReadings ?? '—'} />
          <Metric label="Accepted" value={analytics?.accepted ?? '—'} />
          <Metric label="Provisional" value={analytics?.provisional ?? '—'} />
          <Metric label="Quarantined" value={analytics?.quarantined ?? '—'} />
          <Metric label="Escrow resolution rate" value={analytics?.escrowResolutionRate == null ? '—' : `${Math.round(analytics.escrowResolutionRate * 100)}%`} />
          <Metric label="Active devices" value={analytics?.activeDevices ?? '—'} />
          <Metric label="Spatial cells shown" value={cells.length} />
        </div>
      </section>

      <div className="grid two" style={{ marginBottom: 16 }}>
        <section className="card">
          <h2>H3 cell status</h2>
          <p className="section-note">Cells and decision states are read from the persisted backend projections.</p>
          <H3Map cells={cells} readings={readings} />
        </section>
        <section className="card">
          <h2>Escrow records</h2>
          <p className="section-note">Remaining time is calculated from each persisted expiry timestamp.</p>
          <div className="list">
            {escrows.map((escrow) => <EscrowCard key={escrow.id} escrow={escrow} />)}
            {!escrows.length && !loading && <p className="muted">No escrow records returned.</p>}
          </div>
        </section>
      </div>

      <section className="card" style={{ marginBottom: 16 }}>
        <h2>Telemetry readings</h2>
        <p className="section-note">Filter, sort, and paginate persisted readings.</p>
        <ReadingsTable rows={readings} />
      </section>

      <section className="card">
        <h2>Audit ledger</h2>
        <p className="section-note">Entries are shown only when the backend ledger projection is available. Cryptographic verification remains a server-side responsibility.</p>
        <LedgerTable entries={ledger} />
      </section>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: number | string }) {
  return <div className="metric"><div className="label">{label}</div><div className="value">{value}</div></div>;
}

function EscrowCard({ escrow }: { escrow: EscrowRecord }) {
  const remaining = useCountdown(escrow.expiresAt);
  const displayState = escrow.state === 'CORROBORATED' ? 'ACCEPT' : escrow.state === 'PENDING' ? 'PROVISIONAL_ESCROW' : 'QUARANTINE';
  return (
    <article className="escrow" aria-label={`Escrow ${escrow.id}`}>
      <header><strong className="mono">{escrow.id}</strong>{stateBadge(displayState)}</header>
      <div className="kv">
        <div><span>Device</span><br />{escrow.deviceId}</div>
        <div><span>Cell</span><br /><span className="mono">{escrow.h3Index}</span></div>
        <div><span>State</span><br />{escrow.state}</div>
        <div><span>Created</span><br />{fmt(escrow.createdAt)}</div>
        <div><span>Expiry</span><br />{fmt(escrow.expiresAt)}</div>
        <div><span>Remaining</span><br /><strong>{remaining}</strong></div>
        <div><span>Corroboration</span><br />{escrow.state === 'CORROBORATED' ? `Corroborated ${fmt(escrow.corroboratedAt)}` : escrow.state === 'PENDING' ? 'Pending' : 'Not corroborated'}</div>
      </div>
    </article>
  );
}

function H3Map({ cells, readings }: { cells: CellState[]; readings: TelemetryReading[] }) {
  const center = useMemo(() => {
    for (const cell of cells) {
      try {
        const boundary = cellToBoundary(cell.h3Index);
        if (boundary[0]) return [boundary[0][0], boundary[0][1]] as [number, number];
      } catch {
        continue;
      }
    }
    return [17.385, 78.4867] as [number, number];
  }, [cells]);
  const color = (state: DecisionState) => state === 'ACCEPT' ? '#15803d' : state === 'PROVISIONAL_ESCROW' ? '#b45309' : '#b91c1c';
  return (
    <div className="map" aria-label="H3 geospatial map">
      <MapContainer center={center} zoom={12} style={{ height: '100%', width: '100%' }} scrollWheelZoom={false}>
        <TileLayer attribution="&copy; OpenStreetMap contributors" url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png" />
        {cells.map((cell) => {
          let positions: [number, number][] = [];
          try { positions = cellToBoundary(cell.h3Index).map(([lat, lng]) => [lat, lng]); } catch { return null; }
          const related = readings.filter((reading) => reading.h3Index === cell.h3Index).slice(0, 3);
          return <Polygon key={cell.h3Index} positions={positions} pathOptions={{ color: color(cell.state), fillColor: color(cell.state), fillOpacity: 0.35, weight: 2 }}>
            <Popup><strong className="mono">{cell.h3Index}</strong><br />State: {cell.state}<br />Readings: {cell.readings}{related.map((reading) => <div key={reading.id}>{reading.deviceId}: {reading.decision} ({reading.reason})</div>)}</Popup>
          </Polygon>;
        })}
      </MapContainer>
    </div>
  );
}

type SortKey = keyof TelemetryReading;
function ReadingsTable({ rows }: { rows: TelemetryReading[] }) {
  const [query, setQuery] = useState('');
  const [decision, setDecision] = useState('all');
  const [sortKey, setSortKey] = useState<SortKey>('timestamp');
  const [direction, setDirection] = useState<'asc' | 'desc'>('desc');
  const [page, setPage] = useState(1);
  const pageSize = 10;
  const filtered = useMemo(() => rows.filter((row) => {
    const text = [row.id, row.deviceId, row.h3Index, row.reason].join(' ').toLowerCase();
    return (decision === 'all' || row.decision === decision) && (!query || text.includes(query.toLowerCase()));
  }), [rows, query, decision]);
  const sorted = useMemo(() => [...filtered].sort((a, b) => {
    const comparison = String(a[sortKey] ?? '').localeCompare(String(b[sortKey] ?? ''), undefined, { numeric: true });
    return direction === 'asc' ? comparison : -comparison;
  }), [filtered, sortKey, direction]);
  const pages = Math.max(1, Math.ceil(sorted.length / pageSize));
  const safePage = Math.min(page, pages);
  const pageRows = sorted.slice((safePage - 1) * pageSize, safePage * pageSize);
  const header = (key: SortKey, label: string) => <th scope="col" onClick={() => { setDirection(sortKey === key && direction === 'asc' ? 'desc' : 'asc'); setSortKey(key); }}>{label}{sortKey === key ? (direction === 'asc' ? ' ▲' : ' ▼') : ''}</th>;
  return <div>
    <div className="controls">
      <input aria-label="Filter readings" placeholder="Filter device/cell/reason" value={query} onChange={(event) => { setQuery(event.target.value); setPage(1); }} />
      <select aria-label="Filter decision" value={decision} onChange={(event) => { setDecision(event.target.value); setPage(1); }}><option value="all">All decisions</option><option value="ACCEPT">ACCEPT</option><option value="PROVISIONAL_ESCROW">PROVISIONAL_ESCROW</option><option value="QUARANTINE">QUARANTINE</option></select>
    </div>
    <div style={{ overflowX: 'auto' }}><table aria-label="Telemetry readings table"><thead><tr>{header('deviceId', 'Device ID')}{header('timestamp', 'Timestamp')}{header('rainfallMm', 'Rainfall (mm)')}{header('radarRainfallMm', 'Radar (mm)')}{header('peerConsensusMm', 'Peer consensus (mm)')}{header('decision', 'Decision')}{header('reason', 'Reason code')}</tr></thead><tbody>
      {pageRows.map((row) => <tr key={row.id}><td>{row.deviceId}</td><td>{fmt(row.timestamp)}</td><td>{row.rainfallMm}</td><td>{row.radarRainfallMm}</td><td>{row.peerConsensusMm ?? '—'} <span className="muted">(n={row.peerCount})</span></td><td>{stateBadge(row.decision)}</td><td className="mono">{row.reason}</td></tr>)}
    </tbody></table></div>
    <div className="pager"><button disabled={safePage <= 1} onClick={() => setPage((value) => Math.max(1, value - 1))}>Previous</button><span>Page {safePage} of {pages} ({sorted.length} rows)</span><button disabled={safePage >= pages} onClick={() => setPage((value) => Math.min(pages, value + 1))}>Next</button></div>
  </div>;
}

function LedgerTable({ entries }: { entries: LedgerEntryView[] }) {
  return <div style={{ overflowX: 'auto' }}><table aria-label="Audit ledger entries"><thead><tr><th scope="col">Seq</th><th scope="col">Event reference</th><th scope="col">Previous hash</th><th scope="col">Current hash</th><th scope="col">Timestamp</th><th scope="col">Checkpoint signature</th></tr></thead><tbody>
    {entries.map((entry) => <tr key={entry.seq}><td>{entry.seq}</td><td>{entry.eventRef}</td><td className="mono">{entry.prevHash}</td><td className="mono">{entry.hash}</td><td>{fmt(entry.timestamp)}</td><td>{entry.checkpointSignatureStatus}</td></tr>)}
  </tbody></table>{!entries.length && <p className="muted">No audit ledger entries are available from the backend.</p>}</div>;
}
