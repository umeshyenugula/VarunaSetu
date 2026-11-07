"""Adversarial simulation (Phase 2, Task 3).

Models:
- multi-cell collusion;
- compromised neighboring cells;
- attacker fraction from 10% to 80%;
- neighboring compromised cells from 0 to 6;
- varying peer density;
- radar lag scenarios.

Reports attack success rates and failure cases. Uses the Python verifier mirror
(`sim.three_state`) so escrow/neighbor/radar timing is exercised. Synthetic only.
"""

from __future__ import annotations

import argparse
import csv
import itertools
import json
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

BASE = datetime(2025, 8, 1, tzinfo=timezone.utc)
SEEDS = (1, 2, 3, 4, 5)
ATTACKER_FRACTIONS = (0.10, 0.20, 0.30, 0.40, 0.50, 0.60, 0.70, 0.80)
NEIGHBOR_COMPROMISED_COUNTS = (0, 1, 2, 3, 4, 5, 6)
PEER_DENSITIES = (6, 10, 16, 24, 32)  # devices per cell
RADAR_LAG_SCENARIOS = (
    {"key": "no_lag", "lag_s": 0, "corroborates": True, "description": "Wet radar available at attack time (no lag)."},
    {"key": "lag_within_ttl", "lag_s": 300, "corroborates": True, "description": "Radar corroborates 5 minutes after escrow, within 15-minute TTL."},
    {"key": "lag_after_ttl", "lag_s": 1200, "corroborates": False, "description": "Radar arrives 20 minutes after escrow, after TTL; cannot corroborate."},
    {"key": "no_radar", "lag_s": None, "corroborates": False, "description": "Radar remains dry/no update through TTL (persistent lag/unavailable)."},
)


@dataclass
class AttackCounts:
    attacks: int = 0
    accepted: int = 0  # attack succeeded in getting value accepted/corroborated
    quarantined: int = 0
    escrowed: int = 0
    escrow_corroborated: int = 0
    escrow_expired: int = 0
    escrow_unresolved: int = 0

    @property
    def success_rate(self) -> float:
        return self.accepted / self.attacks if self.attacks else 0.0

    @property
    def quarantine_rate(self) -> float:
        return self.quarantined / self.attacks if self.attacks else 0.0


def _fmt(v: float) -> str:
    return f"{v:.6f}"


def _mean_std(vals: Sequence[float]) -> tuple[float, float]:
    if not vals:
        return 0.0, 0.0
    return statistics.fmean(vals), (statistics.stdev(vals) if len(vals) > 1 else 0.0)


def _reading(device: str, cell: str, when: datetime, depth_mm: float, radar_mm: float) -> ts.Reading:
    return ts.Reading(
        device_id=device, timestamp=when,
        claimed_litres=depth_mm * 100.0, roof_area_m2=100.0, runoff_coeff=1.0,
        stored_litres=0.0, capacity_litres=100_000.0, radar_mm=radar_mm, cell_id=cell,
    )


_RADAR_LAG_INDEX = {x["key"]: i for i, x in enumerate(RADAR_LAG_SCENARIOS)}


def run_case(
    *,
    seed: int,
    attacker_fraction: float,
    neighbor_compromised: int,
    peer_density: int,
    radar_lag_key: str,
    trials: int = 120,
) -> AttackCounts:
    return _run_case_precise(seed=seed, attacker_fraction=attacker_fraction, neighbor_compromised=neighbor_compromised,
                             peer_density=peer_density, radar_lag_key=radar_lag_key, trials=trials)


