"""Seeded rainfall replay from Open-Meteo archive (gridded weather context).

Task 5 (Phase 2): convert hourly archive rainfall into simulated device telemetry.
Labels all archive-derived experiments as **gridded weather-context replay, not
rooftop ground truth**.

Models:
- device noise;
- catchment differences (roof area, runoff coefficient);
- radar lag;
- normal events;
- attack events.

Requires the tidy CSV produced by ``sim.rainfall_retrieval``. If absent, the
script can generate a clearly labeled synthetic fallback so the pipeline remains
reproducible offline; the fallback is NOT archive data.
"""

from __future__ import annotations

import argparse
import csv
import json
import math
import random
import statistics
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Sequence

try:
    from . import three_state as ts
except ImportError:
    import three_state as ts  # type: ignore[no-redef]

SEEDS = (1, 2, 3, 4, 5)
DEVICES_PER_CELL = 12
CELLS = 8
RADAR_LAG_OPTIONS_H = (0, 1, 2, 3)  # hours
LABEL = "gridded weather-context replay (Open-Meteo archive context), not rooftop ground truth"


@dataclass
class ReplayConfig:
    devices_per_cell: int = DEVICES_PER_CELL
    cells: int = CELLS
    attack_fraction: float = 0.10
    radar_lag_hours: int = 1
    seed: int = 1
    events_per_hour: int = 1  # one reading per device per hour by default


def _read_archive(csv_path: Path) -> list[dict[str, Any]]:
    rows = []
    with csv_path.open("r", encoding="utf-8", newline="") as f:
        reader = csv.DictReader(f)
        for row in reader:
            rows.append(row)
    return rows


def _synthetic_fallback(start: str = "2025-07-01", end: str = "2025-09-30") -> list[dict[str, Any]]:
    # Deterministic synthetic hourly series with monsoon-like bursts. Clearly not archive.
    rng = random.Random(20250701)
    start_dt = datetime.fromisoformat(start)
    end_dt = datetime.fromisoformat(end)
    hours = (end_dt - start_dt).days * 24 + 24
    rows = []
    for h in range(hours):
        t = start_dt + timedelta(hours=h)
        day = h / 24.0
        seasonal = max(0.0, 6.0 * math.sin(math.pi * day / 92.0))
        burst = 0.0
        if rng.random() < 0.08:
            burst = rng.uniform(2.0, 18.0) * math.exp(-0.5 * ((h % 24 - 16) / 4.0) ** 2)
        rain = max(0.0, seasonal * rng.uniform(0.0, 0.8) + burst + rng.gauss(0, 0.05))
        rows.append({"time": t.strftime("%Y-%m-%dT%H:%M"), "rain_mm": rain, "precipitation_mm": rain,
                     "source": "SYNTHETIC_FALLBACK_NOT_ARCHIVE"})
    return rows


def load_hourly_rain(archive_csv: Path) -> tuple[list[dict[str, Any]], str]:
    if archive_csv.exists():
        rows = _read_archive(archive_csv)
        # Normalize field names from retrieval output.
        out = []
        for r in rows:
            rain = r.get("rain_mm") or r.get("rain") or r.get("precipitation_mm") or r.get("precipitation") or "0"
            try:
                rv = float(rain)
            except (TypeError, ValueError):
                rv = 0.0
            out.append({"time": r.get("time"), "rain_mm": rv})
        return out, "archive"
    return _synthetic_fallback(), "synthetic_fallback"


def _parse_local_time(value: str) -> datetime:
    # Treat local archive timestamps as naive local context; attach UTC for stable ordering in sim.
    dt = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt


