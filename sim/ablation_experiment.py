"""Reproducible synthetic ablation for the HydroLedger verifier.

The script generates seven scenarios under ten fixed seeds, evaluates four
verification methods, and writes every table required by Phase 0B:

    docs/results/METHOD.md
    docs/results/overall_results.csv
    docs/results/scenario_results.csv
    docs/results/confusion_matrices.csv

It uses the Python standard library only and never accesses the network.

Run from the repository root:

    python sim/ablation_experiment.py

Or regenerate everything (tests first) with:

    bash sim/run_all.sh

Ground truth and metric definitions are documented in ``sim/config.py`` and in
the generated ``METHOD.md``.
"""

from __future__ import annotations

import argparse
import csv
import math
import os
import platform
import random
import statistics
import sys
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Callable, Iterable, NamedTuple, Sequence

try:  # imported as ``sim.ablation_experiment``
    from . import config
except ImportError:  # executed as ``python sim/ablation_experiment.py``
    import config  # type: ignore[no-redef]


# ---------------------------------------------------------------------------
# Data structures
# ---------------------------------------------------------------------------


class CellProfile(NamedTuple):
    """Rainfall and radar properties of one synthetic cell."""

    storms: tuple[tuple[float, float, float], ...]  # (centre_s, width_s, peak_mm)
    baseline_mm: float
    radar_gain: float
    radar_bias_mm: float


class Event(NamedTuple):
    """One synthetic telemetry reading.

    ``claimed_litres`` is the stored payload. Effective depth is recomputed in
    the evaluator as ``claimed_litres / (area_m2 * runoff)`` so the experiment
    exercises the same conversion as the Go verifier.
    """

    t: float
    cell: int
    device: int
    true_mm: float
    radar_mm: float
    claimed_litres: float
    area_m2: float
    runoff: float
    stored_litres: float
    fraudulent: bool


@dataclass
class Confusion:
    tn: int = 0
    fp: int = 0
    fn: int = 0
    tp: int = 0

    def add(self, fraudulent: bool, rejected: bool) -> None:
        if fraudulent and rejected:
            self.tp += 1
        elif fraudulent:
            self.fn += 1
        elif rejected:
            self.fp += 1
        else:
            self.tn += 1

    def merge(self, other: "Confusion") -> None:
        self.tn += other.tn
        self.fp += other.fp
        self.fn += other.fn
        self.tp += other.tp

    @property
    def events(self) -> int:
        return self.tn + self.fp + self.fn + self.tp

    @property
    def positives(self) -> int:
        return self.tp + self.fn

    @property
    def negatives(self) -> int:
        return self.tn + self.fp


@dataclass(frozen=True)
class Metrics:
    precision: float
    recall: float
    f1: float
    false_positive_rate: float


class Decisions(NamedTuple):
    """Binary reject/accept outcomes of the four methods."""

    static: bool
    physics: bool
    peer: bool
    hybrid: bool


@dataclass(frozen=True)
class RunRecord:
    scenario: str
    seed: int
    method: str
    tn: int
    fp: int
    fn: int
    tp: int

    def confusion(self) -> Confusion:
        return Confusion(tn=self.tn, fp=self.fp, fn=self.fn, tp=self.tp)


# ---------------------------------------------------------------------------
# Deterministic generation
# ---------------------------------------------------------------------------


def _scenario_index(scenario: config.Scenario) -> int:
    return config.SCENARIOS.index(scenario)


def _rng(seed: int, scenario_index: int) -> random.Random:
    """A stable integer seed; never uses Python's randomized ``hash``."""

    return random.Random(seed * config.SEED_STRIDE + scenario_index)


def _rainfall_at(profile: CellProfile, t: float) -> float:
    total = profile.baseline_mm
    for centre, width, peak in profile.storms:
        total += peak * math.exp(-0.5 * ((t - centre) / width) ** 2)
    return total


