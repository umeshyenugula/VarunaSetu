export type DecisionState = 'ACCEPT' | 'PROVISIONAL_ESCROW' | 'QUARANTINE';
export type EscrowState = 'PENDING' | 'CORROBORATED' | 'EXPIRED_QUARANTINED';

export interface TelemetryReading {
  id: string;
  deviceId: string;
  timestamp: string;
  h3Index: string;
  rainfallMm: number;
  radarRainfallMm: number;
  peerConsensusMm: number | null;
  peerCount: number;
  decision: DecisionState;
  reason: string;
}

export interface EscrowRecord {
  id: string;
  deviceId: string;
  h3Index: string;
  state: EscrowState;
  createdAt: string;
  expiresAt: string;
  corroboratedAt?: string | null;
}

export interface LedgerEntryView {
  seq: number;
  eventRef: string;
  prevHash: string;
  hash: string;
  timestamp: string;
  checkpointSignatureStatus: 'valid' | 'invalid' | 'none' | 'unknown';
  checkpointSeq?: number;
}

export interface AnalyticsSummary {
  totalReadings: number;
  accepted: number;
  provisional: number;
  quarantined: number;
  escrowResolutionRate: number | null;
  activeDevices: number;
}

export interface CellState {
  h3Index: string;
  state: DecisionState;
  readings: number;
}