def generate_replay(hourly: Sequence[dict[str, Any]], cfg: ReplayConfig) -> list[dict[str, Any]]:
    rng = random.Random(cfg.seed)
    # Per-device catchment differences.
    devices = []
    for c in range(cfg.cells):
        for d in range(cfg.devices_per_cell):
            devices.append({
                "cell": f"cell-{c}", "device": f"dev-{c}-{d}",
                "roof_area_m2": rng.uniform(60.0, 220.0),
                "runoff_coeff": rng.uniform(0.75, 0.95),
                "bias_mm": rng.gauss(0.0, 0.15),
            })

    # Precompute lagged radar series per cell (shared radar, lagged).
    rain_series = [max(0.0, float(h["rain_mm"])) for h in hourly]
    lag = max(0, cfg.radar_lag_hours)

    events: list[dict[str, Any]] = []
    for hi, h in enumerate(hourly):
        t0 = _parse_local_time(str(h["time"]))
        true_rain = max(0.0, float(h["rain_mm"]))
        radar_idx = hi - lag
        radar_rain = rain_series[radar_idx] if radar_idx >= 0 else 0.0
        radar_mm = max(0.0, radar_rain + rng.gauss(0.0, 0.1))
        for step in range(cfg.events_per_hour):
            when = t0 + timedelta(minutes=step * (60 // max(1, cfg.events_per_hour)))
            for dev in devices:
                # Device observation of gridded rain context (not rooftop truth).
                catchment_factor = rng.uniform(0.85, 1.15)
                noise = rng.gauss(0.0, 0.2)
                observed_mm = max(0.0, true_rain * catchment_factor + dev["bias_mm"] + noise)
                fraudulent = rng.random() < cfg.attack_fraction
                if fraudulent:
                    # Attack: inflate claim, sometimes under dry radar.
                    claim_mm = max(observed_mm + rng.uniform(3.0, 12.0), rng.uniform(5.0, 15.0))
                    if rng.random() < 0.5:
                        radar_mm_event = min(radar_mm, 0.3)
                    else:
                        radar_mm_event = radar_mm
                else:
                    claim_mm = observed_mm
                    radar_mm_event = radar_mm
                events.append({
                    "time": when.isoformat(),
                    "hour_index": hi,
                    "cell_id": dev["cell"],
                    "device_id": dev["device"],
                    "archive_rain_mm": true_rain,
                    "radar_mm": radar_mm_event,
                    "radar_lag_hours": lag,
                    "observed_context_mm": observed_mm,
                    "claimed_mm": claim_mm,
                    "claimed_litres": claim_mm * dev["roof_area_m2"] * dev["runoff_coeff"],
                    "roof_area_m2": dev["roof_area_m2"],
                    "runoff_coeff": dev["runoff_coeff"],
                    "stored_litres": rng.uniform(0.0, 500.0),
                    "capacity_litres": 100_000.0,
                    "fraudulent": fraudulent,
                    "label": LABEL,
                })
    return events


def evaluate_replay(events: Sequence[dict[str, Any]]) -> dict[str, Any]:
    v = ts.ThreeStateVerifier()
    counts = {"total": 0, "accept": 0, "escrow": 0, "quarantine": 0,
              "tp": 0, "fp": 0, "fn": 0, "tn": 0, "escrow_count": 0}
    for e in events:
        r = ts.Reading(
            device_id=e["device_id"], timestamp=_parse_local_time(e["time"]),
            claimed_litres=float(e["claimed_litres"]), roof_area_m2=float(e["roof_area_m2"]),
            runoff_coeff=float(e["runoff_coeff"]), stored_litres=float(e["stored_litres"]),
            capacity_litres=float(e["capacity_litres"]), radar_mm=float(e["radar_mm"]),
            cell_id=e["cell_id"],
        )
        d = v.evaluate(r)
        counts["total"] += 1
        counts[d.state.lower() if d.state != ts.PROVISIONAL_ESCROW else "escrow"] += 1
        fraud = bool(e["fraudulent"])
        # Resolved-style simple mapping for replay summary: escrow treated as not accepted.
        accepted = d.state == ts.ACCEPT
        if fraud and not accepted:
            counts["tp"] += 1
        elif fraud and accepted:
            counts["fn"] += 1
        elif not fraud and not accepted:
            counts["fp"] += 1
        else:
            counts["tn"] += 1
    tp, fp, fn, tn = counts["tp"], counts["fp"], counts["fn"], counts["tn"]
    prec = tp / (tp + fp) if (tp + fp) else 0.0
    rec = tp / (tp + fn) if (tp + fn) else 0.0
    f1 = 2 * prec * rec / (prec + rec) if (prec + rec) else 0.0
    fpr = fp / (fp + tn) if (fp + tn) else 0.0
    counts.update({"precision": prec, "recall": rec, "f1": f1, "fpr": fpr})
    return counts


def run(output_dir: Path, archive_csv: Path, seeds: Sequence[int] = SEEDS,
        cells: int = CELLS, devices_per_cell: int = DEVICES_PER_CELL) -> None:
    output_dir.mkdir(parents=True, exist_ok=True)
    hourly, source = load_hourly_rain(archive_csv)
    summary_rows = []
    all_metrics = []

    for lag in RADAR_LAG_OPTIONS_H:
        for seed in seeds:
            cfg = ReplayConfig(seed=seed, radar_lag_hours=lag, cells=cells, devices_per_cell=devices_per_cell)
            events = generate_replay(hourly, cfg)
            metrics = evaluate_replay(events)
            row = {
                "data_source": source, "label": LABEL, "seed": seed, "radar_lag_hours": lag,
                "cells": cells, "devices_per_cell": devices_per_cell,
                "hours": len(hourly), "events": metrics["total"],
                "accept": metrics["accept"], "escrow": metrics["escrow"], "quarantine": metrics["quarantine"],
                "tp": metrics["tp"], "fp": metrics["fp"], "fn": metrics["fn"], "tn": metrics["tn"],
                "precision": f"{metrics['precision']:.6f}", "recall": f"{metrics['recall']:.6f}",
                "f1": f"{metrics['f1']:.6f}", "fpr": f"{metrics['fpr']:.6f}",
            }
            summary_rows.append(row)
            all_metrics.append((lag, metrics))
            # Save a small sample for inspection (first seed/lag=1).
            if seed == seeds[0] and lag == 1:
                sample_path = output_dir / "replay_sample_events.csv"
                with sample_path.open("w", newline="", encoding="utf-8") as f:
                    if events:
                        w = csv.DictWriter(f, fieldnames=list(events[0].keys()))
                        w.writeheader()
                        for e in events[:5000]:
                            w.writerow(e)

    out_csv = output_dir / "replay_results.csv"
    with out_csv.open("w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=list(summary_rows[0].keys()))
        w.writeheader()
        w.writerows(summary_rows)

    report = [
        "# Rainfall Replay Results",
        "",
        f"**Label:** {LABEL}",
        "",
        f"- Archive CSV: `{archive_csv.as_posix()}`",
        f"- Data source used: `{source}`" + (" (WARNING: not Open-Meteo archive; offline fallback)" if source != "archive" else ""),
        f"- Hourly rows: {len(hourly)}",
        f"- Seeds: {list(seeds)}",
        f"- Radar lag options (hours): {list(RADAR_LAG_OPTIONS_H)}",
        f"- Cells: {CELLS}, devices/cell: {DEVICES_PER_CELL}",
        "",
        "## Policy note",
        "",
        "Replay evaluation uses the three-state verifier mirror. For the summary table,",
        "`PROVISIONAL_ESCROW` is treated as not accepted (conservative binary mapping); see",
        "`docs/results/phase2/three_state/THREE_STATE_EVAL.md` for alternate escrow treatments.",
        "",
        "## Results",
        "",
        "See `replay_results.csv`. A 5000-event sample (seed/lag reference case) is in",
        "`replay_sample_events.csv` when generated.",
        "",
        "## Limitations",
        "",
        "- Gridded archive rainfall is weather context, not measured rooftop catchment inflow.",
        "- Device physics/catchment losses are simplified (noise + catchment factors).",
        "- Radar lag is modeled as hourly series lag, not instrument/network physics.",
        "- Attack labels are synthetic injections for evaluation.",
        "",
    ]
    (output_dir / "REPLAY.md").write_text("\n".join(report) + "\n", encoding="utf-8", newline="\n")
    print(f"wrote {out_csv} (source={source}, hours={len(hourly)})")


def main(argv: Sequence[str] | None = None) -> int:
    p = argparse.ArgumentParser(description="Rainfall replay (Phase 2).")
    p.add_argument("--archive-csv", default="docs/results/phase2/rainfall/open_meteo_archive_hourly.csv")
    p.add_argument("--output-dir", default="docs/results/phase2/replay")
    p.add_argument("--seeds", default="1,2,3,4,5")
    p.add_argument("--cells", type=int, default=CELLS)
    p.add_argument("--devices-per-cell", type=int, default=DEVICES_PER_CELL)
    args = p.parse_args(argv)
    seeds = tuple(int(s.strip()) for s in args.seeds.split(",") if s.strip())
    run(Path(args.output_dir), Path(args.archive_csv), seeds=seeds, cells=args.cells, devices_per_cell=args.devices_per_cell)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