def _build_world(
    rng: random.Random, scenario: config.Scenario
) -> tuple[list[CellProfile], list[list[float]], list[list[float]]]:
    burst_cell = rng.randrange(config.CELLS) if scenario.mode == "microburst" else -1

    profiles: list[CellProfile] = []
    for cell in range(config.CELLS):
        storms: list[tuple[float, float, float]] = []

        if scenario.mode in ("normal", "microburst", "isolated_spike"):
            for _ in range(rng.randint(*config.STORMS_PER_CELL)):
                storms.append(
                    (
                        rng.uniform(*config.STORM_CENTRE_FRACTION_RANGE)
                        * config.DURATION_S,
                        rng.uniform(*config.STORM_WIDTH_S),
                        rng.uniform(*config.STORM_PEAK_MM),
                    )
                )

        if scenario.mode == "microburst" and cell == burst_cell:
            storms.append(
                (
                    rng.uniform(*config.MICROBURST_CENTRE_FRACTION_RANGE)
                    * config.DURATION_S,
                    rng.uniform(*config.MICROBURST_WIDTH_S),
                    rng.uniform(*config.MICROBURST_PEAK_MM),
                )
            )
            gain = rng.uniform(*config.MICROBURST_RADAR_CATCH_RANGE)
        else:
            gain = rng.uniform(*config.RADAR_GAIN_RANGE)

        if scenario.mode == "coordinated":
            baseline = rng.uniform(*config.COORDINATED_TRUE_MM)
        elif scenario.mode == "radar_bounded":
            baseline = rng.uniform(*config.RADAR_BOUNDED_TRUE_MM)
        elif scenario.mode == "dry_spoof":
            baseline = 0.0
        else:
            baseline = rng.uniform(*config.BASELINE_MM)

        radar_bias = (
            rng.uniform(*config.RADAR_BOUNDED_RADAR_MM)
            if scenario.radar_mode == "bounded"
            else 0.0
        )

        profiles.append(
            CellProfile(
                storms=tuple(storms),
                baseline_mm=baseline,
                radar_gain=gain,
                radar_bias_mm=radar_bias,
            )
        )

    areas = [
        [rng.uniform(*config.ROOF_AREA_M2_RANGE) for _ in range(config.DEVICES_PER_CELL)]
        for _ in range(config.CELLS)
    ]
    coeffs = [
        [rng.uniform(*config.RUNOFF_COEFF_RANGE) for _ in range(config.DEVICES_PER_CELL)]
        for _ in range(config.CELLS)
    ]
    return profiles, areas, coeffs


def _radar_mm(
    rng: random.Random,
    profile: CellProfile,
    true_mm: float,
    radar_mode: str,
) -> float:
    if radar_mode == "dry":
        return rng.uniform(*config.DRY_RADAR_MM)
    if radar_mode == "bounded":
        return max(
            0.0,
            profile.radar_bias_mm
            + rng.gauss(0.0, config.RADAR_BOUNDED_RADAR_NOISE_MM),
        )
    spot_noise = config.RADAR_ABS_NOISE_MM + config.RADAR_REL_NOISE * max(true_mm, 0.0)
    return max(0.0, true_mm * profile.radar_gain + rng.gauss(0.0, spot_noise))


def _claim_mm(
    rng: random.Random,
    scenario: config.Scenario,
    true_mm: float,
    radar_mm: float,
    fraudulent: bool,
) -> float:
    if not fraudulent:
        return max(0.0, true_mm + rng.gauss(0.0, config.SENSOR_NOISE_MM))

    if scenario.mode in ("normal", "microburst"):
        return max(0.0, true_mm + rng.uniform(*config.NORMAL_OVERCLAIM_MM))
    if scenario.mode == "isolated_spike":
        return max(0.0, true_mm + rng.uniform(*config.SPIKE_OVERCLAIM_MM))
    if scenario.mode == "dry_spoof":
        return rng.uniform(*config.DRY_SPOOF_CLAIM_MM)
    if scenario.mode == "radar_bounded":
        r_max = config.ALPHA * radar_mm + config.BETA_MM
        fraction = rng.uniform(*config.RADAR_BOUNDED_CLAIM_FRACTION)
        claim = fraction * r_max + rng.gauss(
            0.0, config.RADAR_BOUNDED_CLAIM_NOISE_MM
        )
        return min(
            max(true_mm + config.RADAR_BOUNDED_MIN_OVERCLAIM_MM, claim),
            config.RADAR_BOUNDED_MAX_FRACTION * r_max,
        )
    if scenario.mode == "coordinated":
        return max(
            true_mm + config.COORDINATED_MIN_OVERCLAIM_MM,
            rng.gauss(
                config.COORDINATED_CLAIM_MEAN_MM,
                config.COORDINATED_CLAIM_SD_MM,
            ),
        )
    raise ValueError(f"unknown scenario mode: {scenario.mode}")


