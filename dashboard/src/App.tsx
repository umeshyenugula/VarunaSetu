import { useEffect, useMemo, useState } from 'react';
import { MapContainer, Polygon, Popup, TileLayer } from 'react-leaflet';
import { cellToBoundary } from 'h3-js';
import 'leaflet/dist/leaflet.css';
import { computeAnalytics, listCells, listEscrows, listLedger, listReadings, verifyLedgerIntegrity } from './api/client';
import { scenarios } from './mocks/fixtures';
import type { CellState, DecisionState, EscrowRecord, LedgerEntryView, TelemetryReading } from './types';

function stateBadge(state: DecisionState | string) {
  const cls = state === 'ACCEPT' ? 'accept' : state === 'PROVISIONAL_ESCROW' ? 'escrow' : state === 'QUARANTINE' ? 'quarantine' : '';
  return <span className={`badge ${cls}`}>{state}</span>;
}

function fmt(ts?: string | null) {
  if (!ts) return '—';
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? ts : d.toLocaleString();
}

function useCountdown(target?: string) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);
  if (!target) return '—';
  const ms = new Date(target).getTime() - now;
  if (Number.isNaN(ms)) return '—';
  if (ms <= 0) return 'Expired';
  const s = Math.floor(ms / 1000);
  const m = Math.floor(s / 60);
  const sec = s % 60;
  return `${m}m ${sec}s`;
}

