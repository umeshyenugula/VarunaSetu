import type { CellState, DemoScenario, EscrowRecord, LedgerEntryView, TelemetryReading } from '../types';

// All fixture data is SIMULATED for demonstration. Never treat as production telemetry.
const T = (min: number) => new Date(Date.now() + min * 60_000).toISOString();
const T0 = (min: number) => new Date(Date.now() - min * 60_000).toISOString();

// Hyderabad-area H3 res-9 cells (canonical examples for map rendering)
export const CELLS = {
  c1: '8928308280fffff',
  c2: '8928308280bffff',
  c3: '89283082873ffff',
  c4: '89283082877ffff',
  c5: '8928308283bffff',
};

export const scenarios: DemoScenario[] = [
  { id: 'normal', title: '1. Normal rainfall agreed by peers', description: 'Peers and radar agree; reading accepted.' },
  { id: 'spike', title: '2. Isolated spike quarantined', description: 'Single device spike disagrees with peers/radar; quarantined.' },
  { id: 'escrow', title: '3. Radar-dry, coherent peers create escrow', description: 'Conflict creates PROVISIONAL_ESCROW pending corroboration.' },
  { id: 'confirmed', title: '4. Later radar confirms escrow', description: 'Escrow corroborated and accepted.' },
  { id: 'expired', title: '5. Escrow expires and is quarantined', description: 'No corroboration before expiry; quarantined.' },
  { id: 'collusion', title: '6. Cross-cell collusion (known limitation)', description: 'Known limitation: coordinated cross-cell behavior can mislead; shown for transparency.' },
];

export const readings: TelemetryReading[] = [
  { id: 'r-001', deviceId: 'dev-a1', timestamp: T0(40), h3Index: CELLS.c1, rainfallMm: 12.4, radarRainfallMm: 11.8, peerConsensusMm: 12.1, peerCount: 4, decision: 'ACCEPT', reason: 'RADAR_AND_PEERS_AGREE', simulated: true, scenario: 'normal' },
  { id: 'r-002', deviceId: 'dev-a2', timestamp: T0(39), h3Index: CELLS.c1, rainfallMm: 12.0, radarRainfallMm: 11.8, peerConsensusMm: 12.1, peerCount: 4, decision: 'ACCEPT', reason: 'RADAR_AND_PEERS_AGREE', simulated: true, scenario: 'normal' },
  { id: 'r-003', deviceId: 'dev-b1', timestamp: T0(30), h3Index: CELLS.c2, rainfallMm: 48.0, radarRainfallMm: 6.0, peerConsensusMm: 6.2, peerCount: 5, decision: 'QUARANTINE', reason: 'SPATIAL_PEER_OUTLIER', simulated: true, scenario: 'spike' },
  { id: 'r-004', deviceId: 'dev-c1', timestamp: T0(20), h3Index: CELLS.c3, rainfallMm: 18.0, radarRainfallMm: 0.2, peerConsensusMm: 17.5, peerCount: 4, decision: 'PROVISIONAL_ESCROW', reason: 'RADAR_PEER_CONFLICT_ESCROW', simulated: true, scenario: 'escrow' },
  { id: 'r-005', deviceId: 'dev-d1', timestamp: T0(15), h3Index: CELLS.c4, rainfallMm: 16.5, radarRainfallMm: 0.3, peerConsensusMm: 16.0, peerCount: 4, decision: 'ACCEPT', reason: 'ESCROW_CORROBORATED', simulated: true, scenario: 'confirmed' },
  { id: 'r-006', deviceId: 'dev-e1', timestamp: T0(70), h3Index: CELLS.c5, rainfallMm: 15.0, radarRainfallMm: 0.1, peerConsensusMm: 14.8, peerCount: 4, decision: 'QUARANTINE', reason: 'ESCROW_EXPIRED_UNCORROBORATED', simulated: true, scenario: 'expired' },
  { id: 'r-007', deviceId: 'dev-f1', timestamp: T0(10), h3Index: CELLS.c1, rainfallMm: 22.0, radarRainfallMm: 1.0, peerConsensusMm: 21.0, peerCount: 3, decision: 'PROVISIONAL_ESCROW', reason: 'RADAR_PEER_CONFLICT_ESCROW', simulated: true, scenario: 'collusion' },
  { id: 'r-008', deviceId: 'dev-f2', timestamp: T0(10), h3Index: CELLS.c2, rainfallMm: 22.5, radarRainfallMm: 1.0, peerConsensusMm: 21.2, peerCount: 3, decision: 'PROVISIONAL_ESCROW', reason: 'RADAR_PEER_CONFLICT_ESCROW', simulated: true, scenario: 'collusion' },
];

export const escrows: EscrowRecord[] = [
  { id: 'e-001', deviceId: 'dev-c1', h3Index: CELLS.c3, state: 'PENDING', createdAt: T0(20), expiresAt: T(10), corroboratedAt: null, simulated: true, scenario: 'escrow' },
  { id: 'e-002', deviceId: 'dev-d1', h3Index: CELLS.c4, state: 'CORROBORATED', createdAt: T0(15), expiresAt: T0(0), corroboratedAt: T0(5), simulated: true, scenario: 'confirmed' },
  { id: 'e-003', deviceId: 'dev-e1', h3Index: CELLS.c5, state: 'EXPIRED_QUARANTINED', createdAt: T0(70), expiresAt: T0(55), corroboratedAt: null, simulated: true, scenario: 'expired' },
  { id: 'e-004', deviceId: 'dev-f1', h3Index: CELLS.c1, state: 'PENDING', createdAt: T0(10), expiresAt: T(5), corroboratedAt: null, simulated: true, scenario: 'collusion' },
];

export const ledgerEntries: LedgerEntryView[] = [
  { seq: 0, eventRef: 'telemetry:r-001', prevHash: '0'.repeat(64), hash: '3a7bd3e2360a3d29eea436fcfb7e44c735d117c42d1c1835420b6b9942dd4f1b', timestamp: T0(40), checkpointSignatureStatus: 'valid', checkpointSeq: 0, simulated: true },
  { seq: 1, eventRef: 'telemetry:r-003', prevHash: '3a7bd3e2360a3d29eea436fcfb7e44c735d117c42d1c1835420b6b9942dd4f1b', hash: 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', timestamp: T0(30), checkpointSignatureStatus: 'none', simulated: true },
  { seq: 2, eventRef: 'escrow:e-001', prevHash: 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855', hash: '9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08', timestamp: T0(20), checkpointSignatureStatus: 'valid', checkpointSeq: 2, simulated: true },
];

export function getCellsFromReadings(items: TelemetryReading[]): CellState[] {
  const by = new Map<string, { state: 'ACCEPT' | 'PROVISIONAL_ESCROW' | 'QUARANTINE'; readings: number; simulated: boolean }>();
  const rank = { QUARANTINE: 3, PROVISIONAL_ESCROW: 2, ACCEPT: 1 };
  for (const r of items) {
    const cur = by.get(r.h3Index);
    if (!cur || rank[r.decision] > rank[cur.state]) by.set(r.h3Index, { state: r.decision, readings: (cur?.readings ?? 0) + 1, simulated: cur?.simulated || r.simulated });
    else cur.readings++;
  }
  return [...by.entries()].map(([h3Index, v]) => ({ h3Index, ...v }));
}