def generate_events(
    scenario: config.Scenario,
    seed: int,
    events_per_seed: int = config.EVENTS_PER_SEED,
) -> list[Event]:
    """Generate one chronologically ordered synthetic event stream.

    The function is deterministic for a given ``(scenario, seed,
    events_per_seed)``. Event generation is sorted by timestamp before it is
    returned so the peer window sees observations in event-time order.
    """

    if events_per_seed <= 0:
        raise ValueError("events_per_seed must be positive")

    rng = _rng(seed, _scenario_index(scenario))
    profiles, areas, coeffs = _build_world(rng, scenario)

    compromised = [[False] * config.DEVICES_PER_CELL for _ in range(config.CELLS)]
    if scenario.mode == "coordinated":
        n_compromised = round(scenario.compromised_fraction * config.DEVICES_PER_CELL)
        for cell in range(config.CELLS):
            for device in rng.sample(range(config.DEVICES_PER_CELL), n_compromised):
                compromised[cell][device] = True

    if scenario.mode in (
        "normal",
        "microburst",
        "isolated_spike",
        "dry_spoof",
        "radar_bounded",
    ):
        n_fraud = round(scenario.fraud_fraction * events_per_seed)
        fraud_indices = set(rng.sample(range(events_per_seed), n_fraud))
    else:
        fraud_indices = set()

    events: list[Event] = []
    for index in range(events_per_seed):
        cell = rng.randrange(config.CELLS)
        device = rng.randrange(config.DEVICES_PER_CELL)
        t = rng.random() * config.DURATION_S

        true_mm = _rainfall_at(profiles[cell], t)
        radar_mm = _radar_mm(rng, profiles[cell], true_mm, scenario.radar_mode)

        if scenario.mode == "coordinated":
            fraudulent = compromised[cell][device]
        else:
            fraudulent = index in fraud_indices

        claim_mm = _claim_mm(rng, scenario, true_mm, radar_mm, fraudulent)
        area_m2 = areas[cell][device]
        runoff = coeffs[cell][device]
        stored = rng.uniform(*config.STORED_LITRES_RANGE)

        claimed_litres = claim_mm * area_m2 * runoff
        free_space = config.TANK_CAPACITY_L - stored
        if claimed_litres > free_space:  # not expected; keeps every event valid
            claimed_litres = free_space

        events.append(
            Event(
                t=t,
                cell=cell,
                device=device,
                true_mm=true_mm,
                radar_mm=radar_mm,
                claimed_litres=claimed_litres,
                area_m2=area_m2,
                runoff=runoff,
                stored_litres=stored,
                fraudulent=fraudulent,
            )
        )

    events.sort(key=lambda e: (e.t, e.cell, e.device))
    return events


# ---------------------------------------------------------------------------
# Verification methods
# ---------------------------------------------------------------------------


