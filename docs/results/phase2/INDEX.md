# Phase 2 Results Index

Generated artifacts for simulation, rainfall replay and evaluation.
All archive-derived replay work is labeled **gridded weather-context replay, not rooftop ground truth**.
No Go implementation was modified.

## Task outputs

| Task | Primary output | Present |
|---|---|---|
| 1 Ablation reproduction | `docs/results/phase2/ablation/ABLATION_REPRODUCTION.md` | yes |
| 1 Ablation tables (Phase 0B engine) | `docs/results/overall_results.csv` | yes |
| 2 Three-state evaluation | `docs/results/phase2/three_state/THREE_STATE_EVAL.md` | yes |
| 3 Adversarial simulation | `docs/results/phase2/adversarial/ADVERSARIAL.md` | yes |
| 4 Rainfall retrieval | `docs/results/phase2/rainfall/open_meteo_archive_metadata.json` | yes |
| 5 Rainfall replay | `docs/results/phase2/replay/REPLAY.md` | yes |
| 6 Go/Python consistency | `docs/results/phase2/consistency/CONSISTENCY.md` | yes |
| Known failures | `docs/results/phase2/KNOWN_FAILURES.md` | yes |

## Rainfall retrieval status

- Status: `OK`
- Rows: `2208` / expected `2208`
- URL recorded: `https://archive-api.open-meteo.com/v1/archive?latitude=17.5449&longitude=78.3864&start_date=2025-07-01&end_date=2025-09-30&hourly=rain%2Cprecipitation&timezone=Asia%2FKolkata`
- Retrieved (UTC): `2026-11-07T09:12:01Z`
- Raw SHA-256: `8e877feb58bbfd9724c26522307a04a66d1004fd3461e20132f7cdceecaa077e`

## Consistency status

- Status: `PASS`
- Decisions compared: `58`
- Resolutions compared: `4`
- Mismatches: `0`

## Reproducibility

```bash
bash sim/run_all.sh
```

See `sim/run_all.sh` and `docs/results/phase2/RUN_LOG.md` for details.
All archive-derived experiments are labeled gridded weather-context replay, not rooftop ground truth.

