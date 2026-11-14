import { useEffect, useState, type ReactNode } from 'react';
import { apiBase } from './config';
import { useLiveData, type LiveData } from './hooks';
import Escrows from './pages/Escrows';
import Ledger from './pages/Ledger';
import MapView from './pages/MapView';
import Overview from './pages/Overview';
import Readings from './pages/Readings';
import { backend } from './simulator/api';
import Simulator from './simulator/Simulator';
import { Banner, Button, PageHeader } from './ui';

type RouteId = 'story' | 'overview' | 'map' | 'readings' | 'escrows' | 'ledger';
interface Route { id: RouteId; label: string; group: string; title: string; description: string }

const ROUTES: Route[] = [
  { id: 'story', label: 'Story', group: 'Simulation', title: 'Story simulation', description: 'Run a rainfall scenario end to end and watch the backend verify it, step by step.' },
  { id: 'overview', label: 'Overview', group: 'Monitoring', title: 'Overview', description: 'System health at a glance, calculated by the backend.' },
  { id: 'map', label: 'Map', group: 'Monitoring', title: 'Map', description: 'H3 cells coloured by their verification state.' },
  { id: 'readings', label: 'Readings', group: 'Monitoring', title: 'Readings', description: 'Every telemetry reading with the verdict the backend reached.' },
  { id: 'escrows', label: 'Escrows', group: 'Monitoring', title: 'Escrows', description: 'Readings held back until neighbours or radar corroborate them.' },
  { id: 'ledger', label: 'Ledger', group: 'Monitoring', title: 'Audit ledger', description: 'Tamper-evident record of every decision.' },
];

function routeFromHash(): RouteId {
  const hash = window.location.hash.replace(/^#\/?/, '');
  if (hash === 'operations') return 'overview'; // old bookmark
  return ROUTES.some((r) => r.id === hash) ? (hash as RouteId) : 'story';
}

export default function App() {
  const [route, setRoute] = useState<RouteId>(routeFromHash);
  useEffect(() => {
    const onHash = () => setRoute(routeFromHash());
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  }, []);

  const current = ROUTES.find((r) => r.id === route) as Route;
  const groups = [...new Set(ROUTES.map((r) => r.group))];
  const monitoring = current.group === 'Monitoring';
  const data = useLiveData(monitoring ? route : null);

  return (
    <div className="shell">
      <aside className="sidebar">
        <div className="brand"><strong>VarunaSetu</strong><span>Urban water verification</span></div>
        <nav aria-label="Screens">
          {groups.map((group) => (
            <div className="nav-group" key={group}>
              <div className="nav-title">{group}</div>
              {ROUTES.filter((r) => r.group === group).map((r) => (
                <a key={r.id} href={`#/${r.id}`} className={r.id === route ? 'on' : ''} aria-current={r.id === route ? 'page' : undefined}>{r.label}</a>
              ))}
            </div>
          ))}
        </nav>
        <ApiStatus />
      </aside>

      <main className="main">
        <PageHeader
          title={current.title}
          description={current.description}
          actions={monitoring && <Button onClick={data.refresh} disabled={data.loading}>{data.loading ? 'Loading…' : 'Refresh'}</Button>}
        />
        {route === 'story' ? <Simulator /> : <Monitoring data={data}>{screen(route, data)}</Monitoring>}
      </main>
    </div>
  );
}

function screen(route: RouteId, data: LiveData): ReactNode {
  switch (route) {
    case 'overview': return <Overview data={data} />;
    case 'map': return <MapView data={data} />;
    case 'readings': return <Readings data={data} />;
    case 'escrows': return <Escrows data={data} />;
    case 'ledger': return <Ledger data={data} />;
    default: return null;
  }
}

/** Same loading and error treatment on every monitoring screen. */
function Monitoring({ data, children }: { data: LiveData; children: ReactNode }) {
  return (
    <>
      {data.error && <Banner tone="error">Live data unavailable: {data.error}. Start the backend on port 8080 and press Refresh.</Banner>}
      {data.loading && !data.error && <Banner tone="info">Loading live data…</Banner>}
      {children}
    </>
  );
}

function ApiStatus() {
  const [state, setState] = useState<'checking' | 'up' | 'down'>('checking');
  useEffect(() => {
    let on = true;
    backend.health().then(() => on && setState('up')).catch(() => on && setState('down'));
    return () => { on = false; };
  }, []);
  const base = apiBase();
  const target = base.startsWith('/') ? import.meta.env.VITE_PROXY_TARGET || 'http://localhost:8080' : base;
  return (
    <div className="api-status" title="Where API requests are sent">
      <div className="nav-title">Backend API</div>
      <div className={`status ${state}`}><i />{state === 'up' ? 'Connected' : state === 'down' ? 'Unreachable' : 'Checking'}</div>
      <code>{target}</code>
    </div>
  );
}
