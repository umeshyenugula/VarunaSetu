"""Three-state policy evaluation (ACCEPT / PROVISIONAL_ESCROW / QUARANTINE).

Task 2 (Phase 2): evaluate the escrow-aware verifier policy on synthetic
scenarios. This uses ``sim.three_state.ThreeStateVerifier`` (a Python mirror of
the Go verifier) and models:

- escrow creation (RADAR_PEER_CONFLICT_ESCROW);
- expiry (ESCROW_EXPIRED_UNCORROBORATED);
- radar corroboration (later reading with RadarMM > RadarDryMM);
- neighbor corroboration (configured neighbor cells with recent rainy medians);
- quarantine on expiry.

Confusion-matrix treatment of ESCROW is documented explicitly. The generated
tables report alternate treatments so the effect of the mapping is visible.
"""

from __future__ import annotations

import argparse
import csv
import json
import math
import os
import random
import statistics
import sys
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Iterable, Sequence

try:
    from . import three_state as ts
except ImportError:
    import three_state as ts  # type: ignore[no-redef]

BASE = datetime(2025, 7, 1, tzinfo=timezone.utc)
SEEDS = (1, 2, 3, 4, 5)
EVENTS_PER_SEED = 20_000

# Scenario design is intentionally small and explicit. These are synthetic
# policy-evaluation scenarios, not field distributions.
SCENARIOS = (
    {
        "key": "honest_rain_agree",
        "label": "Honest rain, radar and peers agree",
        "fraud_fraction": 0.0,
        "mode": "agree",
        "description": "Honest devices report coherent rain under a rainy radar; no fraud.",
    },
    {
        "key": "radar_conflict_then_corroborated",
        "label": "Radar/peer conflict, later radar corroborates",
        "fraud_fraction": 0.0,
        "mode": "escrow_radar",
        "description": "Peers report coherent rain while radar is dry; a later reading in the same cell has RadarMM > 0.5 before TTL expiry, so escrow resolves to ACCEPT/ESCROW_CORROBORATED.",
    },
    {
        "key": "radar_conflict_neighbor_corroborated",
        "label": "Radar/peer conflict, neighbor cells corroborate",
        "fraud_fraction": 0.0,
        "mode": "escrow_neighbor",
        "description": "Cell escrow is created under dry radar; two configured neighbor cells each have >=3 recent devices with rainy median before expiry.",
    },
    {
        "key": "radar_conflict_expires",
        "label": "Radar/peer conflict expires uncorroborated",
        "fraud_fraction": 1.0,
        "mode": "escrow_expire",
        "description": "Coherent peer rain under dry radar with no later radar and no neighbor corroboration; escrow expires to QUARANTINE/ESCROW_EXPIRED_UNCORROBORATED. Treated as fraud for evaluation because the claim is fabricated in this scenario.",
    },
    {
        "key": "mixed_fraud_and_honest",
        "label": "Mixed honest rain and isolated fraud",
        "fraud_fraction": 0.15,
        "mode": "mixed",
        "description": "Rainy radar with coherent honest peers; 15% of readings are large positive outliers (fraud).",
    },
)

ESCROW_TREATMENTS = (
    ("rejected", "Count PROVISIONAL_ESCROW as rejected/not-accepted for binary fraud metrics. Conservative: holds value until resolved."),
    ("accepted", "Count PROVISIONAL_ESCROW as accepted (alternate). Optimistic: can over-credit unresolved claims."),
    ("excluded", "Exclude PROVISIONAL_ESCROW readings from binary confusion counts and report their rate separately."),
    ("resolved", "Evaluate by resolved escrow outcome when available: ESCROW_CORROBORATED->accepted, ESCROW_EXPIRED_UNCORROBORATED->rejected. Unresolved at horizon counted per --unresolved policy."),
)


