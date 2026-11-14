import { useMemo } from 'react';
import { MapContainer, Polygon, Popup, TileLayer } from 'react-leaflet';
import { cellToBoundary } from 'h3-js';
import 'leaflet/dist/leaflet.css';
import type { LiveData } from '../hooks';
import type { DecisionState } from '../types';
import { Badge, Card, Empty, TableWrap } from '../ui';

const COLORS: Record<DecisionState, string> = { ACCEPT: '#0a7a3d', PROVISIONAL_ESCROW: '#a35c00', QUARANTINE: '#c0172b' };

export default function MapView({ data }: { data: LiveData }) {
  const { cells, readings } = data;
  const center = useMemo(() => {
    for (const cell of cells) {
      try {
        const first = cellToBoundary(cell.h3Index)[0];
        if (first) return [first[0], first[1]] as [number, number];
      } catch { continue; }
    }
    return [17.385, 78.4867] as [number, number];
  }, [cells]);

  return (
    <>
      <Card title="H3 cell status" note="Cell colour is the worst verdict recorded in that cell. Click a cell for its latest readings." flush>
        <div className="map" aria-label="H3 geospatial map">
          <MapContainer center={center} zoom={12} style={{ height: '100%', width: '100%' }} scrollWheelZoom={false}>
            <TileLayer attribution="&copy; OpenStreetMap contributors" url="https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png" />
            {cells.map((cell) => {
              let positions: [number, number][] = [];
              try { positions = cellToBoundary(cell.h3Index).map(([lat, lng]) => [lat, lng] as [number, number]); } catch { return null; }
              const related = readings.filter((r) => r.h3Index === cell.h3Index).slice(0, 3);
              const color = COLORS[cell.state];
              return (
                <Polygon key={cell.h3Index} positions={positions} pathOptions={{ color, fillColor: color, fillOpacity: 0.25, weight: 1 }}>
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
            {cells.map((c) => <tr key={c.h3Index}><td className="mono">{c.h3Index}</td><td><Badge state={c.state} /></td><td>{c.readings}</td></tr>)}
          </tbody></table></TableWrap>
        ) : <Empty>No cells returned.</Empty>}
      </Card>
    </>
  );
}
