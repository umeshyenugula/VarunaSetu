"""Python mirror of the Go verifier's three-state decision logic.

This module is deliberately a *mirror*, not a second specification. It follows
the behaviour documented in ``CONTRACTS.md`` and implemented in
``hydroledger/verifier``:

* field validation, then storage/catchment capacity;
* one latest observation per device per cell, bounded by ``window_max_devices``;
* eligible peers are other devices in the same cell with
  ``reading.timestamp - peer.timestamp <= peer_max_age_s`` (negative ages are
  eligible, as in Go);
* median/MAD robust Z, positive-outlier test, radar dry / peers rain / coherent
  test, then the radar physical bound;
* in-memory escrows with TTL, later-radar corroboration and neighbour-cell
  corroboration as of a caller-supplied ``now``.

Timestamps are Python ``datetime`` objects. Internally they are converted to
integer nanoseconds so age and TTL comparisons do not accumulate floating
point error and so the hot loops stay fast. Python ``datetime`` itself has
microsecond resolution; fixtures therefore use whole seconds.

This is a research simulator. It does not authenticate devices or radar, does
not persist state, and does not make HydroLedger Sybil-proof or tamper-proof.
"""

from __future__ import annotations

import math
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from typing import Iterable, Sequence

ACCEPT = "ACCEPT"
PROVISIONAL_ESCROW = "PROVISIONAL_ESCROW"
QUARANTINE = "QUARANTINE"

REASON_RADAR_AND_PEERS_AGREE = "RADAR_AND_PEERS_AGREE"
REASON_RADAR_PHYSICS_FALLBACK = "RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS"
REASON_INSUFFICIENT_PEERS_REVIEW = "INSUFFICIENT_PEERS_REVIEW"
REASON_RADAR_PEER_CONFLICT_ESCROW = "RADAR_PEER_CONFLICT_ESCROW"
REASON_SPATIAL_PEER_OUTLIER = "SPATIAL_PEER_OUTLIER"
REASON_PHYSICAL_BOUND_EXCEEDED = "PHYSICAL_BOUND_EXCEEDED"
REASON_INVALID_INPUT = "INVALID_INPUT"
REASON_ESCROW_CORROBORATED = "ESCROW_CORROBORATED"
REASON_ESCROW_EXPIRED = "ESCROW_EXPIRED_UNCORROBORATED"
REASON_CAPACITY_EXCEEDED = "STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED"

STATES = (ACCEPT, PROVISIONAL_ESCROW, QUARANTINE)

_EPOCH = datetime(1970, 1, 1, tzinfo=timezone.utc)
_NS = 1_000_000_000


def to_ns(value: datetime) -> int:
    """Convert a timezone-aware datetime to integer nanoseconds since epoch.

    ``datetime`` resolution is microseconds, so the low three digits of the
    result are always zero. A naive datetime is rejected because mixing naive
    and aware values would silently produce wrong ages.
    """

    if value.tzinfo is None or value.utcoffset() is None:
        raise ValueError("timestamps must be timezone-aware")
    delta = value - _EPOCH
    return (
        (delta.days * 86_400 + delta.seconds) * _NS
        + delta.microseconds * 1_000
    )