def _median_sorted(values: Sequence[float]) -> float:
    n = len(values)
    if n % 2 == 1:
        return values[n // 2]
    return values[n // 2 - 1] / 2.0 + values[n // 2] / 2.0


def _peer_stats(peers: list[float]) -> tuple[float, float, float]:
    """Median, MAD and spread of an already non-empty peer list."""

    peers.sort()
    median = _median_sorted(peers)
    deviations = sorted(abs(x - median) for x in peers)
    mad = _median_sorted(deviations)
    return median, mad, peers[-1] - peers[0]


def classify(r_obs: float, radar_mm: float, peers: Sequence[float]) -> Decisions:
    """Apply the four binary methods to one reading and its peer depths.

    Peer and hybrid use the same peer statistics. A reading with fewer than
    ``config.MIN_PEERS`` peers is accepted by peer-only and falls back to the
    radar bound in hybrid, matching the Go verifier's insufficient-peer path.

    Binary mapping: the Go verifier's ``PROVISIONAL_ESCROW`` is treated as
    not accepted (rejected) for hybrid. Phase 0B does not model escrow TTL
    resolution; this is a documented, conservative choice.
    """

    static_reject = r_obs > config.STATIC_THRESHOLD_MM
    r_max = config.ALPHA * radar_mm + config.BETA_MM
    physics_reject = r_obs > r_max

    peer_reject = False
    hybrid_reject = physics_reject

    if len(peers) >= config.MIN_PEERS:
        median, mad, spread = _peer_stats(list(peers))
        z_score = config.Z_SCALE * abs(r_obs - median) / (
            mad + config.MAD_FLOOR_MM
        )
        outlier = z_score > config.OUTLIER_Z and r_obs > median
        peer_reject = outlier

        radar_dry = radar_mm <= config.RADAR_DRY_MM
        peers_rain = median >= config.PEER_RAIN_MM
        coherent = spread <= max(
            config.COHERENCE_ABS_MM,
            config.COHERENCE_REL_FRAC * median,
        )

        if outlier:
            hybrid_reject = True
        elif radar_dry and peers_rain and coherent:
            # The Go verifier would hold this in provisional escrow.
            hybrid_reject = True
        else:
            hybrid_reject = r_obs > r_max

    return Decisions(
        static=static_reject,
        physics=physics_reject,
        peer=peer_reject,
        hybrid=hybrid_reject,
    )


def evaluate_events(events: Iterable[Event]) -> dict[str, Confusion]:
    """Apply all four methods to a chronologically ordered event stream.

    Peer and hybrid share one peer window because both use the same
    per-cell, one-latest-observation-per-device policy as the Go verifier.
    A reading is recorded in the window whatever verdict it receives.
    """

    windows: list[dict[int, tuple[float, float]]] = [
        {} for _ in range(config.CELLS)
    ]
    results = {method: Confusion() for method in config.METHOD_ORDER}

    for event in events:
        r_obs = event.claimed_litres / (event.area_m2 * event.runoff)

        window = windows[event.cell]
        peers = [
            depth
            for device, (ts, depth) in window.items()
            if device != event.device and event.t - ts <= config.PEER_MAX_AGE_S
        ]
        # Record after collecting peers: a device is never its own peer.
        window[event.device] = (event.t, r_obs)

        decision = classify(r_obs, event.radar_mm, peers)
        results["static"].add(event.fraudulent, decision.static)
        results["physics"].add(event.fraudulent, decision.physics)
        results["peer"].add(event.fraudulent, decision.peer)
        results["hybrid"].add(event.fraudulent, decision.hybrid)

    return results


# ---------------------------------------------------------------------------
# Metrics
# ---------------------------------------------------------------------------


def metrics_from_confusion(confusion: Confusion) -> Metrics:
    """Precision, recall, F1 and FPR. A zero denominator yields 0.0."""

    tp, fp, fn, tn = confusion.tp, confusion.fp, confusion.fn, confusion.tn
    precision = tp / (tp + fp) if tp + fp else 0.0
    recall = tp / (tp + fn) if tp + fn else 0.0
    f1 = (
        2.0 * precision * recall / (precision + recall)
        if precision + recall
        else 0.0
    )
    fpr = fp / (fp + tn) if fp + tn else 0.0
    return Metrics(precision, recall, f1, fpr)


def mean_std(values: Sequence[float]) -> tuple[float, float]:
    """Mean and sample standard deviation (n-1); std is 0.0 for n < 2."""

    if not values:
        return 0.0, 0.0
    mean = statistics.fmean(values)
    if len(values) < 2:
        return mean, 0.0
    return mean, statistics.stdev(values)


# ---------------------------------------------------------------------------
# Experiment driver
# ---------------------------------------------------------------------------


def run_experiment(
    events_per_seed: int = config.EVENTS_PER_SEED,
    seeds: Sequence[int] = config.SEEDS,
    scenario_keys: Sequence[str] | None = None,
    progress: Callable[[str, int, dict[str, Confusion]], None] | None = None,
) -> list[RunRecord]:
    """Run every requested scenario and seed; return one record per method."""

    if not seeds:
        raise ValueError("at least one seed is required")
    if events_per_seed <= 0:
        raise ValueError("events_per_seed must be positive")

    available = {scenario.key: scenario for scenario in config.SCENARIOS}
    if scenario_keys is None:
        scenarios = list(config.SCENARIOS)
    else:
        unknown = [key for key in scenario_keys if key not in available]
        if unknown:
            raise ValueError(f"unknown scenario key(s): {', '.join(unknown)}")
        scenarios = [available[key] for key in scenario_keys]

    records: list[RunRecord] = []
    for scenario in scenarios:
        for seed in seeds:
            events = generate_events(scenario, seed, events_per_seed)
            by_method = evaluate_events(events)
            if progress is not None:
                progress(scenario.key, seed, by_method)
            for method in config.METHOD_ORDER:
                confusion = by_method[method]
                records.append(
                    RunRecord(
                        scenario=scenario.key,
                        seed=seed,
                        method=method,
                        tn=confusion.tn,
                        fp=confusion.fp,
                        fn=confusion.fn,
                        tp=confusion.tp,
                    )
                )
    return records


# ---------------------------------------------------------------------------
# Aggregation helpers
# ---------------------------------------------------------------------------


def _scenario_rows(records: Sequence[RunRecord]) -> list[list[str]]:
    grouped: dict[tuple[str, str], list[RunRecord]] = {}
    for record in records:
        grouped.setdefault((record.method, record.scenario), []).append(record)

    rows: list[list[str]] = []
    scenario_order = {scenario.key: i for i, scenario in enumerate(config.SCENARIOS)}
    for method in config.METHOD_ORDER:
        for key in sorted(scenario_order, key=scenario_order.get):
            items = grouped.get((method, key))
            if not items:
                continue
            metrics = [metrics_from_confusion(item.confusion()) for item in items]
            p_mean, p_std = mean_std([m.precision for m in metrics])
            r_mean, r_std = mean_std([m.recall for m in metrics])
            f_mean, f_std = mean_std([m.f1 for m in metrics])
            fpr_mean, fpr_std = mean_std([m.false_positive_rate for m in metrics])
            total = Confusion()
            for item in items:
                total.merge(item.confusion())
            rows.append(
                [
                    method,
                    key,
                    str(total.events),
                    str(total.positives),
                    str(total.negatives),
                    _fmt(p_mean),
                    _fmt(p_std),
                    _fmt(r_mean),
                    _fmt(r_std),
                    _fmt(f_mean),
                    _fmt(f_std),
                    _fmt(fpr_mean),
                    _fmt(fpr_std),
                    str(total.tp),
                    str(total.fp),
                    str(total.fn),
                    str(total.tn),
                ]
            )
    return rows


def _overall_rows(records: Sequence[RunRecord]) -> list[list[str]]:
    by_seed_method: dict[int, dict[str, Confusion]] = {}
    for record in records:
        by_seed_method.setdefault(record.seed, {}).setdefault(
            record.method, Confusion()
        ).merge(record.confusion())

    rows: list[list[str]] = []
    for method in config.METHOD_ORDER:
        metrics: list[Metrics] = []
        total = Confusion()
        for seed in sorted(by_seed_method):
            confusion = by_seed_method[seed].get(method)
            if confusion is None:
                continue
            metrics.append(metrics_from_confusion(confusion))
            total.merge(confusion)
        p_mean, p_std = mean_std([m.precision for m in metrics])
        r_mean, r_std = mean_std([m.recall for m in metrics])
        f_mean, f_std = mean_std([m.f1 for m in metrics])
        fpr_mean, fpr_std = mean_std([m.false_positive_rate for m in metrics])
        rows.append(
            [
                method,
                str(len(metrics)),
                str(total.events),
                str(total.positives),
                str(total.negatives),
                _fmt(p_mean),
                _fmt(p_std),
                _fmt(r_mean),
                _fmt(r_std),
                _fmt(f_mean),
                _fmt(f_std),
                _fmt(fpr_mean),
                _fmt(fpr_std),
                str(total.tp),
                str(total.fp),
                str(total.fn),
                str(total.tn),
            ]
        )
    return rows


def _confusion_rows(records: Sequence[RunRecord]) -> list[list[str]]:
    by_scenario_seed: dict[tuple[str, int], dict[str, Confusion]] = {}
    by_scenario: dict[str, dict[str, Confusion]] = {}
    by_seed: dict[int, dict[str, Confusion]] = {}
    overall: dict[str, Confusion] = {method: Confusion() for method in config.METHOD_ORDER}

    for record in records:
        confusion = record.confusion()
        by_scenario_seed.setdefault((record.scenario, record.seed), {}).setdefault(
            record.method, Confusion()
        ).merge(confusion)
        by_scenario.setdefault(record.scenario, {}).setdefault(
            record.method, Confusion()
        ).merge(confusion)
        by_seed.setdefault(record.seed, {}).setdefault(
            record.method, Confusion()
        ).merge(confusion)
        overall[record.method].merge(confusion)

    rows: list[list[str]] = []
    scenario_order = {scenario.key: i for i, scenario in enumerate(config.SCENARIOS)}

    for scenario_key in sorted(scenario_order, key=scenario_order.get):
        scenario_data = by_scenario.get(scenario_key)
        if not scenario_data:
            continue
        for seed in sorted(by_seed):
            per_method = by_scenario_seed.get((scenario_key, seed))
            if not per_method:
                continue
            for method in config.METHOD_ORDER:
                c = per_method[method]
                rows.append(
                    [
                        "scenario",
                        scenario_key,
                        str(seed),
                        method,
                        str(c.tn),
                        str(c.fp),
                        str(c.fn),
                        str(c.tp),
                    ]
                )
        for method in config.METHOD_ORDER:
            c = scenario_data[method]
            rows.append(
                [
                    "scenario",
                    scenario_key,
                    "pooled",
                    method,
                    str(c.tn),
                    str(c.fp),
                    str(c.fn),
                    str(c.tp),
                ]
            )

    for seed in sorted(by_seed):
        for method in config.METHOD_ORDER:
            c = by_seed[seed][method]
            rows.append(
                [
                    "overall",
                    "ALL",
                    str(seed),
                    method,
                    str(c.tn),
                    str(c.fp),
                    str(c.fn),
                    str(c.tp),
                ]
            )
    for method in config.METHOD_ORDER:
        c = overall[method]
        rows.append(
            [
                "overall",
                "ALL",
                "pooled",
                method,
                str(c.tn),
                str(c.fp),
                str(c.fn),
                str(c.tp),
            ]
        )
    return rows


def _fmt(value: float) -> str:
    return f"{value:.{config.FLOAT_DECIMALS}f}"


# ---------------------------------------------------------------------------
# Output
# ---------------------------------------------------------------------------


def _write_csv(path: str, header: Sequence[str], rows: Sequence[Sequence[str]]) -> None:
    with open(path, "w", newline="", encoding="utf-8") as handle:
        writer = csv.writer(handle)
        writer.writerow(header)
        writer.writerows(rows)


def write_results(
    records: Sequence[RunRecord],
    output_dir: str,
    events_per_seed: int,
    seeds: Sequence[int],
    command: str,
) -> None:
    """Write all CSV tables and the generated METHOD.md."""

    os.makedirs(output_dir, exist_ok=True)

    _write_csv(
        os.path.join(output_dir, "overall_results.csv"),
        [
            "method",
            "seeds",
            "events",
            "positives",
            "negatives",
            "precision_mean",
            "precision_std",
            "recall_mean",
            "recall_std",
            "f1_mean",
            "f1_std",
            "false_positive_rate_mean",
            "false_positive_rate_std",
            "tp",
            "fp",
            "fn",
            "tn",
        ],
        _overall_rows(records),
    )

    _write_csv(
        os.path.join(output_dir, "scenario_results.csv"),
        [
            "method",
            "scenario",
            "events",
            "positives",
            "negatives",
            "precision_mean",
            "precision_std",
            "recall_mean",
            "recall_std",
            "f1_mean",
            "f1_std",
            "false_positive_rate_mean",
            "false_positive_rate_std",
            "tp",
            "fp",
            "fn",
            "tn",
        ],
        _scenario_rows(records),
    )

    _write_csv(
        os.path.join(output_dir, "confusion_matrices.csv"),
        ["scope", "scenario", "seed", "method", "tn", "fp", "fn", "tp"],
        _confusion_rows(records),
    )

    _write_method_md(
        os.path.join(output_dir, "METHOD.md"),
        records,
        events_per_seed,
        seeds,
        command,
    )


def _markdown_table(header: Sequence[str], rows: Sequence[Sequence[str]]) -> str:
    lines = ["| " + " | ".join(header) + " |"]
    lines.append("|" + "|".join("---" for _ in header) + "|")
    for row in rows:
        lines.append("| " + " | ".join(row) + " |")
    return "\n".join(lines)


def _write_method_md(
    path: str,
    records: Sequence[RunRecord],
    events_per_seed: int,
    seeds: Sequence[int],
    command: str,
) -> None:
    overall = _overall_rows(records)
    scenario = _scenario_rows(records)

    overall_table = _markdown_table(
        ["Method", "Precision", "Recall", "F1", "FPR", "TP", "FP", "FN", "TN"],
        [
            [
                row[0],
                f"{row[5]} ± {row[6]}",
                f"{row[7]} ± {row[8]}",
                f"{row[9]} ± {row[10]}",
                f"{row[11]} ± {row[12]}",
                row[13],
                row[14],
                row[15],
                row[16],
            ]
            for row in overall
        ],
    )

    scenario_table = _markdown_table(
        ["Scenario", "Method", "Precision", "Recall", "F1", "FPR"],
        [
            [
                row[1],
                row[0],
                f"{row[5]} ± {row[6]}",
                f"{row[7]} ± {row[8]}",
                f"{row[9]} ± {row[10]}",
                f"{row[11]} ± {row[12]}",
            ]
            for row in scenario
        ],
    )

    scenario_rows = []
    for scenario_cfg in config.SCENARIOS:
        prior = (
            f"{scenario_cfg.compromised_fraction:.0%} compromised devices"
            if scenario_cfg.mode == "coordinated"
            else f"{scenario_cfg.fraud_fraction:.0%} fraudulent events"
        )
        scenario_rows.append(
            [scenario_cfg.key, scenario_cfg.label, prior, scenario_cfg.description]
        )
    scenario_defs = _markdown_table(
        ["Key", "Label", "Fraud prior", "Generation"], scenario_rows
    )

    now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    document = f"""# HydroLedger Phase 0B — Simulation and Ablation Method

Generated by `sim/ablation_experiment.py` on {now} UTC.

- Command: `{command}`
- Python: {platform.python_version()}
- Platform: `{platform.platform()}`
- Events per seed per scenario: {events_per_seed:,}
- Seeds: {", ".join(str(seed) for seed in seeds)}
- Scenarios: {len(config.SCENARIOS)}
- Methods: {", ".join(config.METHOD_ORDER)}

## Scope and honesty statement

This is a **synthetic simulation**, not field data and not a claim about
Hyderabad rainfall or flood behaviour. The numbers below describe the
behaviour of four algorithms on the generator in `sim/config.py`. They are
not evidence of real-world accuracy, production readiness, Sybil resistance
or tamper resistance.

The parameters were fixed before evaluation. No parameter was tuned on the
evaluation seeds, and no result was adjusted to match a historical headline
figure. A rerun with the same code, seeds, parameters and Python
implementation reproduces the tables to the printed precision.

## Ground truth and event proportions

`fraudulent=True` means the claimed effective depth is deliberately
inconsistent with the synthetic rainfall process. Honest sensor noise and
under-claims are not fraud. Positive class = fraudulent reading.

{scenario_defs}

Scenarios 1-5 use an exact number of fraudulent events per seed
(`round(fraud_fraction × events)`). The coordinated scenarios choose a fixed
fraction of compromised devices in every cell; the realised event share is
therefore close to, but not exactly, the nominal fraction.

## Distributions and world parameters

| Parameter | Value |
|---|---|
| Cells | {config.CELLS} |
| Devices per cell | {config.DEVICES_PER_CELL} |
| Simulation horizon | {config.DURATION_S} s (24 h) |
| Roof area | uniform {config.ROOF_AREA_M2_RANGE} m² |
| Runoff coefficient | uniform {config.RUNOFF_COEFF_RANGE} |
| Tank capacity | {config.TANK_CAPACITY_L:,.0f} L |
| Initial stored volume | uniform {config.STORED_LITRES_RANGE} L |
| Honest sensor noise | Gaussian, σ = {config.SENSOR_NOISE_MM} mm |
| Ordinary storms per cell | {config.STORMS_PER_CELL[0]}–{config.STORMS_PER_CELL[1]} Gaussian pulses |
| Storm centre | uniform {config.STORM_CENTRE_FRACTION_RANGE} of the 24 h horizon |
| Storm width | uniform {config.STORM_WIDTH_S[0]}–{config.STORM_WIDTH_S[1]} s |
| Storm peak | uniform {config.STORM_PEAK_MM[0]}–{config.STORM_PEAK_MM[1]} mm |
| Microburst centre | uniform {config.MICROBURST_CENTRE_FRACTION_RANGE} of the 24 h horizon |
| Radar gain (ordinary) | uniform {config.RADAR_GAIN_RANGE} |
| Radar relative / absolute noise | {config.RADAR_REL_NOISE} / {config.RADAR_ABS_NOISE_MM} mm |
| Microburst peak / width | {config.MICROBURST_PEAK_MM} mm / {config.MICROBURST_WIDTH_S} s |
| Microburst radar catch | uniform {config.MICROBURST_RADAR_CATCH_RANGE} |
| Normal / spike over-claim | {config.NORMAL_OVERCLAIM_MM} / {config.SPIKE_OVERCLAIM_MM} mm |
| Dry-spoof claim | uniform {config.DRY_SPOOF_CLAIM_MM} mm |
| Radar-bounded true depth | uniform {config.RADAR_BOUNDED_TRUE_MM} mm |
| Radar-bounded radar | uniform {config.RADAR_BOUNDED_RADAR_MM} mm |
| Radar-bounded claim | uniform {config.RADAR_BOUNDED_CLAIM_FRACTION} × physical bound, capped at {config.RADAR_BOUNDED_MAX_FRACTION} × bound |
| Radar-bounded claim noise | Gaussian, σ = {config.RADAR_BOUNDED_CLAIM_NOISE_MM} mm |
| Coordinated claim | Gaussian, μ = {config.COORDINATED_CLAIM_MEAN_MM} mm, σ = {config.COORDINATED_CLAIM_SD_MM} mm; minimum over-claim {config.COORDINATED_MIN_OVERCLAIM_MM} mm |
| Coordinated true depth | uniform {config.COORDINATED_TRUE_MM} mm |
| Dry radar | uniform {config.DRY_RADAR_MM} mm |

The tank capacity is deliberately large enough that the storage/catchment
rule is not the discriminating factor. All four methods would reject a
capacity overflow before their main rule; this experiment does not measure
capacity validation.

## Verification methods

### 1. Static threshold

Accept when `R_obs <= {config.STATIC_THRESHOLD_MM} mm`; reject otherwise.
This is a naive fixed-depth baseline. It does not use radar or peers.

### 2. Physics-only

Accept when `R_obs <= R_max`, where
`R_max = {config.ALPHA} × RadarMM + {config.BETA_MM}`; reject otherwise.
This is the Go verifier's radar physical bound without peer evidence.

### 3. Peer-only

Maintain a per-cell window containing the latest observation from each
device. Peers are other devices observed within `{config.PEER_MAX_AGE_S}`
seconds. When at least `{config.MIN_PEERS}` peers exist, compute
`M = median(peers)`, `MAD = median(|x - M|)`, and
`Z = {config.Z_SCALE} × |R_obs - M| / (MAD + {config.MAD_FLOOR_MM})`.
Reject when `Z > {config.OUTLIER_Z}` **and** `R_obs > M` (a positive
outlier). With fewer than `{config.MIN_PEERS}` peers, accept: peer-only has
no evidence to reject. Radar is not used.

### 4. Hybrid physics + peers

With fewer than `{config.MIN_PEERS}` peers, accept when `R_obs <= R_max`
and reject otherwise. With enough peers, first reject a positive outlier.
Otherwise, reject when the radar is dry (`RadarMM <= {config.RADAR_DRY_MM}`),
the peer median is rainy (`M >= {config.PEER_RAIN_MM}`), and the peer spread is
coherent (`max - min <= max({config.COHERENCE_ABS_MM},
{config.COHERENCE_REL_FRAC} × M)`). Otherwise reject when `R_obs > R_max`
and accept when it is within the bound.

The dry-radar/rainy-peer case corresponds to the Go verifier's
`PROVISIONAL_ESCROW`. Phase 0B has no TTL resolution, so a provisional
escrow is counted as **not accepted** (rejected) for the binary confusion
matrix. This is conservative and is stated in the limitations.

## Metrics

For each scenario, seed and method:

- `precision = TP / (TP + FP)`
- `recall = TP / (TP + FN)`
- `F1 = 2 × precision × recall / (precision + recall)`
- `false_positive_rate = FP / (FP + TN)`

A zero denominator yields `0.0`; this convention is applied consistently.
Mean and sample standard deviation (`n - 1`) are computed across seeds.
Per-scenario and overall metrics are computed per seed and then averaged;
they are not the F1 of averaged counts. `overall_results.csv` pools all
scenario confusions within each seed, so it reflects this deliberately
adversarial scenario mix, not a real-world fraud prevalence.

## Latest results — overall

{overall_table}

## Latest results — per scenario

{scenario_table}

## Reproducibility

```bash
bash sim/run_all.sh
```

This runs the unit tests and then regenerates `overall_results.csv`,
`scenario_results.csv`, `confusion_matrices.csv` and this file. No network
access is used. The implementation depends only on the Python standard
library; see `sim/requirements.txt`. Each scenario/seed pair uses
`random.Random(seed * {config.SEED_STRIDE} + scenario_index)`; Python's
randomized `hash` is never used.

## Known limitations

- Synthetic generator only; no field data, no real Hyderabad rainfall, no
  flood prediction.
- The radar is supplied per reading, as in the current Go `Reading`. A
  reading can therefore carry its own radar value; the simulation does not
  model authenticated radar or device identity.
- The peer window is simulated, not the Go implementation itself. The
  algorithm mirrors the documented Go rules, but this is not an integration
  test of the Go package.
- Provisional escrow is mapped to "rejected" for binary metrics; escrow TTL
  resolution is not simulated.
- No Sybil resistance: coordinated compromise is modelled only as a fraction
  of devices. At 60% compromise a peer majority can invert the consensus;
  inspect `scenario_results.csv` for the observed effect.
- Capacity validation is deliberately non-binding in this experiment.
- The scenario mix and fraud priors are design choices. Changing them changes
  the overall metrics; they are not estimates of real-world prevalence.
"""

    with open(path, "w", encoding="utf-8", newline="\n") as handle:
        handle.write(document)


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def _parse_seeds(raw: str) -> tuple[int, ...]:
    parts = [part.strip() for part in raw.split(",") if part.strip()]
    if not parts:
        raise argparse.ArgumentTypeError("at least one seed is required")
    try:
        return tuple(int(part) for part in parts)
    except ValueError as exc:
        raise argparse.ArgumentTypeError(f"invalid seed list: {raw}") from exc


def _parse_scenarios(raw: str) -> tuple[str, ...] | None:
    parts = [part.strip() for part in raw.split(",") if part.strip()]
    return tuple(parts) if parts else None


def _progress(scenario: str, seed: int, by_method: dict[str, Confusion]) -> None:
    def line(method: str) -> str:
        metrics = metrics_from_confusion(by_method[method])
        return f"{method} F1={metrics.f1:.4f} FPR={metrics.false_positive_rate:.4f}"

    print(
        f"[{scenario}] seed {seed}: " + " | ".join(line(m) for m in config.METHOD_ORDER),
        file=sys.stderr,
        flush=True,
    )


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Run the HydroLedger Phase 0B synthetic ablation."
    )
    parser.add_argument(
        "--events",
        type=int,
        default=config.EVENTS_PER_SEED,
        help=f"events per seed per scenario (default {config.EVENTS_PER_SEED})",
    )
    parser.add_argument(
        "--seeds",
        type=_parse_seeds,
        default=config.SEEDS,
        help="comma-separated integer seeds (default: 1..10)",
    )
    parser.add_argument(
        "--scenarios",
        type=_parse_scenarios,
        default=None,
        help="comma-separated scenario keys (default: all seven)",
    )
    parser.add_argument(
        "--output-dir",
        default=config.DEFAULT_OUTPUT_DIR,
        help=f"output directory (default {config.DEFAULT_OUTPUT_DIR})",
    )
    args = parser.parse_args(argv)

    command = "python " + " ".join(sys.argv)
    records = run_experiment(
        events_per_seed=args.events,
        seeds=args.seeds,
        scenario_keys=args.scenarios,
        progress=_progress,
    )
    write_results(
        records,
        args.output_dir,
        args.events,
        args.seeds,
        command,
    )
    print(f"wrote results to {args.output_dir}", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
