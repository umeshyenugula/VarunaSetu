# Rainfall Replay Results

**Label:** gridded weather-context replay (Open-Meteo archive context), not rooftop ground truth

- Archive CSV: `docs/results/phase2/rainfall/open_meteo_archive_hourly.csv`
- Data source used: `archive`
- Hourly rows: 2208
- Seeds: [1, 2, 3]
- Radar lag options (hours): [0, 1, 2, 3]
- Cells: 8, devices/cell: 12

## Policy note

Replay evaluation uses the three-state verifier mirror. For the summary table,
`PROVISIONAL_ESCROW` is treated as not accepted (conservative binary mapping); see
`docs/results/phase2/three_state/THREE_STATE_EVAL.md` for alternate escrow treatments.

## Results

See `replay_results.csv`. A 5000-event sample (seed/lag reference case) is in
`replay_sample_events.csv` when generated.

## Limitations

- Gridded archive rainfall is weather context, not measured rooftop catchment inflow.
- Device physics/catchment losses are simplified (noise + catchment factors).
- Radar lag is modeled as hourly series lag, not instrument/network physics.
- Attack labels are synthetic injections for evaluation.

