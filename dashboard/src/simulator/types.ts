export type BuildingType = 'RESIDENTIAL' | 'APARTMENT' | 'COMMERCIAL' | 'INDUSTRIAL' | 'PUBLIC';
export type SensorType = 'RAIN_GAUGE' | 'WATER_LEVEL' | 'FLOW_METER';

export interface SimulatorBuilding {
  id: string;
  name: string;
  type: BuildingType;
  x: number;
  y: number;
  roofAreaM2: number;
  runoffCoefficient: number;
  zoneId: string;
  hasPit: boolean;
  pitCapacityLitres: number;
  hasTank: boolean;
  tankCapacityLitres: number;
  storedLitres: number;
}

export interface VirtualSensor {
  id: string;
  type: SensorType;
  buildingId: string;
  enabled: boolean;
  fault: 'NONE' | 'NOISE' | 'STUCK' | 'DISCONNECTED' | 'IMPOSSIBLE';
  intervalSeconds: number;
}

export interface WaterBalance {
  rainfallLitres: number;
  runoffLitres: number;
  capturedLitres: number;
  overflowLitres: number;
  rechargedLitres: number;
  storedLitres: number;
}

export interface SimulatorState {
  buildings: SimulatorBuilding[];
  sensors: VirtualSensor[];
  rainfallMmPerHour: number;
  simulationHours: number;
  speed: number;
  running: boolean;
  balance: WaterBalance;
  events: string[];
}
