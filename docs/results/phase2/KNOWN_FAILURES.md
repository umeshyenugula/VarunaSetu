# Phase 2 — Known Failures and Limitations

This file summarizes known failure modes and limitations observed or modeled by the
Phase 2 simulation/evaluation pipeline. It is not exhaustive. All results are
synthetic or gridded weather-context replay, not field validation.

## Task 1 — Ablation reproduction
- **Historical headline mismatch (expected):** Reproduced F1 values differ from the
  historical reference (Static 25.45%, Physics-only 63.63%, Peer-only 95.36%,
  Hybrid 96.42%) because the Phase 0B generator, priors, and stress scenario mix
  differ from the unspecified setup that produced those figures. No parameters were
  tuned to close the gap. See `ablation/ABLATION_REPRODUCTION.md`.

## Task 2 — Three-state policy
- **ESCROW mapping affects metrics:** `PROVISIONAL_ESCROW` is neither accepted nor
  rejected at decision time. Confusion matrices therefore depend on treatment
  (rejected/accepted/excluded/resolved). All treatments are reported; `resolved` is
  most faithful when sweeps are modeled. See `three_state/THREE_STATE_EVAL.md`.
- **Synthetic escrow scenarios:** Neighbor/radar corroboration is exercised with
  explicit timing and directed neighbor configuration, not field network behavior.

## Task 3 — Adversarial simulation
- **Colluding neighboring cells:** When `neighbor_compromised_cells >= 2`, compromised
  neighbors can satisfy `NeighborMinCells=2` and corroborate escrows. This is a
  structural limitation (see HANDOFF K1/C2, L2 in prior phases). Detailed conditions
  with mean attack success >= 0.5 are in `adversarial/failure_cases.json`.
- **Peer-majority/coherence attacks:** High attacker fractions can shape peer
  statistics or create coherent escrow conditions depending on density and radar/lag.
  Results are scenario-specific.
- **Radar lag/unavailability:** `lag_after_ttl` and `no_radar` remove timely radar
  corroboration; acceptance then depends on neighbor corroboration/expiry behavior.
- **Not a security proof:** The attacker model is parameterized and synthetic.

## Task 4 — Rainfall retrieval
- **External dependency:** Retrieval requires network access to Open-Meteo. The
  pipeline records URL/timestamp/checksums and can use cached raw JSON. Missing
  intervals are reported, never silently filled.
- **Gridded data:** Open-Meteo archive values are gridded/reanalysis weather context,
  not measured rooftop catchment inflow.

## Task 5 — Rainfall replay
- **Labeled fallback:** If archive CSV is absent, replay uses a deterministic
  `SYNTHETIC_FALLBACK_NOT_ARCHIVE` series and labels it clearly. Final results should
  be interpreted according to `data_source`.
- **Simplified physics:** Catchment differences, device noise, and radar lag are
  simplified models. Archive-derived experiments remain gridded weather-context
  replay, not rooftop ground truth.

## Task 6 — Go/Python consistency
- **Harness scope:** Consistency checks algorithmic decisions/resolutions on synthetic
  fixtures through `sim/gocli` and `sim.three_state`. It does not prove concurrency,
  persistence, deployment, or security properties of the Go implementation.
- **Toolchain availability:** If Go is unavailable, the check is reported as skipped,
  not passed.