@dataclass
class Counts:
    tn: int = 0
    fp: int = 0
    fn: int = 0
    tp: int = 0
    escrow: int = 0
    total: int = 0
    escrow_resolved_accept: int = 0
    escrow_resolved_reject: int = 0
    escrow_unresolved: int = 0

    def add(self, fraudulent: bool, accepted: bool | None, state: str) -> None:
        self.total += 1
        if state == ts.PROVISIONAL_ESCROW:
            self.escrow += 1
        if accepted is None:
            return
        if fraudulent and not accepted:
            self.tp += 1
        elif fraudulent and accepted:
            self.fn += 1
        elif not fraudulent and not accepted:
            self.fp += 1
        else:
            self.tn += 1

    def metrics(self) -> dict[str, float]:
        prec = self.tp / (self.tp + self.fp) if (self.tp + self.fp) else 0.0
        rec = self.tp / (self.tp + self.fn) if (self.tp + self.fn) else 0.0
        f1 = 2 * prec * rec / (prec + rec) if (prec + rec) else 0.0
        fpr = self.fp / (self.fp + self.tn) if (self.fp + self.tn) else 0.0
        escrow_rate = self.escrow / self.total if self.total else 0.0
        return {
            "precision": prec,
            "recall": rec,
            "f1": f1,
            "fpr": fpr,
            "escrow_rate": escrow_rate,
        }


def _fmt(v: float) -> str:
    return f"{v:.6f}"


def _mean_std(values: Sequence[float]) -> tuple[float, float]:
    if not values:
        return 0.0, 0.0
    m = statistics.fmean(values)
    return m, (statistics.stdev(values) if len(values) > 1 else 0.0)


def _reading(
    device: str,
    cell: str,
    when: datetime,
    depth_mm: float,
    radar_mm: float,
    *,
    area: float = 100.0,
    runoff: float = 1.0,
) -> ts.Reading:
    return ts.Reading(
        device_id=device,
        timestamp=when,
        claimed_litres=depth_mm * area * runoff,
        roof_area_m2=area,
        runoff_coeff=runoff,
        stored_litres=0.0,
        capacity_litres=100_000.0,
        radar_mm=radar_mm,
        cell_id=cell,
    )


@dataclass
class EvalEvent:
    reading: ts.Reading
    fraudulent: bool
    sweep_after: datetime | None = None


_SCENARIO_INDEX = {sc["key"]: i for i, sc in enumerate(SCENARIOS)}