def _run_case_precise(*, seed: int, attacker_fraction: float, neighbor_compromised: int,
                      peer_density: int, radar_lag_key: str, trials: int) -> AttackCounts:
    lag = next(x for x in RADAR_LAG_SCENARIOS if x["key"] == radar_lag_key)
    # Stable seed derivation (no Python hash()).
    case_seed = (
        seed * 1_000_003
        + int(round(attacker_fraction * 100)) * 10_007
        + neighbor_compromised * 101
        + peer_density * 7
        + _RADAR_LAG_INDEX[radar_lag_key] * 131
    )
    rng = random.Random(case_seed)
    counts = AttackCounts()
    target = "target"
    all_neighbors = [f"n{i}" for i in range(6)]

    for trial in range(trials):
        v = ts.ThreeStateVerifier()
        configured = all_neighbors[:6]  # up to 6 neighbors modeled
        v.set_neighbors(target, configured)
        n_attackers = max(0, min(peer_density, round(attacker_fraction * peer_density)))
        n_honest = peer_density - n_attackers
        when = BASE + timedelta(seconds=trial * 7200)
        attack_depth = 9.0
        dry_radar = 0.0
        # Honest readings under dry radar (low depth) establish context.
        for j in range(n_honest):
            v.evaluate(_reading(f"h{j}", target, when + timedelta(seconds=j), 0.2, dry_radar))

        attack_outcomes: list[tuple[str, str, str, datetime]] = []
        for j in range(n_attackers):
            dev = f"a{j}"
            ts_read = when + timedelta(seconds=100 + j)
            d = v.evaluate(_reading(dev, target, ts_read, attack_depth, dry_radar))
            attack_outcomes.append((d.state, dev, target, ts_read))

        # Populate neighbors: first `neighbor_compromised` of the 6 are compromised.
        for ni, nb in enumerate(configured):
            compromised = ni < neighbor_compromised
            for d in range(3):
                depth = attack_depth if compromised else 0.3
                v.evaluate(_reading(f"{nb}-d{d}", nb, when + timedelta(seconds=200), depth, 0.0))

        # Radar lag / corroboration.
        if lag["lag_s"] is not None and lag["lag_s"] > 0 and lag["key"] != "no_radar":
            probe_t = when + timedelta(seconds=100 + lag["lag_s"])
            v.evaluate(_reading("probe", target, probe_t, 0.2, 1.0 if lag["corroborates"] else 0.0))
        elif lag["key"] == "no_lag" and lag["corroborates"]:
            v.evaluate(_reading("probe", target, when + timedelta(seconds=101), 0.2, 1.0))

        resolutions = v.resolve_escrows(when + timedelta(seconds=7200))
        res_map = {(r.cell_id, r.device_id, ts.to_ns(r.reading_timestamp)): r for r in resolutions}

        for state, dev, cell, ts_read in attack_outcomes:
            counts.attacks += 1
            if state == ts.ACCEPT:
                counts.accepted += 1
            elif state == ts.QUARANTINE:
                counts.quarantined += 1
            elif state == ts.PROVISIONAL_ESCROW:
                counts.escrowed += 1
                r = res_map.get((cell, dev, ts.to_ns(ts_read)))
                if r is None:
                    counts.escrow_unresolved += 1
                elif r.state == ts.ACCEPT:
                    counts.accepted += 1
                    counts.escrow_corroborated += 1
                else:
                    counts.quarantined += 1
                    counts.escrow_expired += 1
            else:
                counts.quarantined += 1
    return counts


