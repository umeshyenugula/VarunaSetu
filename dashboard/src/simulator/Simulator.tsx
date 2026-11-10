import { useEffect, useMemo, useState } from 'react';
import { advanceSimulation, initialSimulatorState } from './engine';
import type { BuildingType, SensorType, SimulatorBuilding, SimulatorState } from './types';

const STORAGE_KEY = 'varunasetu-simulator-session';
const buildingTypes: BuildingType[] = ['RESIDENTIAL', 'APARTMENT', 'COMMERCIAL', 'INDUSTRIAL', 'PUBLIC'];
const sensorTypes: SensorType[] = ['RAIN_GAUGE', 'WATER_LEVEL', 'FLOW_METER'];

function readSession(): SimulatorState {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    return saved ? JSON.parse(saved) as SimulatorState : initialSimulatorState();
  } catch {
    return initialSimulatorState();
  }
}

export default function Simulator({ onBack }: { onBack: () => void }) {
  const [state, setState] = useState<SimulatorState>(readSession);
  const [selectedId, setSelectedId] = useState(state.buildings[0]?.id ?? '');
  const [draggedSensor, setDraggedSensor] = useState<SensorType | null>(null);
  const selected = state.buildings.find((building) => building.id === selectedId);

  useEffect(() => {
    if (!state.running) return;
    const started = performance.now();
    const timer = window.setInterval(() => {
      const elapsed = (performance.now() - started) / 1000;
      setState((current) => advanceSimulation(current, elapsed));
    }, 1000);
    return () => window.clearInterval(timer);
  }, [state.running]);

  const updateSelected = (patch: Partial<SimulatorBuilding>) => {
    setState((current) => ({ ...current, buildings: current.buildings.map((building) => building.id === selectedId ? { ...building, ...patch } : building) }));
  };
  const addBuilding = () => {
    const id = `BLD-${String(state.buildings.length + 1).padStart(3, '0')}`;
    const building: SimulatorBuilding = { id, name: 'New building', type: 'RESIDENTIAL', x: 8, y: 4, roofAreaM2: 100, runoffCoefficient: 0.8, zoneId: 'A', hasPit: false, pitCapacityLitres: 0, hasTank: false, tankCapacityLitres: 0, storedLitres: 0 };
    setState((current) => ({ ...current, buildings: [...current.buildings, building] }));
    setSelectedId(id);
  };
  const dropSensor = (buildingId: string) => {
    if (!draggedSensor) return;
    if (draggedSensor === 'WATER_LEVEL' && !state.buildings.find((building) => building.id === buildingId)?.hasTank) return;
    const id = `VS-${draggedSensor.slice(0, 4)}-${String(state.sensors.length + 1).padStart(3, '0')}`;
    setState((current) => ({ ...current, sensors: [...current.sensors, { id, type: draggedSensor, buildingId, enabled: true, fault: 'NONE', intervalSeconds: 10 }] }));
    setDraggedSensor(null);
  };
  const reset = () => { const next = initialSimulatorState(); setState(next); setSelectedId(next.buildings[0].id); };
  const balanceRows = useMemo(() => [['Rainfall received', state.balance.rainfallLitres], ['Runoff generated', state.balance.runoffLitres], ['Captured in tanks', state.balance.capturedLitres], ['Overflow to drainage', state.balance.overflowLitres], ['Recharged', state.balance.rechargedLitres], ['Stored water', state.balance.storedLitres]], [state.balance]);

  return <div className="simulator">
    <header className="sim-header"><div><strong>VarunaSetu</strong><span className="sim-title">Urban Water Digital Twin</span></div><div className="sim-actions"><span className="badge sim">SIMULATED</span><button onClick={onBack}>Intelligence dashboard</button><button onClick={() => localStorage.setItem(STORAGE_KEY, JSON.stringify(state))}>Save</button><button onClick={reset}>Reset</button></div></header>
    <div className="sim-toolbar"><label>Rainfall <input type="number" min="0" max="200" value={state.rainfallMmPerHour} onChange={(event) => setState((current) => ({ ...current, rainfallMmPerHour: Math.max(0, Number(event.target.value)) }))} /> mm/hour</label><label>Speed <select value={state.speed} onChange={(event) => setState((current) => ({ ...current, speed: Number(event.target.value) }))}><option value="1">1x</option><option value="5">5x</option><option value="10">10x</option><option value="30">30x</option></select></label><button className="primary" onClick={() => setState((current) => ({ ...current, running: !current.running }))}>{state.running ? 'Pause rainfall' : 'Start rainfall'}</button><span className="sim-clock">{state.simulationHours.toFixed(3)} simulated hours</span></div>
    <main className="sim-layout">
      <aside className="sim-panel"><h2>Sensor toolbox</h2><p className="section-note">Drag a sensor onto a valid building. Water-level sensors require a tank.</p>{sensorTypes.map((type) => <div key={type} className="tool-item" draggable onDragStart={() => setDraggedSensor(type)}>{type.replace('_', ' ')}</div>)}<h2>Neighbourhood</h2><button onClick={addBuilding}>+ Add building</button>{state.buildings.map((building) => <button key={building.id} className={`object-item ${selectedId === building.id ? 'selected' : ''}`} onClick={() => setSelectedId(building.id)}>{building.name}<small>{building.type}</small></button>)}</aside>
      <section className="sim-canvas"><div className="sim-grid">{state.buildings.map((building) => <BuildingTile key={building.id} building={building} selected={selectedId === building.id} sensorCount={state.sensors.filter((sensor) => sensor.buildingId === building.id).length} onSelect={() => setSelectedId(building.id)} onDrop={() => dropSensor(building.id)} />)}</div><div className="sim-legend"><span>Blue: verified-capable</span><span>Yellow: simulated telemetry</span><span>Grey: uninstrumented</span></div></section>
      <aside className="sim-panel"><h2>Selection</h2>{selected ? <><label>Name<input value={selected.name} onChange={(event) => updateSelected({ name: event.target.value })} /></label><label>Type<select value={selected.type} onChange={(event) => updateSelected({ type: event.target.value as BuildingType })}>{buildingTypes.map((type) => <option key={type}>{type}</option>)}</select></label><label>Roof area<input type="number" min="1" value={selected.roofAreaM2} onChange={(event) => updateSelected({ roofAreaM2: Number(event.target.value) })} /></label><label>Runoff coefficient<input type="number" min="0" max="1" step="0.05" value={selected.runoffCoefficient} onChange={(event) => updateSelected({ runoffCoefficient: Number(event.target.value) })} /></label><label><input type="checkbox" checked={selected.hasTank} onChange={(event) => updateSelected({ hasTank: event.target.checked })} /> Storage tank</label>{selected.hasTank && <label>Tank capacity (L)<input type="number" min="1" value={selected.tankCapacityLitres} onChange={(event) => updateSelected({ tankCapacityLitres: Number(event.target.value) })} /></label>}<label><input type="checkbox" checked={selected.hasPit} onChange={(event) => updateSelected({ hasPit: event.target.checked })} /> Recharge pit</label><h2>Attached devices</h2>{state.sensors.filter((sensor) => sensor.buildingId === selected.id).map((sensor) => <div className="sensor-row" key={sensor.id}><span>{sensor.id}<small>{sensor.type}</small></span><select value={sensor.fault} onChange={(event) => setState((current) => ({ ...current, sensors: current.sensors.map((item) => item.id === sensor.id ? { ...item, fault: event.target.value as typeof item.fault } : item) }))}><option>NONE</option><option>NOISE</option><option>STUCK</option><option>DISCONNECTED</option><option>IMPOSSIBLE</option></select></div>)}</> : <p className="muted">Select a building.</p>}<h2>Water balance</h2>{balanceRows.map(([label, value]) => <div className="balance-row" key={label}><span>{label}</span><strong>{Number(value).toFixed(1)} L</strong></div>)}<p className="section-note">Runoff uses V = rainfall depth × roof area × runoff coefficient. Estimated runoff is kept separate from captured and stored water.</p></aside>
    </main>
    <footer className="sim-events"><strong>Event stream</strong>{state.events.slice(0, 3).map((event, index) => <span key={`${event}-${index}`}>{event}</span>)}<span className="muted">Backend transmission is intentionally gated by the existing signed-device enrollment contract.</span></footer>
  </div>;
}

function BuildingTile({ building, selected, sensorCount, onSelect, onDrop }: { building: SimulatorBuilding; selected: boolean; sensorCount: number; onSelect: () => void; onDrop: () => void }) {
  return <button className={`building-tile type-${building.type.toLowerCase()} ${selected ? 'selected' : ''}`} style={{ left: `${building.x * 8}%`, top: `${building.y * 13}%` }} onClick={onSelect} onDragOver={(event) => event.preventDefault()} onDrop={onDrop} title={`${building.name} - drop a sensor here`}><span className="building-roof" /><strong>{building.name}</strong><small>{building.roofAreaM2} m² roof</small>{sensorCount > 0 && <em>{sensorCount} sensor{sensorCount > 1 ? 's' : ''}</em>}</button>;
}