def _generate_scenario(key: str, seed: int, n: int) -> tuple[list[EvalEvent], dict[str, list[str]]]:
    # Stable seed derivation (never Python's randomized hash()).
    rng = random.Random(seed * 10_000 + _SCENARIO_INDEX[key] * 1_000 + 17)
    events: list[EvalEvent] = []
    neighbors: dict[str, list[str]] = {}

    def t(i: int) -> datetime:
        # Spread events over time to avoid peer-window collapse; keep < TTL unless intended.
        return BASE + timedelta(seconds=i * 30)

    if key == "honest_rain_agree":
        # Build coherent peer context: for each "round", 4 peers report similar rain,
        # then a 5th target reports similar rain under rainy radar -> ACCEPT/AGREE.
        rounds = max(1, n // 5)
        for i in range(rounds):
            base_t = t(i)
            cell = f"c{i % 8}"
            base_depth = rng.uniform(4.0, 7.0)
            for p in range(4):
                depth = base_depth + rng.gauss(0.0, 0.05)
                events.append(EvalEvent(_reading(f"ha{i}-{p}", cell, base_t + timedelta(seconds=p), depth, 10.0), False))
            target_depth = base_depth + rng.gauss(0.0, 0.05)
            events.append(EvalEvent(_reading(f"ha{i}-t", cell, base_t + timedelta(seconds=5), target_depth, 10.0), False))

    elif key == "radar_conflict_then_corroborated":
        # Create isolated escrow cases with corroborating radar before expiry.
        # In this scenario the claim is treated as honest/contextual (fraud_fraction 0)
        # and resolves to corroborated acceptance.
        for i in range(max(1, n // 20)):
            base = BASE + timedelta(seconds=i * 1200)
            cell = f"r{i % 6}"
            for p in range(4):
                events.append(EvalEvent(_reading(f"rp{i}-{p}", cell, base + timedelta(seconds=p), 9.0, 0.0), False))
            target_t = base + timedelta(seconds=5)
            events.append(EvalEvent(_reading(f"rt{i}", cell, target_t, 9.0, 0.0), False))
            events.append(EvalEvent(_reading(f"rl{i}", cell, target_t + timedelta(seconds=120), 9.0, 1.0), False,
                                    sweep_after=target_t + timedelta(seconds=180)))

    elif key == "radar_conflict_neighbor_corroborated":
        neighbors = {
            "cn0": ["nn1", "nn2"], "cn1": ["nn3", "nn4"], "cn2": ["nn5", "nn6"],
            "cn3": ["nn1", "nn3"], "cn4": ["nn2", "nn4"], "cn5": ["nn5", "nn1"],
        }
        for i in range(max(1, n // 30)):
            base = BASE + timedelta(seconds=i * 1200)
            cell = f"cn{i % 6}"
            for p in range(4):
                events.append(EvalEvent(_reading(f"np{i}-{p}", cell, base + timedelta(seconds=p), 9.0, 0.0), False))
            target_t = base + timedelta(seconds=5)
            events.append(EvalEvent(_reading(f"nt{i}", cell, target_t, 9.0, 0.0), False))
            for nb in neighbors.get(cell, [])[:2]:
                for d in range(3):
                    events.append(EvalEvent(_reading(f"{nb}-d{d}-{i}", nb, target_t + timedelta(seconds=60), 3.5, 0.0), False))
            events.append(EvalEvent(_reading(f"probe{i}", cell, target_t + timedelta(seconds=61), 0.1, 0.0), False,
                                    sweep_after=target_t + timedelta(seconds=120)))

    elif key == "radar_conflict_expires":
        # Fabricated coherent claims under dry radar, never corroborated.
        for i in range(max(1, n // 20)):
            base = BASE + timedelta(seconds=i * 1200)
            cell = f"e{i % 6}"
            for p in range(4):
                events.append(EvalEvent(_reading(f"ep{i}-{p}", cell, base + timedelta(seconds=p), 9.0, 0.0), True))
            target_t = base + timedelta(seconds=5)
            events.append(EvalEvent(_reading(f"et{i}", cell, target_t, 9.0, 0.0), True,
                                    sweep_after=target_t + timedelta(seconds=901)))

    elif key == "mixed_fraud_and_honest":
        # Build honest peer context per cell, then inject fraud spikes.
        for i in range(max(1, n // 6)):
            base_t = t(i)
            cell = f"m{i % 10}"
            base_depth = rng.uniform(4.0, 6.5)
            for p in range(4):
                events.append(EvalEvent(_reading(f"mh{i}-{p}", cell, base_t + timedelta(seconds=p),
                                                 base_depth + rng.gauss(0, 0.05), 10.0), False))
            if rng.random() < 0.15:
                events.append(EvalEvent(_reading(f"mf{i}", cell, base_t + timedelta(seconds=5), 40.0, 10.0), True))
            else:
                events.append(EvalEvent(_reading(f"mh{i}-t", cell, base_t + timedelta(seconds=5),
                                                 base_depth + rng.gauss(0, 0.05), 10.0), False))

    else:
        raise ValueError(key)

    events.sort(key=lambda e: (e.reading.timestamp, e.reading.cell_id, e.reading.device_id))
    return events, neighbors


def _accepted_for_treatment(
    state: str,
    treatment: str,
    resolution_state: str | None,
    unresolved_policy: str,
) -> bool | None:
    if treatment == "rejected":
        return state == ts.ACCEPT
    if treatment == "accepted":
        return state != ts.QUARANTINE
    if treatment == "excluded":
        return None if state == ts.PROVISIONAL_ESCROW else (state == ts.ACCEPT)
    if treatment == "resolved":
        if state == ts.ACCEPT:
            return True
        if state == ts.QUARANTINE:
            return False
        # escrow
        if resolution_state == ts.ACCEPT:
            return True
        if resolution_state == ts.QUARANTINE:
            return False
        if unresolved_policy == "rejected":
            return False
        if unresolved_policy == "accepted":
            return True
        return None
    raise ValueError(treatment)


def evaluate(events: Sequence[EvalEvent], neighbors: dict[str, list[str]], *, unresolved_policy: str = "rejected") -> dict[str, Counts]:
    verifier = ts.ThreeStateVerifier()
    for cell, nbs in neighbors.items():
        verifier.set_neighbors(cell, nbs)

    # Track each reading's initial state and final resolution for resolved treatment.
    initial: list[tuple[bool, str, str, str, datetime]] = []  # fraud, state, device, cell, ts
    resolved_map: dict[tuple[str, str, int], str] = {}  # (cell, device, ts_ns) -> state

    def sweep(now: datetime) -> None:
        for r in verifier.resolve_escrows(now):
            resolved_map[(r.cell_id, r.device_id, ts.to_ns(r.reading_timestamp))] = r.state

    for ev in events:
        d = verifier.evaluate(ev.reading)
        initial.append((ev.fraudulent, d.state, ev.reading.device_id, ev.reading.cell_id, ev.reading.timestamp))
        if ev.sweep_after is not None:
            sweep(ev.sweep_after)

    # Final sweep at horizon.
    if events:
        sweep(max(e.reading.timestamp for e in events) + timedelta(seconds=3600))

    counts = {name: Counts() for name, _ in ESCROW_TREATMENTS}
    for fraudulent, state, device, cell, when in initial:
        res = resolved_map.get((cell, device, ts.to_ns(when)))
        for treatment, _ in ESCROW_TREATMENTS:
            accepted = _accepted_for_treatment(state, treatment, res, unresolved_policy)
            counts[treatment].add(fraudulent, accepted, state)
            if state == ts.PROVISIONAL_ESCROW and treatment == "resolved":
                if res == ts.ACCEPT:
                    counts[treatment].escrow_resolved_accept += 1
                elif res == ts.QUARANTINE:
                    counts[treatment].escrow_resolved_reject += 1
                else:
                    counts[treatment].escrow_unresolved += 1
    return counts


def run(output_dir: Path, events_per_seed: int = EVENTS_PER_SEED, seeds: Sequence[int] = SEEDS) -> None:
    output_dir.mkdir(parents=True, exist_ok=True)
    rows: list[dict[str, Any]] = []

    for sc in SCENARIOS:
        for treatment, _ in ESCROW_TREATMENTS:
            by_seed: list[Counts] = []
            for seed in seeds:
                events, neighbors = _generate_scenario(sc["key"], seed, events_per_seed)
                by_treatment = evaluate(events, neighbors)
                by_seed.append(by_treatment[treatment])
            metric_lists = {k: [] for k in ("precision", "recall", "f1", "fpr", "escrow_rate")}
            for c in by_seed:
                for k, v in c.metrics().items():
                    metric_lists[k].append(v)
            pooled = Counts()
            for c in by_seed:
                pooled.tn += c.tn; pooled.fp += c.fp; pooled.fn += c.fn; pooled.tp += c.tp
                pooled.escrow += c.escrow; pooled.total += c.total
                pooled.escrow_resolved_accept += c.escrow_resolved_accept
                pooled.escrow_resolved_reject += c.escrow_resolved_reject
                pooled.escrow_unresolved += c.escrow_unresolved
            row = {
                "scenario": sc["key"], "scenario_label": sc["label"],
                "escrow_treatment": treatment, "seeds": len(seeds),
                "events_per_seed_requested": events_per_seed,
                "pooled_total": pooled.total, "pooled_escrow": pooled.escrow,
                "pooled_tp": pooled.tp, "pooled_fp": pooled.fp, "pooled_fn": pooled.fn, "pooled_tn": pooled.tn,
                "pooled_escrow_resolved_accept": pooled.escrow_resolved_accept,
                "pooled_escrow_resolved_reject": pooled.escrow_resolved_reject,
                "pooled_escrow_unresolved": pooled.escrow_unresolved,
            }
            for k, vals in metric_lists.items():
                m, s = _mean_std(vals)
                row[f"{k}_mean"] = _fmt(m)
                row[f"{k}_std"] = _fmt(s)
            rows.append(row)

    csv_path = output_dir / "three_state_results.csv"
    with csv_path.open("w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        w.writerows(rows)

    # Confusion matrix summary (resolved treatment, primary).
    primary = [r for r in rows if r["escrow_treatment"] == "resolved"]
    cm_path = output_dir / "three_state_confusion.csv"
    with cm_path.open("w", newline="", encoding="utf-8") as f:
        fields = ["scenario", "scenario_label", "escrow_treatment", "tp", "fp", "fn", "tn", "escrow", "escrow_resolved_accept", "escrow_resolved_reject", "escrow_unresolved", "precision_mean", "recall_mean", "f1_mean", "fpr_mean"]
        w = csv.DictWriter(f, fieldnames=fields)
        w.writeheader()
        for r in rows:
            w.writerow({k: r.get(k if k in r else f"pooled_{k}" if f"pooled_{k}" in r else k, "") for k in fields} | {
                "tp": r["pooled_tp"], "fp": r["pooled_fp"], "fn": r["pooled_fn"], "tn": r["pooled_tn"],
                "escrow": r["pooled_escrow"],
                "escrow_resolved_accept": r["pooled_escrow_resolved_accept"],
                "escrow_resolved_reject": r["pooled_escrow_resolved_reject"],
                "escrow_unresolved": r["pooled_escrow_unresolved"],
            })

    md = _report(rows)
    (output_dir / "THREE_STATE_EVAL.md").write_text(md, encoding="utf-8", newline="\n")
    print(f"wrote {csv_path}")
    print(f"wrote {cm_path}")


def _report(rows: list[dict[str, Any]]) -> str:
    lines = [
        "# Three-State Policy Evaluation",
        "",
        "Generated by `sim/three_state_eval.py` (Phase 2, Task 2). Synthetic telemetry only.",
        "",
        "## Escrow treatment in confusion matrices",
        "",
        "A reading classified as `PROVISIONAL_ESCROW` is neither immediately accepted nor",
        "immediately rejected. Binary fraud metrics therefore require an explicit mapping.",
        "This evaluation reports **alternate treatments**:",
        "",
    ]
    for name, desc in ESCROW_TREATMENTS:
        lines.append(f"- **{name}**: {desc}")
    lines += [
        "",
        "The `resolved` treatment is the most faithful to the three-state policy when a",
        "resolution sweep is available: `ESCROW_CORROBORATED` becomes accepted;",
        "`ESCROW_EXPIRED_UNCORROBORATED` becomes rejected. Readings still unresolved at",
        "the evaluation horizon are counted as rejected by default (`--unresolved rejected`)",
        "and reported separately.",
        "",
        "## Scenarios",
        "",
        "| Key | Fraud prior | Description |",
        "|---|---:|---|",
    ]
    for sc in SCENARIOS:
        lines.append(f"| {sc['key']} | {sc['fraud_fraction']:.0%} | {sc['description']} |")
    lines += [
        "",
        "## Results (selected: resolved treatment)",
        "",
        "| Scenario | Escrow rate | Precision | Recall | F1 | FPR | TP | FP | FN | TN | Escrow accept | Escrow reject | Escrow unresolved |",
        "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|",
    ]
    for r in rows:
        if r["escrow_treatment"] != "resolved":
            continue
        lines.append(
            f"| {r['scenario']} | {r['escrow_rate_mean']}±{r['escrow_rate_std']} | "
            f"{r['precision_mean']}±{r['precision_std']} | {r['recall_mean']}±{r['recall_std']} | "
            f"{r['f1_mean']}±{r['f1_std']} | {r['fpr_mean']}±{r['fpr_std']} | "
            f"{r['pooled_tp']} | {r['pooled_fp']} | {r['pooled_fn']} | {r['pooled_tn']} | "
            f"{r['pooled_escrow_resolved_accept']} | {r['pooled_escrow_resolved_reject']} | {r['pooled_escrow_unresolved']} |"
        )
    lines += [
        "",
        "## Full treatment comparison",
        "",
        "See `three_state_results.csv` for every scenario × escrow-treatment combination.",
        "Differences between treatments show how much policy interpretation affects",
        "reported metrics; they do not indicate that one treatment is universally correct.",
        "",
        "## Known limitations",
        "",
        "- Synthetic scenarios only; not field validation.",
        "- Uses the Python mirror (`sim.three_state`), validated by the Go/Python consistency check.",
        "- Neighbor corroboration scenarios configure directed neighbor lists explicitly.",
        "- Fraud labels are scenario-defined; escrow-expiry scenarios label fabricated claims as fraud.",
        "",
    ]
    return "\n".join(lines) + "\n"


def main(argv: Sequence[str] | None = None) -> int:
    p = argparse.ArgumentParser(description="Three-state policy evaluation.")
    p.add_argument("--output-dir", default="docs/results/phase2/three_state")
    p.add_argument("--events", type=int, default=EVENTS_PER_SEED)
    p.add_argument("--seeds", default="1,2,3,4,5")
    args = p.parse_args(argv)
    seeds = tuple(int(s.strip()) for s in args.seeds.split(",") if s.strip())
    run(Path(args.output_dir), events_per_seed=args.events, seeds=seeds)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