def run(output_dir: Path, seeds: Sequence[int] = SEEDS, trials: int = 120) -> None:
    output_dir.mkdir(parents=True, exist_ok=True)
    rows: list[dict[str, Any]] = []
    failures: list[dict[str, Any]] = []

    for (af, nc, pd, lag) in itertools.product(ATTACKER_FRACTIONS, NEIGHBOR_COMPROMISED_COUNTS, PEER_DENSITIES, RADAR_LAG_SCENARIOS):
        by_seed = [_run_case_precise(seed=s, attacker_fraction=af, neighbor_compromised=nc, peer_density=pd,
                                     radar_lag_key=lag["key"], trials=trials) for s in seeds]
        sr = [c.success_rate for c in by_seed]
        qr = [c.quarantine_rate for c in by_seed]
        m_sr, s_sr = _mean_std(sr)
        m_qr, s_qr = _mean_std(qr)
        attacks = sum(c.attacks for c in by_seed)
        accepted = sum(c.accepted for c in by_seed)
        quarantined = sum(c.quarantined for c in by_seed)
        escrowed = sum(c.escrowed for c in by_seed)
        corr = sum(c.escrow_corroborated for c in by_seed)
        expired = sum(c.escrow_expired for c in by_seed)
        unres = sum(c.escrow_unresolved for c in by_seed)
        row = {
            "attacker_fraction": af, "neighbor_compromised_cells": nc, "peer_density": pd,
            "radar_lag": lag["key"], "radar_lag_description": lag["description"],
            "seeds": len(seeds), "trials_per_seed": trials,
            "attacks": attacks, "accepted_attacks": accepted, "quarantined_attacks": quarantined,
            "escrowed_attacks": escrowed, "escrow_corroborated": corr, "escrow_expired": expired,
            "escrow_unresolved": unres,
            "attack_success_rate_mean": _fmt(m_sr), "attack_success_rate_std": _fmt(s_sr),
            "quarantine_rate_mean": _fmt(m_qr), "quarantine_rate_std": _fmt(s_qr),
        }
        rows.append(row)
        # Failure case reporting: high success under collusion/majority/lag conditions.
        if m_sr >= 0.5 and attacks > 0:
            failures.append({
                "condition": f"af={af}, neighbor_compromised={nc}, density={pd}, lag={lag['key']}",
                "attack_success_rate_mean": _fmt(m_sr),
                "mechanism": _mechanism(af, nc, lag["key"]),
            })

    csv_path = output_dir / "adversarial_results.csv"
    with csv_path.open("w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        w.writerows(rows)

    # Summary slices.
    summary_rows = _summaries(rows)
    sum_path = output_dir / "adversarial_summary.csv"
    with sum_path.open("w", newline="", encoding="utf-8") as f:
        w = csv.DictWriter(f, fieldnames=list(summary_rows[0].keys()))
        w.writeheader()
        w.writerows(summary_rows)

    (output_dir / "failure_cases.json").write_text(
        json.dumps({"count": len(failures), "threshold": "attack_success_rate_mean >= 0.5", "cases": failures}, indent=2) + "\n",
        encoding="utf-8", newline="\n")
    (output_dir / "ADVERSARIAL.md").write_text(_report(rows, failures), encoding="utf-8", newline="\n")
    print(f"wrote {csv_path} ({len(rows)} rows)")


def _mechanism(af: float, nc: int, lag: str) -> str:
    parts = []
    if af >= 0.6:
        parts.append("attacker majority can dominate/cohere peer statistics in the target cell")
    if nc >= 2:
        parts.append(">=2 compromised neighboring cells can satisfy neighbor corroboration")
    if lag in ("lag_after_ttl", "no_radar"):
        parts.append("radar lag/unavailability prevents timely radar corroboration")
    if lag == "no_lag" and af < 0.5 and nc < 2:
        parts.append("wet-radar/no-lag case should usually limit escrow abuse; inspect quarantines")
    return "; ".join(parts) or "inspect row details"


def _summaries(rows: list[dict[str, Any]]) -> list[dict[str, Any]]:
    out = []
    # By attacker fraction (averaged over other factors).
    for af in ATTACKER_FRACTIONS:
        subset = [r for r in rows if r["attacker_fraction"] == af]
        out.append(_agg("by_attacker_fraction", f"attacker_fraction={af}", subset))
    for nc in NEIGHBOR_COMPROMISED_COUNTS:
        subset = [r for r in rows if r["neighbor_compromised_cells"] == nc]
        out.append(_agg("by_neighbor_compromised_cells", f"neighbor_compromised_cells={nc}", subset))
    for pd in PEER_DENSITIES:
        subset = [r for r in rows if r["peer_density"] == pd]
        out.append(_agg("by_peer_density", f"peer_density={pd}", subset))
    for lag in RADAR_LAG_SCENARIOS:
        subset = [r for r in rows if r["radar_lag"] == lag["key"]]
        out.append(_agg("by_radar_lag", f"radar_lag={lag['key']}", subset))
    return out


def _agg(kind: str, label: str, subset: list[dict[str, Any]]) -> dict[str, Any]:
    def f(k: str) -> float:
        return statistics.fmean(float(r[k]) for r in subset) if subset else 0.0
    return {
        "slice": kind, "label": label, "rows": len(subset),
        "attack_success_rate_mean": _fmt(f("attack_success_rate_mean")),
        "quarantine_rate_mean": _fmt(f("quarantine_rate_mean")),
        "attacks": sum(int(r["attacks"]) for r in subset),
        "accepted_attacks": sum(int(r["accepted_attacks"]) for r in subset),
        "escrow_corroborated": sum(int(r["escrow_corroborated"]) for r in subset),
    }


def _report(rows: list[dict[str, Any]], failures: list[dict[str, Any]]) -> str:
    lines = [
        "# Adversarial Simulation",
        "",
        "Generated by `sim/adversarial.py` (Phase 2, Task 3). Synthetic telemetry only.",
        "Uses the Python verifier mirror (`sim.three_state`).",
        "",
        "## Factors",
        "",
        f"- Attacker fraction: {list(ATTACKER_FRACTIONS)} (target cell, as fraction of peer density).",
        f"- Neighboring compromised cells: {list(NEIGHBOR_COMPROMISED_COUNTS)} out of 6 configured neighbors.",
        f"- Peer density (devices/cell): {list(PEER_DENSITIES)}.",
        "- Radar lag scenarios:",
    ]
    for lag in RADAR_LAG_SCENARIOS:
        lines.append(f"  - `{lag['key']}`: {lag['description']}")
    lines += [
        "",
        "## Attack model",
        "",
        "Attackers in the target cell submit coherent fabricated readings (9 mm) under dry radar.",
        "Honest devices submit low readings (0.2 mm) under dry radar unless noted.",
        "Compromised neighboring cells (count = factor) populate >=3 devices with fabricated rainy",
        "medians, enabling neighbor corroboration when >=2 such cells are configured and recent.",
        "A single compromised neighbor cannot satisfy the default `NeighborMinCells=2` requirement.",
        "Success means the attacker's fabricated reading is ultimately accepted (immediate ACCEPT",
        "or escrow resolved to ESCROW_CORROBORATED). Quarantine/expiry/failure means the verifier",
        "did not accept the fabricated value.",
        "",
        "## Failure cases (attack success rate mean >= 0.5)",
        "",
        f"Total failure-case conditions reported: {len(failures)}. See `failure_cases.json`.",
        "",
    ]
    for f in failures[:40]:
        lines.append(f"- {f['condition']}: success={f['attack_success_rate_mean']} — {f['mechanism']}")
    if len(failures) > 40:
        lines.append(f"- ... ({len(failures)-40} more in failure_cases.json)")
    lines += [
        "",
        "## Known limitations",
        "",
        "- Synthetic, parameterized attacker model; not a proof of security.",
        "- Neighbor corroboration is evaluated with explicitly configured directed neighbor lists.",
        "- Radar lag is modeled as timing/availability of a later corroborating reading, not RF/channel physics.",
        "- Multi-cell collusion remains a known structural risk (see HANDOFF/CONTRACTS): H3 indexing does not prevent collusion.",
        "- Results depend on peer density, honest baseline, and the specific fabricated depth chosen here.",
        "",
        "## Outputs",
        "",
        "- `adversarial_results.csv`: full factor grid with attack success/quarantine/escrow counts.",
        "- `adversarial_summary.csv`: aggregated slices by each factor.",
        "- `failure_cases.json`: conditions with mean success >= 0.5.",
        "",
    ]
    return "\n".join(lines) + "\n"


def main(argv: Sequence[str] | None = None) -> int:
    p = argparse.ArgumentParser(description="Adversarial simulation (Phase 2).")
    p.add_argument("--output-dir", default="docs/results/phase2/adversarial")
    p.add_argument("--seeds", default="1,2,3,4,5")
    p.add_argument("--trials", type=int, default=120, help="trials per seed per factor combination")
    args = p.parse_args(argv)
    seeds = tuple(int(s.strip()) for s in args.seeds.split(",") if s.strip())
    run(Path(args.output_dir), seeds=seeds, trials=args.trials)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
