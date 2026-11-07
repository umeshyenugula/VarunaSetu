# HydroLedger Dashboard Demonstration Mode (Phase 5)

All scenario data in the dashboard demo is **SIMULATED** and visibly labeled. It is for explanation only and must not be treated as production telemetry or as evidence of real-world performance.

## Scripted scenarios

1. **Normal rainfall agreed upon by peers**
   - Devices in the same H3 cell report consistent rainfall; radar agrees.
   - Expected: `ACCEPT` / `RADAR_AND_PEERS_AGREE`.

2. **Isolated spike quarantined**
   - A single device reports a large spike while peers/radar are low.
   - Expected: `QUARANTINE` / `SPATIAL_PEER_OUTLIER`.

3. **Radar-dry but coherent peers create escrow**
   - Radar is low/dry but coherent peers report rain (conflict).
   - Expected: `PROVISIONAL_ESCROW` / `RADAR_PEER_CONFLICT_ESCROW` with expiry countdown.

4. **Later radar confirms escrow**
   - A provisional escrow receives later corroborating evidence.
   - Expected: escrow becomes `CORROBORATED`; reading outcome shown as accepted/corroborated.

5. **Escrow expires and becomes quarantined**
   - No corroboration arrives before expiry.
   - Expected: `EXPIRED_QUARANTINED` / `ESCROW_EXPIRED_UNCORROBORATED`.

6. **Cross-cell collusion demonstrates the known limitation**
   - Coordinated reports across cells create misleading provisional states.
   - Expected UI behavior: show provisional/conflict states but present this as a **known limitation** (see `HANDOFF.md`/verifier contracts). The dashboard must not imply collusion is solved.

## Running the demo

```bash
cd dashboard
npm install
npm run dev
```

Use the “Demonstration mode (6 scripted scenarios)” selector to filter the map, analytics, readings, and escrow panels. The ledger viewer provides a structural integrity check interface; authoritative hash/Ed25519 verification is implemented in Phase 4 Go code (`ledger/`).
