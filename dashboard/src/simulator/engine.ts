import type { SimulatorBuilding, SimulatorState, WaterBalance } from './types';

export const DEMO_BUILDINGS: SimulatorBuilding[] = [
  { id: 'BLD-001', name: 'Riverside House', type: 'RESIDENTIAL', x: 2, y: 2, roofAreaM2: 90, runoffCoefficient: 0.8, zoneId: 'A', hasPit: true, pitCapacityLitres: 300, hasTank: true, tankCapacityLitres: 1200, storedLitres: 0 },
  { id: 'BLD-002', name: 'Cedar House', type: 'RESIDENTIAL', x: 4, y: 2, roofAreaM2: 110, runoffCoefficient: 0.8, zoneId: 'A', hasPit: false, pitCapacityLitres: 0, hasTank: true, tankCapacityLitres: 900, storedLitres: 0 },
  { id: 'BLD-003', name: 'Market Court', type: 'COMMERCIAL', x: 6, y: 2, roofAreaM2: 280, runoffCoefficient: 0.75, zoneId: 'B', hasPit: true, pitCapacityLitres: 900, hasTank: true, tankCapacityLitres: 2200, storedLitres: 0 },
  { id: 'BLD-004', name: 'Hill Apartment', type: 'APARTMENT', x: 2, y: 4, roofAreaM2: 420, runoffCoefficient: 0.78, zoneId: 'B', hasPit: true, pitCapacityLitres: 1200, hasTank: true, tankCapacityLitres: 4000, storedLitres: 0 },
  { id: 'BLD-005', name: 'Workshop', type: 'INDUSTRIAL', x: 5, y: 4, roofAreaM2: 650, runoffCoefficient: 0.7, zoneId: 'C', hasPit: false, pitCapacityLitres: 0, hasTank: false, tankCapacityLitres: 0, storedLitres: 0 },
];

export const initialSimulatorState = (): SimulatorState => ({
  buildings: DEMO_BUILDINGS.map((building) => ({ ...building })),
  sensors: [
    { id: 'VS-RAIN-001', type: 'RAIN_GAUGE', buildingId: 'BLD-001', enabled: true, fault: 'NONE', intervalSeconds: 10 },
    { id: 'VS-LEVEL-001', type: 'WATER_LEVEL', buildingId: 'BLD-003', enabled: true, fault: 'NONE', intervalSeconds: 10 },
  ],
  rainfallMmPerHour: 20,
  simulationHours: 0,
  speed: 1,
  running: false,
  balance: emptyBalance(),
  events: ['Demo neighbourhood loaded. Telemetry is labelled SIMULATED.'],
});

export function emptyBalance(): WaterBalance {
  return { rainfallLitres: 0, runoffLitres: 0, capturedLitres: 0, overflowLitres: 0, rechargedLitres: 0, storedLitres: 0 };
}

export function advanceSimulation(state: SimulatorState, elapsedWallSeconds: number): SimulatorState {
  if (!state.running || elapsedWallSeconds <= 0) return state;
  const hours = elapsedWallSeconds * state.speed / 3600;
  const depthMm = state.rainfallMmPerHour * hours;
  const balance = { ...state.balance };
  const buildings = state.buildings.map((building) => {
    const rainfall = depthMm * building.roofAreaM2;
    const runoff = rainfall * building.runoffCoefficient;
    const captured = building.hasTank ? Math.min(runoff, Math.max(0, building.tankCapacityLitres - building.storedLitres)) : 0;
    const overflow = runoff - captured;
    const recharged = building.hasPit ? Math.min(overflow, building.pitCapacityLitres * hours) : 0;
    balance.rainfallLitres += rainfall;
    balance.runoffLitres += runoff;
    balance.capturedLitres += captured;
    balance.overflowLitres += Math.max(0, overflow - recharged);
    balance.rechargedLitres += recharged;
    balance.storedLitres += captured;
    return { ...building, storedLitres: building.storedLitres + captured };
  });
  return {
    ...state,
    buildings,
    simulationHours: state.simulationHours + hours,
    balance,
    events: [`Rainfall advanced ${depthMm.toFixed(2)} mm; generated ${balance.runoffLitres.toFixed(1)} L runoff.`, ...state.events].slice(0, 20),
  };
}
