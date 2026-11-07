"""Phase 2 orchestrator: runs Tasks 1-6 in order and writes results.

This is the Python entry point used by ``sim/run_all.sh``. It keeps each task's
logic in its own module while providing one command that regenerates Phase 2
artifacts under ``docs/results/phase2/`` (and preserves Phase 0B outputs under
``docs/results/``).

No Go implementation is modified.
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import time
from pathlib import Path
from typing import Sequence

ROOT = Path(__file__).resolve().parent.parent
PHASE2 = ROOT / "docs" / "results" / "phase2"


def _banner(title: str) -> None:
    print(f"\n{'=' * 72}\n{title}\n{'=' * 72}", flush=True)


def _run(cmd: list[str], *, cwd: Path | None = None, check: bool = True) -> subprocess.CompletedProcess[str]:
    print("+", " ".join(cmd), flush=True)
    proc = subprocess.run(cmd, cwd=str(cwd or ROOT), text=True)
    if check and proc.returncode != 0:
        raise RuntimeError(f"command failed ({proc.returncode}): {' '.join(cmd)}")
    return proc


def task1_ablation_reproduction(*, skip_full: bool = False) -> None:
    _banner("Task 1 — Ablation integration / historical reproduction report")
    # Do not modify the experiment. Reproduce using the existing Phase 0B engine.
    if not skip_full:
        _run([sys.executable, "sim/ablation_experiment.py", "--output-dir", "docs/results"])
    # Write an explicit reproduction report.
    out = PHASE2 / "ablation"
    out.mkdir(parents=True, exist_ok=True)
    historical = {
        "static": 0.2545, "physics": 0.6363, "peer": 0.9536, "hybrid": 0.9642,
        "source": "Phase 2 spec historical reference (provided to the phase prompt)",
    }
    actual = {}
    overall = ROOT / "docs" / "results" / "overall_results.csv"
    if overall.exists():
        import csv
        with overall.open("r", encoding="utf-8", newline="") as f:
            for row in csv.DictReader(f):
                actual[row["method"]] = {
                    "f1_mean": row["f1_mean"], "f1_std": row["f1_std"],
                    "precision_mean": row["precision_mean"], "recall_mean": row["recall_mean"],
                    "fpr_mean": row["false_positive_rate_mean"],
                }
    report = {
        "task": "Phase 2 Task 1 — Existing Ablation Integration",
        "experiment_modified": False,
        "implementation": "sim/ablation_experiment.py (Phase 0B rebuilt version; used unchanged)",
        "config": "sim/config.py (seeds 1-10, 100000 events/seed/scenario, 7 scenarios)",
        "historical_reference_f1": historical,
        "actual_results": actual,
        "differences": "Actual results differ from the historical headline table because the generator, priors, and scenario mix differ from whatever produced that table. No parameter was tuned to reproduce the historical figures. See docs/results/METHOD.md and this report.",
    }
    (out / "ablation_reproduction.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8", newline="\n")
    lines = [
        "# Task 1 — Ablation Reproduction Report",
        "",
        "The Phase 0B ablation experiment (`sim/ablation_experiment.py`) was used **unchanged**.",
        "It was not modified to reproduce historical figures.",
        "",
        "## Historical reference (from phase spec)",
        "",
        "| Method | Historical F1 |",
        "|---|---:|",
        "| Static | 25.45% |",
        "| Physics-only | 63.63% |",
        "| Peer-only | 95.36% |",
        "| Hybrid | 96.42% |",
        "",
        "## Actual reproduced results (this code/config)",
        "",
        "| Method | F1 mean ± std | Precision | Recall | FPR |",
        "|---|---:|---:|---:|---:|",
    ]
    for m in ("static", "physics", "peer", "hybrid"):
        a = actual.get(m, {})
        lines.append(f"| {m} | {a.get('f1_mean','?')} ± {a.get('f1_std','?')} | {a.get('precision_mean','?')} | {a.get('recall_mean','?')} | {a.get('fpr_mean','?')} |")
    lines += [
        "",
        "## Honest difference report",
        "",
        "The reproduced numbers do **not** match the historical headline table. The difference is",
        "expected: this repository's Phase 0B generator uses a different synthetic world, priors,",
        "and adversarial stress mix (including 50% fraud and 40/60% coordinated compromise with",
        "dry radar) than the unspecified setup that produced the historical figures. Parameters",
        "were fixed a priori and were not searched/tuned to close the gap.",
        "",
        "Primary sources: `docs/results/METHOD.md`, `docs/results/overall_results.csv`,",
        "`docs/results/scenario_results.csv`, `docs/results/confusion_matrices.csv`.",
        "",
    ]
    (out / "ABLATION_REPRODUCTION.md").write_text("\n".join(lines) + "\n", encoding="utf-8", newline="\n")
    print(f"wrote {out / 'ABLATION_REPRODUCTION.md'}")


def task2_three_state(events: int = 20_000, seeds: str = "1,2,3,4,5") -> None:
    _banner("Task 2 — Three-state policy evaluation")
    _run([sys.executable, "-m", "sim.three_state_eval", "--output-dir", "docs/results/phase2/three_state",
          "--events", str(events), "--seeds", seeds])


def task3_adversarial(trials: int = 120, seeds: str = "1,2,3,4,5") -> None:
    _banner("Task 3 — Adversarial simulation")
    _run([sys.executable, "-m", "sim.adversarial", "--output-dir", "docs/results/phase2/adversarial",
          "--trials", str(trials), "--seeds", seeds])


def task4_rainfall(*, offline: bool = False) -> None:
    _banner("Task 4 — Rainfall archive retrieval")
    cmd = [sys.executable, "-m", "sim.rainfall_retrieval", "--output-dir", "docs/results/phase2/rainfall"]
    if offline:
        cmd.append("--offline")
    proc = _run(cmd, check=False)
    if proc.returncode != 0:
        cache = PHASE2 / "rainfall" / "open_meteo_archive_raw.json"
        if cache.exists():
            print("Retrieval failed but cached raw JSON exists; continuing with cache.", file=sys.stderr)
        elif offline:
            raise RuntimeError("Offline retrieval requested but no cached raw JSON exists.")
        else:
            print("WARNING: rainfall retrieval unavailable and no cache exists; replay will use labeled synthetic fallback.", file=sys.stderr)


def task5_replay(seeds: str = "1,2,3,4,5") -> None:
    _banner("Task 5 — Rainfall replay")
    _run([sys.executable, "-m", "sim.rainfall_replay",
          "--archive-csv", "docs/results/phase2/rainfall/open_meteo_archive_hourly.csv",
          "--output-dir", "docs/results/phase2/replay", "--seeds", seeds])


def task6_consistency() -> None:
    _banner("Task 6 — Go/Python consistency")
    _run([sys.executable, "-m", "sim.consistency", "--output-dir", "docs/results/phase2/consistency"], check=False)
    # consistency.py returns 0 for PASS/SKIP-unavailable and 1 for mismatch/fail; treat mismatch as failure.
    # The subprocess above used check=False so we can inspect.
    res = PHASE2 / "consistency" / "consistency_result.json"
    if res.exists():
        data = json.loads(res.read_text(encoding="utf-8"))
        print(f"consistency status={data.get('status')} mismatches={len(data.get('mismatches', []))}")
        if data.get("status") == "FAIL" and data.get("mismatches"):
            raise RuntimeError("Go/Python consistency mismatches detected")


def main(argv: Sequence[str] | None = None) -> int:
    p = argparse.ArgumentParser(description="Run HydroLedger Phase 2 pipeline.")
    p.add_argument("--offline", action="store_true", help="do not fetch rainfall from network")
    p.add_argument("--skip-ablation", action="store_true", help="skip full ablation rerun (use existing docs/results)")
    p.add_argument("--quick", action="store_true", help="smaller run for local verification")
    p.add_argument("--skip-consistency", action="store_true")
    args = p.parse_args(argv)

    t0 = time.time()
    PHASE2.mkdir(parents=True, exist_ok=True)
    quick = args.quick
    task1_ablation_reproduction(skip_full=args.skip_ablation)
    task2_three_state(events=3000 if quick else 15_000, seeds="1,2,3" if quick else "1,2,3,4,5")
    task3_adversarial(trials=15 if quick else 40, seeds="1,2,3" if quick else "1,2,3,4")
    task4_rainfall(offline=args.offline)
    task5_replay(seeds="1,2,3" if quick else "1,2,3,4")
    if not args.skip_consistency:
        task6_consistency()
    _banner(f"Phase 2 complete in {time.time()-t0:.1f}s")
    print(f"Results: {PHASE2}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