def from_ns(value: int) -> datetime:
    """Inverse of :func:`to_ns`, rounded to the nearest microsecond."""

    return _EPOCH + timedelta(microseconds=value // 1_000)


def _is_zero_timestamp(value: datetime | None) -> bool:
    if value is None:
        return True
    return (
        value.year == 1
        and value.month == 1
        and value.day == 1
        and value.hour == 0
        and value.minute == 0
        and value.second == 0
        and value.microsecond == 0
    )


def _finite(value: float) -> bool:
    return math.isfinite(value)


@dataclass(frozen=True)
class Config:
    """Verifier tunables. Defaults mirror ``verifier.DefaultConfig()``."""

    alpha: float = 1.15
    beta_mm: float = 2.0

    min_peers: int = 4
    peer_max_age_s: float = 600.0
    window_max_devices: int = 128

    z_scale: float = 0.6745
    mad_floor_mm: float = 0.15
    outlier_z: float = 3.5

    peer_rain_mm: float = 2.0
    coherence_abs_mm: float = 1.0
    coherence_rel_frac: float = 0.25

    radar_dry_mm: float = 0.5
    escrow_ttl_s: float = 900.0
    neighbor_min_cells: int = 2
    neighbor_min_devices: int = 3

    def validate(self) -> None:
        non_neg = {
            "alpha": self.alpha,
            "beta_mm": self.beta_mm,
            "peer_rain_mm": self.peer_rain_mm,
            "coherence_abs_mm": self.coherence_abs_mm,
            "coherence_rel_frac": self.coherence_rel_frac,
            "radar_dry_mm": self.radar_dry_mm,
        }
        positive = {
            "z_scale": self.z_scale,
            "mad_floor_mm": self.mad_floor_mm,
            "outlier_z": self.outlier_z,
            "peer_max_age_s": self.peer_max_age_s,
            "escrow_ttl_s": self.escrow_ttl_s,
        }
        for name, value in non_neg.items():
            if not _finite(value) or value < 0:
                raise ValueError(f"config {name} must be finite and >= 0")
        for name, value in positive.items():
            if not _finite(value) or value <= 0:
                raise ValueError(f"config {name} must be finite and > 0")
        if self.min_peers < 1:
            raise ValueError("config min_peers must be >= 1")
        if self.window_max_devices < self.min_peers + 1:
            raise ValueError("config window_max_devices must be >= min_peers + 1")
        if self.neighbor_min_cells < 1:
            raise ValueError("config neighbor_min_cells must be >= 1")
        if self.neighbor_min_devices < 1:
            raise ValueError("config neighbor_min_devices must be >= 1")


DEFAULT_CONFIG = Config()


@dataclass(frozen=True)
class Reading:
    """One device report, matching the fields of the Go ``verifier.Reading``."""

    device_id: str
    timestamp: datetime
    claimed_litres: float
    roof_area_m2: float
    runoff_coeff: float
    stored_litres: float
    capacity_litres: float
    radar_mm: float
    cell_id: str


@dataclass(frozen=True)
class Decision:
    """Result of :meth:`ThreeStateVerifier.evaluate`."""

    state: str
    reason: str
    r_obs_mm: float = 0.0
    r_max_mm: float = 0.0
    peer_count: int = 0
    peer_median_mm: float = 0.0
    peer_mad_mm: float = 0.0
    z_score: float = 0.0


@dataclass(frozen=True)
class Resolution:
    """Final outcome of one escrow, returned once by ``resolve_escrows``."""

    device_id: str
    cell_id: str
    reading_timestamp: datetime
    resolved_at: datetime
    state: str
    reason: str


@dataclass
class _Observation:
    device: str
    ts_ns: int
    depth_mm: float


@dataclass
class _Escrow:
    device: str
    ts_ns: int
    radar_ok: bool = False


@dataclass
class _Cell:
    cell_id: str
    observations: list[_Observation] = field(default_factory=list)
    escrows: list[_Escrow] = field(default_factory=list)


def median_sorted(values: Sequence[float]) -> float:
    """Median of a sorted, non-empty sequence (mean of middle pair if even)."""

    n = len(values)
    if n % 2 == 1:
        return values[n // 2]
    return values[n // 2 - 1] / 2.0 + values[n // 2] / 2.0


def peer_stats(values: Sequence[float]) -> tuple[float, float, float]:
    """Median, MAD and spread of a non-empty sequence; does not mutate input."""

    ordered = sorted(values)
    median = median_sorted(ordered)
    deviations = sorted(abs(value - median) for value in ordered)
    mad = median_sorted(deviations)
    return median, mad, ordered[-1] - ordered[0]


def _invalid(reason: str = REASON_INVALID_INPUT) -> Decision:
    return Decision(state=QUARANTINE, reason=reason)


def _valid_reading(reading: Reading) -> bool:
    if not reading.device_id or not reading.cell_id:
        return False
    if _is_zero_timestamp(reading.timestamp):
        return False
    if not _finite(reading.claimed_litres) or reading.claimed_litres < 0:
        return False
    if not _finite(reading.roof_area_m2) or reading.roof_area_m2 <= 0:
        return False
    if not (reading.runoff_coeff > 0.0 and reading.runoff_coeff <= 1.0):
        return False
    if not _finite(reading.stored_litres) or reading.stored_litres < 0:
        return False
    if not _finite(reading.capacity_litres) or reading.capacity_litres < 0:
        return False
    if not _finite(reading.radar_mm) or reading.radar_mm < 0:
        return False
    return True


class ThreeStateVerifier:
    """In-memory, single-threaded mirror of the Go verifier.

    The Go implementation is safe for concurrent use. This Python mirror is a
    simulator and is intentionally not thread-safe; use one instance per
    simulation run.
    """

    def __init__(self, config: Config | None = None) -> None:
        self._cfg = config or DEFAULT_CONFIG
        self._cfg.validate()
        self._cells: dict[str, _Cell] = {}
        self._neighbors: dict[str, list[str]] = {}
        self._peer_max_age_ns = int(self._cfg.peer_max_age_s * _NS)
        self._escrow_ttl_ns = int(self._cfg.escrow_ttl_s * _NS)

    @property
    def config(self) -> Config:
        return self._cfg

    def set_neighbors(self, cell_id: str, neighbors: Iterable[str]) -> None:
        """Replace the directed neighbour list for ``cell_id``.

        Empty ids, duplicates and the cell itself are dropped, matching Go.
        An empty resulting list removes the configuration.
        """

        if not cell_id:
            return
        out: list[str] = []
        for neighbor in neighbors:
            if not neighbor or neighbor == cell_id or neighbor in out:
                continue
            out.append(neighbor)
        if out:
            self._neighbors[cell_id] = out
        else:
            self._neighbors.pop(cell_id, None)

    def pending_escrows(self) -> int:
        return sum(len(cell.escrows) for cell in self._cells.values())

    def evaluate(self, reading: Reading) -> Decision:
        """Classify one reading and update the cell state exactly like Go."""

        if not _valid_reading(reading):
            return _invalid()

        cfg = self._cfg
        r_obs = reading.claimed_litres / (
            reading.roof_area_m2 * reading.runoff_coeff
        )
        r_max = cfg.alpha * reading.radar_mm + cfg.beta_mm
        if not _finite(r_obs) or not _finite(r_max):
            return _invalid()

        decision = Decision(
            state=QUARANTINE,
            reason=REASON_INVALID_INPUT,
            r_obs_mm=r_obs,
            r_max_mm=r_max,
        )

        if (
            reading.stored_litres > reading.capacity_litres
            or reading.claimed_litres
            > reading.capacity_litres - reading.stored_litres
        ):
            return Decision(
                state=QUARANTINE,
                reason=REASON_CAPACITY_EXCEEDED,
                r_obs_mm=r_obs,
                r_max_mm=r_max,
            )

        ts_ns = to_ns(reading.timestamp)
        cell = self._cells.get(reading.cell_id)
        if cell is None:
            cell = _Cell(cell_id=reading.cell_id)
            self._cells[cell.cell_id] = cell

        decision = self._classify(cell, reading, ts_ns, r_obs, r_max)
        self._record(cell, reading, ts_ns, r_obs, cfg.window_max_devices)

        if (
            reading.radar_mm > cfg.radar_dry_mm
            and cell.escrows
        ):
            self._corroborate_by_radar(cell, ts_ns)

        if decision.state == PROVISIONAL_ESCROW:
            cell.escrows.append(_Escrow(device=reading.device_id, ts_ns=ts_ns))

        return decision

    def _classify(
        self,
        cell: _Cell,
        reading: Reading,
        ts_ns: int,
        r_obs: float,
        r_max: float,
    ) -> Decision:
        cfg = self._cfg
        peers = [
            observation.depth_mm
            for observation in cell.observations
            if observation.device != reading.device_id
            and ts_ns - observation.ts_ns <= self._peer_max_age_ns
        ]
        decision = Decision(
            state=QUARANTINE,
            reason=REASON_INVALID_INPUT,
            r_obs_mm=r_obs,
            r_max_mm=r_max,
            peer_count=len(peers),
        )

        if len(peers) < cfg.min_peers:
            if r_obs <= r_max:
                decision = Decision(
                    state=ACCEPT,
                    reason=REASON_RADAR_PHYSICS_FALLBACK,
                    r_obs_mm=r_obs,
                    r_max_mm=r_max,
                    peer_count=len(peers),
                )
            else:
                decision = Decision(
                    state=QUARANTINE,
                    reason=REASON_INSUFFICIENT_PEERS_REVIEW,
                    r_obs_mm=r_obs,
                    r_max_mm=r_max,
                    peer_count=len(peers),
                )
            return decision

        median, mad, spread = peer_stats(peers)
        z_score = cfg.z_scale * abs(r_obs - median) / (
            mad + cfg.mad_floor_mm
        )
        positive_outlier = z_score > cfg.outlier_z and r_obs > median
        radar_dry = reading.radar_mm <= cfg.radar_dry_mm
        peers_rain = median >= cfg.peer_rain_mm
        peers_coherent = spread <= max(
            cfg.coherence_abs_mm, cfg.coherence_rel_frac * median
        )

        if positive_outlier:
            state, reason = QUARANTINE, REASON_SPATIAL_PEER_OUTLIER
        elif radar_dry and peers_rain and peers_coherent:
            state, reason = PROVISIONAL_ESCROW, REASON_RADAR_PEER_CONFLICT_ESCROW
        elif r_obs > r_max:
            state, reason = QUARANTINE, REASON_PHYSICAL_BOUND_EXCEEDED
        else:
            state, reason = ACCEPT, REASON_RADAR_AND_PEERS_AGREE

        return Decision(
            state=state,
            reason=reason,
            r_obs_mm=r_obs,
            r_max_mm=r_max,
            peer_count=len(peers),
            peer_median_mm=median,
            peer_mad_mm=mad,
            z_score=z_score,
        )

    @staticmethod
    def _record(
        cell: _Cell,
        reading: Reading,
        ts_ns: int,
        depth_mm: float,
        max_devices: int,
    ) -> None:
        for observation in cell.observations:
            if observation.device == reading.device_id:
                if ts_ns >= observation.ts_ns:
                    observation.ts_ns = ts_ns
                    observation.depth_mm = depth_mm
                return

        observation = _Observation(
            device=reading.device_id, ts_ns=ts_ns, depth_mm=depth_mm
        )
        if len(cell.observations) < max_devices:
            cell.observations.append(observation)
            return

        oldest = 0
        for index in range(1, len(cell.observations)):
            if cell.observations[index].ts_ns < cell.observations[oldest].ts_ns:
                oldest = index
        if ts_ns < cell.observations[oldest].ts_ns:
            return
        cell.observations[oldest] = observation

    def _corroborate_by_radar(self, cell: _Cell, ts_ns: int) -> None:
        for escrow in cell.escrows:
            if escrow.radar_ok:
                continue
            delta = ts_ns - escrow.ts_ns
            if 0 < delta <= self._escrow_ttl_ns:
                escrow.radar_ok = True

    def _neighbors_corroborate(self, cell_id: str, now_ns: int) -> bool:
        cfg = self._cfg
        neighbors = self._neighbors.get(cell_id, [])
        if len(neighbors) < cfg.neighbor_min_cells:
            return False

        qualifying = 0
        for neighbor_id in neighbors:
            cell = self._cells.get(neighbor_id)
            if cell is None:
                continue
            values = [
                observation.depth_mm
                for observation in cell.observations
                if now_ns - observation.ts_ns <= self._peer_max_age_ns
            ]
            if len(values) < cfg.neighbor_min_devices:
                continue
            values.sort()
            if median_sorted(values) >= cfg.peer_rain_mm:
                qualifying += 1
                if qualifying >= cfg.neighbor_min_cells:
                    return True
        return False

    def resolve_escrows(self, now: datetime) -> list[Resolution]:
        """Resolve escrows as of ``now``; each is returned at most once."""

        now_ns = to_ns(now)
        resolved: list[Resolution] = []

        for cell_id in sorted(self._cells):
            cell = self._cells[cell_id]
            if not cell.escrows:
                continue
            neighbor_ok = self._neighbors_corroborate(cell_id, now_ns)
            kept: list[_Escrow] = []
            for escrow in cell.escrows:
                if escrow.ts_ns > now_ns:
                    kept.append(escrow)
                    continue
                delta = now_ns - escrow.ts_ns
                if escrow.radar_ok or (
                    delta <= self._escrow_ttl_ns and neighbor_ok
                ):
                    state, reason = ACCEPT, REASON_ESCROW_CORROBORATED
                elif delta > self._escrow_ttl_ns:
                    state, reason = QUARANTINE, REASON_ESCROW_EXPIRED
                else:
                    kept.append(escrow)
                    continue
                resolved.append(
                    Resolution(
                        device_id=escrow.device,
                        cell_id=cell_id,
                        reading_timestamp=from_ns(escrow.ts_ns),
                        resolved_at=now,
                        state=state,
                        reason=reason,
                    )
                )
            cell.escrows = kept

        resolved.sort(
            key=lambda item: (
                item.cell_id,
                item.reading_timestamp,
                item.device_id,
            )
        )
        return resolved

    # ------------------------------------------------------------------
    # Convenience helpers used by the experiment drivers
    # ------------------------------------------------------------------

    def cell_ids(self) -> list[str]:
        return sorted(self._cells)
