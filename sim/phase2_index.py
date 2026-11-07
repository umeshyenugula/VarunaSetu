"""Generate the Phase 2 results index after all tasks have run."""

from __future__ import annotations

import json
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
PHASE2 = ROOT / "docs" / "results" / "phase2"


def main() -> int:
    PHASE2.mkdir(parents=True, exist_ok=True)
    def exists(rel: str) -> str:
        return "yes" if (PHASE2 / rel).exists() or (ROOT / rel).exists() else "no"
    consistency = {}
    cpath = PHASE2 / "consistency" / "consistency_result.json"
    if cpath.exists():
        consistency = json.loads(cpath.read_text(encoding="utf-8"))
    rainfall = {}
    rpath = PHASE2 / "rainfall" / "open_meteo_archive_metadata.json"
    if rpath.exists():
        rainfall = json.loads(rpath.read_text(encoding="utf-8"))

    lines = [
        "# Phase 2 Results Index",
        "",
        "Generated artifacts for simulation, rainfall replay and evaluation.",
        "All archive-derived replay work is labeled **gridded weather-context replay, not rooftop ground truth**.",
        "No Go implementation was modified.",
        "",
        "## Task outputs",
        "",
        "| Task | Primary output | Present |",
        "|---|---|---|",
        "| 1 Ablation reproduction | `docs/results/phase2/ablation/ABLATION_REPRODUCTION.md` | " + exists("docs/results/phase2/ablation/ABLATION_REPRODUCTION.md") + " |",
        "| 1 Ablation tables (Phase 0B engine) | `docs/results/overall_results.csv` | " + exists("docs/results/overall_results.csv") + " |",
        "| 2 Three-state evaluation | `docs/results/phase2/three_state/THREE_STATE_EVAL.md` | " + exists("docs/results/phase2/three_state/THREE_STATE_EVAL.md") + " |",
        "| 3 Adversarial simulation | `docs/results/phase2/adversarial/ADVERSARIAL.md` | " + exists("docs/results/phase2/adversarial/ADVERSARIAL.md") + " |",
        "| 4 Rainfall retrieval | `docs/results/phase2/rainfall/open_meteo_archive_metadata.json` | " + exists("docs/results/phase2/rainfall/open_meteo_archive_metadata.json") + " |",
        "| 5 Rainfall replay | `docs/results/phase2/replay/REPLAY.md` | " + exists("docs/results/phase2/replay/REPLAY.md") + " |",
        "| 6 Go/Python consistency | `docs/results/phase2/consistency/CONSISTENCY.md` | " + exists("docs/results/phase2/consistency/CONSISTENCY.md") + " |",
        "| Known failures | `docs/results/phase2/KNOWN_FAILURES.md` | " + exists("docs/results/phase2/KNOWN_FAILURES.md") + " |",
        "",
        "## Rainfall retrieval status",
        "",
        f"- Status: `{rainfall.get('status', 'not run')}`",
        f"- Rows: `{rainfall.get('actual_hourly_rows', '?')}` / expected `{rainfall.get('expected_hourly_rows', '?')}`",
        f"- URL recorded: `{rainfall.get('request_url', '?')}`",
        f"- Retrieved (UTC): `{rainfall.get('retrieved_at_utc', '?')}`",
        f"- Raw SHA-256: `{(rainfall.get('files') or {}).get('raw_sha256', '?')}`",
        "",
        "## Consistency status",
        "",
        f"- Status: `{consistency.get('status', 'not run')}`",
        f"- Decisions compared: `{consistency.get('decisions_compared', '?')}`",
        f"- Resolutions compared: `{consistency.get('resolutions_compared', '?')}`",
        f"- Mismatches: `{len(consistency.get('mismatches') or [])}`",
        "",
        "## Reproducibility",
        "",
        "```bash",
        "bash sim/run_all.sh",
        "```",
        "",
        "See `sim/run_all.sh` and `docs/results/phase2/RUN_LOG.md` for details.",
        "All archive-derived experiments are labeled gridded weather-context replay, not rooftop ground truth.",
        "",
    ]
    (PHASE2 / "INDEX.md").write_text("\n".join(lines) + "\n", encoding="utf-8", newline="\n")
    print(f"wrote {PHASE2 / 'INDEX.md'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