export default function App() {
  const [scenario, setScenario] = useState<string>('all');
  const [readings, setReadings] = useState<TelemetryReading[]>([]);
  const [escrows, setEscrows] = useState<EscrowRecord[]>([]);
  const [ledger, setLedger] = useState<LedgerEntryView[]>([]);
  const [cells, setCells] = useState<CellState[]>([]);
  const [source, setSource] = useState<'api' | 'simulated'>('simulated');
  const [verify, setVerify] = useState<string>('');
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    (async () => {
      setLoading(true);
      const [r, e, l, c] = await Promise.all([listReadings(), listEscrows(), listLedger(), listCells()]);
      if (!alive) return;
      setReadings(r.items);
      setEscrows(e.items);
      setLedger(l.items);
      setCells(c.items);
      setSource(r.source);
      setLoading(false);
    })();
    return () => { alive = false; };
  }, []);

  const filteredReadings = useMemo(() => scenario === 'all' ? readings : readings.filter((r) => r.scenario === scenario), [readings, scenario]);
  const filteredEscrows = useMemo(() => scenario === 'all' ? escrows : escrows.filter((e) => e.scenario === scenario), [escrows, scenario]);
  const filteredCells = useMemo(() => {
    if (scenario === 'all') return cells;
    const by = new Map<string, { state: DecisionState; readings: number; simulated: boolean }>();
    const rank = { QUARANTINE: 3, PROVISIONAL_ESCROW: 2, ACCEPT: 1 } as const;
    for (const r of filteredReadings) {
      const cur = by.get(r.h3Index);
      if (!cur || rank[r.decision] > rank[cur.state]) by.set(r.h3Index, { state: r.decision, readings: (cur?.readings ?? 0) + 1, simulated: cur?.simulated || r.simulated });
      else cur.readings++;
    }
    return [...by.entries()].map(([h3Index, v]) => ({ h3Index, ...v }));
  }, [cells, filteredReadings, scenario]);

  const analytics = useMemo(() => computeAnalytics(filteredReadings, filteredEscrows), [filteredReadings, filteredEscrows]);

  return (
    <div className="app">
      <header className="header">
        <div>
          <h1>HydroLedger Dashboard</h1>
          <p>Geospatial telemetry verification, escrow workflow, and audit ledger integrity (minimal prototype view).</p>
        </div>
        <div className="badge sim">White/minimal UI</div>
      </header>

      {(source === 'simulated' || analytics.simulatedDataPresent) && (
        <div className="banner" role="status">
          <strong>SIMULATED DATA</strong> — Missing backend list/aggregate/ledger endpoints use mock fixtures (see HANDOFF.md REQUESTED CHANGES). Never treat this data as production telemetry.
        </div>
      )}

      <div className="card" style={{ marginBottom: 16 }}>
        <h2>Demonstration mode (6 scripted scenarios)</h2>
        <p className="section-note">Select a scenario to filter the dashboard. All displayed scenario data is labeled SIMULATED.</p>
        <div className="scenarios">
          <button className={scenario === 'all' ? 'active' : ''} onClick={() => setScenario('all')}>All scenarios</button>
          {scenarios.map((s) => (
            <button key={s.id} className={scenario === s.id ? 'active' : ''} onClick={() => setScenario(s.id)} title={s.description}>{s.title}</button>
          ))}
        </div>
        {scenario !== 'all' && <p className="section-note"><strong>SIMULATED:</strong> {scenarios.find((s) => s.id === scenario)?.description}</p>}
      </div>

      <div className="card" style={{ marginBottom: 16 }}>
        <h2>Dashboard analytics</h2>
        <p className="section-note">Metrics distinguish simulated fixtures from actual API data. Source: <strong>{source === 'simulated' ? 'SIMULATED fixture' : 'API'}</strong>.</p>
        <div className="metrics">
          <Metric label="Total telemetry readings" value={analytics.totalReadings} />
          <Metric label="Accepted" value={analytics.accepted} />
          <Metric label="Provisional" value={analytics.provisional} />
          <Metric label="Quarantined" value={analytics.quarantined} />
          <Metric label="Escrow resolution rate" value={analytics.escrowResolutionRate == null ? '—' : `${Math.round(analytics.escrowResolutionRate * 100)}%`} />
          <Metric label="Active devices" value={analytics.activeDevices} />
          <Metric label="Spatial cells shown" value={filteredCells.length} />
        </div>
      </div>

      <div className="grid two" style={{ marginBottom: 16 }}>
        <section className="card">
          <h2>Geospatial H3 map</h2>
          <p className="section-note">H3 cells colored by decision state: ACCEPT (green), PROVISIONAL_ESCROW (amber), QUARANTINE (red). Click a cell for telemetry context. {source === 'simulated' ? 'SIMULATED.' : ''}</p>
          <H3Map cells={filteredCells} readings={filteredReadings} />
        </section>
        <section className="card">
          <h2>Escrow panel</h2>
          <p className="section-note">Countdown updates live until expiry. {source === 'simulated' ? 'SIMULATED.' : ''}</p>
          <div className="list">
            {filteredEscrows.map((e) => <EscrowCard key={e.id} escrow={e} />)}
            {!filteredEscrows.length && <p className="muted">No escrows for this selection.</p>}
          </div>
        </section>
      </div>

      <section className="card" style={{ marginBottom: 16 }}>
        <h2>Telemetry readings</h2>
        <p className="section-note">Filter, sort, and paginate readings. {source === 'simulated' ? 'SIMULATED.' : ''}</p>
        {loading ? <p className="muted">Loading…</p> : <ReadingsTable rows={filteredReadings} />}
      </section>

      <section className="card">
        <h2>Audit ledger viewer</h2>
        <p className="section-note">Phase 4 ledger entries/checkpoints. Authoritative hashing/Ed25519 verification is implemented in Go; this UI provides a structural verification interface and uses SIMULATED fixtures when backend endpoints are unavailable.</p>
        <div className="controls">
          <button className="pager button" style={{ border: '1px solid var(--border)', background: '#fff', borderRadius: 6, padding: '7px 10px', cursor: 'pointer' }} onClick={async () => {
            const res = await verifyLedgerIntegrity(ledger);
            setVerify(`${res.ok ? 'OK' : 'FAILED'} [${res.source}]: ${res.message}`);
          }}>Verify ledger integrity</button>
          {verify && <span className="section-note">{verify}</span>}
        </div>
        <div style={{ overflowX: 'auto' }}>
          <table aria-label="Audit ledger entries">
            <thead><tr><th>Seq</th><th>Event reference</th><th>Previous hash</th><th>Current hash</th><th>Timestamp</th><th>Checkpoint signature</th></tr></thead>
            <tbody>
              {ledger.map((e) => (
                <tr key={e.seq}>
                  <td>{e.seq}</td>
                  <td>{e.eventRef} {e.simulated && <span className="badge sim">SIMULATED</span>}</td>
                  <td className="mono" title={e.prevHash}>{e.prevHash.slice(0, 16)}…</td>
                  <td className="mono" title={e.hash}>{e.hash.slice(0, 16)}…</td>
                  <td>{fmt(e.timestamp)}</td>
                  <td>{e.checkpointSignatureStatus}{e.checkpointSeq != null ? ` (seq ${e.checkpointSeq})` : ''}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string | number }) {
  return <div className="metric"><div className="label">{label}</div><div className="value">{value}</div></div>;
}

function EscrowCard({ escrow }: { escrow: EscrowRecord }) {
  const remaining = useCountdown(escrow.state === 'PENDING' ? escrow.expiresAt : undefined);
  return (
    <article className="escrow" aria-label={`Escrow ${escrow.id}`}>
      <header>
        <strong className="mono">{escrow.id}</strong>
        <span>{stateBadge(escrow.state === 'CORROBORATED' ? 'ACCEPT' : escrow.state === 'PENDING' ? 'PROVISIONAL_ESCROW' : 'QUARANTINE')} {escrow.simulated && <span className="badge sim">SIMULATED</span>}</span>
      </header>
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
    for (const c of cells) {
      try { const b = cellToBoundary(c.h3Index); if (b?.[0]) return [b[0][0], b[0][1]] as [number, number]; } catch {}
    }
    return [17.385, 78.4867] as [number, number];
  }, [cells]);
  const color = (s: DecisionState) => s === 'ACCEPT' ? '#15803d' : s === 'PROVISIONAL_ESCROW' ? '#b45309' : '#b91c1c';
  return (
    <div className="map" aria-label="H3 geospatial map">
      <MapContainer center={center} zoom={12} style={{ height: '100%', width: '100%' }} scrollWheelZoom={false}>
        <TileLayer attribution='&copy; OpenStreetMap contributors' url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png" />
        {cells.map((c) => {
          let positions: [number, number][] = [];
          try { positions = cellToBoundary(c.h3Index).map(([lat, lng]) => [lat, lng]); } catch { positions = []; }
          if (!positions.length) return null;
          const rel = readings.filter((r) => r.h3Index === c.h3Index).slice(0, 3);
          return (
            <Polygon key={c.h3Index} positions={positions} pathOptions={{ color: color(c.state), fillColor: color(c.state), fillOpacity: 0.35, weight: 2 }}>
              <Popup>
                <strong className="mono">{c.h3Index}</strong><br />
                State: {c.state}<br />Readings shown: {c.readings}<br />
                {rel.map((r) => <div key={r.id} style={{ marginTop: 4 }}><span className="mono">{r.deviceId}</span> {r.decision} ({r.reason}) {r.simulated ? '[SIMULATED]' : ''}</div>)}
              </Popup>
            </Polygon>
          );
        })}
      </MapContainer>
    </div>
  );
}

type SortKey = keyof TelemetryReading;
function ReadingsTable({ rows }: { rows: TelemetryReading[] }) {
  const [q, setQ] = useState('');
  const [decision, setDecision] = useState('all');
  const [sortKey, setSortKey] = useState<SortKey>('timestamp');
  const [dir, setDir] = useState<'asc' | 'desc'>('desc');
  const [page, setPage] = useState(1);
  const pageSize = 5;
  const filtered = useMemo(() => {
    const qq = q.toLowerCase();
    return rows.filter((r) => (decision === 'all' || r.decision === decision) && (!qq || [r.id, r.deviceId, r.h3Index, r.reason].join(' ').toLowerCase().includes(qq)));
  }, [rows, q, decision]);
  const sorted = useMemo(() => {
    const copy = [...filtered];
    copy.sort((a, b) => {
      const av = a[sortKey] ?? '', bv = b[sortKey] ?? '';
      const cmp = String(av).localeCompare(String(bv), undefined, { numeric: true });
      return dir === 'asc' ? cmp : -cmp;
    });
    return copy;
  }, [filtered, sortKey, dir]);
  const pages = Math.max(1, Math.ceil(sorted.length / pageSize));
  const safePage = Math.min(page, pages);
  const pageRows = sorted.slice((safePage - 1) * pageSize, safePage * pageSize);
  function th(key: SortKey, label: string) {
    return <th onClick={() => { if (sortKey === key) setDir(dir === 'asc' ? 'desc' : 'asc'); else { setSortKey(key); setDir('asc'); } }}>{label}{sortKey === key ? (dir === 'asc' ? ' ▲' : ' ▼') : ''}</th>;
  }
  return (
    <div>
      <div className="controls">
        <input aria-label="Filter readings" placeholder="Filter device/cell/reason" value={q} onChange={(e) => { setQ(e.target.value); setPage(1); }} />
        <select aria-label="Filter decision" value={decision} onChange={(e) => { setDecision(e.target.value); setPage(1); }}>
          <option value="all">All decisions</option>
          <option value="ACCEPT">ACCEPT</option>
          <option value="PROVISIONAL_ESCROW">PROVISIONAL_ESCROW</option>
          <option value="QUARANTINE">QUARANTINE</option>
        </select>
      </div>
      <div style={{ overflowX: 'auto' }}>
        <table aria-label="Telemetry readings table">
          <thead><tr>{th('deviceId','Device ID')}{th('timestamp','Timestamp')}{th('rainfallMm','Rainfall (mm)')}{th('radarRainfallMm','Radar (mm)')}{th('peerConsensusMm','Peer consensus (mm)')}{th('decision','Decision')}{th('reason','Reason code')}</tr></thead>
          <tbody>
            {pageRows.map((r) => (
              <tr key={r.id}>
                <td>{r.deviceId} {r.simulated && <span className="badge sim">SIMULATED</span>}</td>
                <td>{fmt(r.timestamp)}</td>
                <td>{r.rainfallMm}</td>
                <td>{r.radarRainfallMm}</td>
                <td>{r.peerConsensusMm ?? '—'} <span className="muted">(n={r.peerCount})</span></td>
                <td>{stateBadge(r.decision)}</td>
                <td className="mono">{r.reason}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="pager">
        <button disabled={safePage <= 1} onClick={() => setPage((p) => Math.max(1, p - 1))}>Previous</button>
        <span>Page {safePage} of {pages} ({sorted.length} rows)</span>
        <button disabled={safePage >= pages} onClick={() => setPage((p) => Math.min(pages, p + 1))}>Next</button>
      </div>
    </div>
  );
}
