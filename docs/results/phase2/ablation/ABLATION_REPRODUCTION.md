# Task 1 — Ablation Reproduction Report

The Phase 0B ablation experiment (`sim/ablation_experiment.py`) was used **unchanged**.
It was not modified to reproduce historical figures.

## Historical reference (from phase spec)

| Method | Historical F1 |
|---|---:|
| Static | 25.45% |
| Physics-only | 63.63% |
| Peer-only | 95.36% |
| Hybrid | 96.42% |

## Actual reproduced results (this code/config)

| Method | F1 mean ± std | Precision | Recall | FPR |
|---|---:|---:|---:|---:|
| static | 0.269232 ± 0.007420 | 0.730538 | 0.165091 | 0.026826 |
| physics | 0.862170 ± 0.001385 | 0.987049 | 0.765345 | 0.004398 |
| peer | 0.643815 ± 0.001343 | 0.999994 | 0.474728 | 0.000001 |
| hybrid | 0.926451 ± 0.001340 | 0.988550 | 0.871699 | 0.004422 |

## Honest difference report

The reproduced numbers do **not** match the historical headline table. The difference is
expected: this repository's Phase 0B generator uses a different synthetic world, priors,
and adversarial stress mix (including 50% fraud and 40/60% coordinated compromise with
dry radar) than the unspecified setup that produced the historical figures. Parameters
were fixed a priori and were not searched/tuned to close the gap.

Primary sources: `docs/results/METHOD.md`, `docs/results/overall_results.csv`,
`docs/results/scenario_results.csv`, `docs/results/confusion_matrices.csv`.

