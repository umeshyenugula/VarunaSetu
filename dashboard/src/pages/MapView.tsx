import { useMemo } from 'react';
import { MapContainer, Polygon, Popup, TileLayer } from 'react-leaflet';
import { cellToBoundary } from 'h3-js';
import 'leaflet/dist/leaflet.css';
import type { LiveData } from '../hooks';
import { buildHash, navigate, useRoute } from '../router';
import type { DecisionState } from '../types';
import { Badge, Banner, Card, Empty, Facts, TableWrap } from '../ui';

const COLORS: Record<DecisionState, string> = { ACCEPT: '#0a7a3d', PROVISIONAL_ESCROW: '#a35c00', QUARANTINE: '#c0172b' };

/** URL contract: #/map/:cell highlights an H3 cell and lists its readings. Clicking a cell writes it to the URL. */
export default function MapView({ data }: { data: LiveData }) {
  const { cells, readings } = data;
  const selected = useRoute().segments[1];
  const chosen = cells.find((c) => c.h3Index === selected);
  const center = useMemo(() => {
    for (const cell of [...cells.filter((c) => c.h3Index === selected), ...cells]) {
      try {
        const first = cellToBoundary(cell.h3Index)[0];
        if (first) return [first[0], first[1]] as [number, number];
      } catch { continue; }
    }
    return [17.385, 78.4867] as [number, number];
  }, [cells, selected]);

  return (
    <>
      {selected && chosen && (
        <Card title="Cell detail" note="Opened from the URL: #/map/:cell" actions={<a className="btn" href={buildHash(['map'])}>Clear</a>}>
          <Facts items={[{ k: 'H3 cell', v: <span className="mono">{chosen.h3Index}</span> }, { k: 'State', v: <Badge state={chosen.state} /> }, { k: 'Readings', v: chosen.readings }]} />
          <ul className="legend">
            {readings.filter((r) => r.h3Index === chosen.h3Index).slice(0, 8).map((r) => (
              <li key={r.id}><i className={r.decision === 'ACCEPT' ? 'accept' : r.decision === 'QUARANTINE' ? 'quarantine' : 'escrow'} /><a className="mono" href={buildHash(['readings', r.id])}>{r.deviceId}</a><span className="mono">{r.reason}</span><span /></li>
            ))}
          </ul>
        </Card>
      )}
      {selected && !chosen && cells.length > 0 && <Banner tone="info">No H3 cell “{selected}” in the data.</Banner>}

      <Card title="H3 cell status" note="Cell colour is the worst verdict recorded in that cell. Click a cell to select it." flush>
        <div className="map" aria-label="H3 geospatial map">
          <MapContainer center={center} zoom={12} style={{ height: '100%', width: '100%' }} scrollWheelZoom={false}>
            <TileLayer attribution="&copy; OpenStreetMap contributors" url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png" />
            {cells.map((cell) => {
              let positions: [number, number][] = [];
              try { positions = cellToBoundary(cell.h3Index).map(([lat, lng]) => [lat, lng] as [number, number]); } catch { return null; }
              const related = readings.filter((r) => r.h3Index === cell.h3Index).slice(0, 3);
              const color = COLORS[cell.state];
              const on = cell.h3Index === selected;
              return (
                <Polygon key={cell.h3Index} positions={positions} pathOptions={{ color, fillColor: color, fillOpacity: on ? 0.55 : 0.25, weight: on ? 4 : 1 }} eventHandlers={{ click: () => navigate(['map', cell.h3Index]) }}>
                  <Popup><strong className="mono">{cell.h3Index}</strong><br />State: {cell.state}<br />Readings: {cell.readings}{related.map((r) => <div key={r.id}>{r.deviceId}: {r.decision} ({r.reason})</div>)}</Popup>
                </Polygon>
              );
            })}
          </MapContainer>
        </div>
      </Card>

      <Card title="Cells" note="Same data as the map, as a table." flush>
        {cells.length ? (
          <TableWrap><table aria-label="H3 cells"><thead><tr><th>H3 cell</th><th>State</th><th>Readings</th></tr></thead><tbody>
            {cells.map((c) => <tr key={c.h3Index} className={c.h3Index === selected ? 'sel' : undefined}><td className="mono"><a href={buildHash(['map', c.h3Index])}>{c.h3Index}</a></td><td><Badge state={c.state} /></td><td>{c.readings}</td></tr>)}
          </tbody></table></TableWrap>
        ) : <Empty>No cells returned.</Empty>}
      </Card>
    </>
  );
}
