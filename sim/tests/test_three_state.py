"""Offline tests for the Python mirror of the Go three-state verifier."""

from __future__ import annotations

import math
import unittest
from datetime import datetime, timedelta, timezone

from sim import three_state as ts

BASE = datetime(2024, 1, 1, tzinfo=timezone.utc)


def reading(
    device: str,
    *,
    at: float = 0.0,
    depth_mm: float = 1.0,
    radar_mm: float = 10.0,
    cell: str = "cell-a",
    area_m2: float = 100.0,
    runoff: float = 1.0,
    stored: float = 0.0,
    capacity: float = 100_000.0,
    claimed_litres: float | None = None,
) -> ts.Reading:
    return ts.Reading(
        device_id=device,
        timestamp=BASE + timedelta(seconds=at),
        claimed_litres=(
            depth_mm * area_m2 * runoff
            if claimed_litres is None
            else claimed_litres
        ),
        roof_area_m2=area_m2,
        runoff_coeff=runoff,
        stored_litres=stored,
        capacity_litres=capacity,
        radar_mm=radar_mm,
        cell_id=cell,
    )


def add_peers(
    verifier: ts.ThreeStateVerifier,
    depths: list[float],
    *,
    cell: str = "cell-a",
    radar_mm: float = 10.0,
    start: float = 0.0,
) -> None:
    for index, depth in enumerate(depths):
        verifier.evaluate(
            reading(
                f"peer-{index}",
                at=start + index,
                depth_mm=depth,
                radar_mm=radar_mm,
                cell=cell,
            )
        )


class TestConfig(unittest.TestCase):
    def test_defaults_match_go(self) -> None:
        cfg = ts.DEFAULT_CONFIG
        self.assertEqual(cfg.alpha, 1.15)
        self.assertEqual(cfg.beta_mm, 2.0)
        self.assertEqual(cfg.min_peers, 4)
        self.assertEqual(cfg.peer_max_age_s, 600.0)
        self.assertEqual(cfg.window_max_devices, 128)
        self.assertEqual(cfg.z_scale, 0.6745)
        self.assertEqual(cfg.mad_floor_mm, 0.15)
        self.assertEqual(cfg.outlier_z, 3.5)
        self.assertEqual(cfg.peer_rain_mm, 2.0)
        self.assertEqual(cfg.coherence_abs_mm, 1.0)
        self.assertEqual(cfg.coherence_rel_frac, 0.25)
        self.assertEqual(cfg.radar_dry_mm, 0.5)
        self.assertEqual(cfg.escrow_ttl_s, 900.0)
        self.assertEqual(cfg.neighbor_min_cells, 2)
        self.assertEqual(cfg.neighbor_min_devices, 3)

    def test_invalid_configs_rejected(self) -> None:
        for cfg in (
            ts.Config(min_peers=0),
            ts.Config(peer_max_age_s=0),
            ts.Config(escrow_ttl_s=0),
            ts.Config(mad_floor_mm=0),
            ts.Config(window_max_devices=4),
            ts.Config(alpha=-1),
            ts.Config(neighbor_min_cells=0),
        ):
            with self.subTest(cfg=cfg):
                with self.assertRaises(ValueError):
                    ts.ThreeStateVerifier(cfg)


class TestValidationAndCapacity(unittest.TestCase):
    def setUp(self) -> None:
        self.verifier = ts.ThreeStateVerifier()

    def test_invalid_fields(self) -> None:
        zero_timestamp = ts.Reading(
            device_id="d",
            timestamp=datetime(1, 1, 1, tzinfo=timezone.utc),
            claimed_litres=100.0,
            roof_area_m2=100.0,
            runoff_coeff=1.0,
            stored_litres=0.0,
            capacity_litres=100_000.0,
            radar_mm=0.0,
            cell_id="cell-a",
        )
        cases = [
            reading(""),
            reading("d", cell=""),
            zero_timestamp,
            reading("d", depth_mm=-1),
            reading("d", area_m2=0),
            reading("d", runoff=0),
            reading("d", runoff=1.1),
            reading("d", stored=-1),
            reading("d", capacity=-1),
            reading("d", radar_mm=-1),
            reading("d", claimed_litres=math.inf),
            reading("d", claimed_litres=math.nan),
        ]
        for item in cases:
            with self.subTest(item=item):
                decision = self.verifier.evaluate(item)
                self.assertEqual(decision.state, ts.QUARANTINE)
                self.assertEqual(decision.reason, ts.REASON_INVALID_INPUT)

    def test_capacity_boundary(self) -> None:
        ok = self.verifier.evaluate(
            reading("d", depth_mm=0.0, stored=99_000, capacity=100_000)
        )
        self.assertEqual(ok.state, ts.ACCEPT)

        exact = self.verifier.evaluate(
            reading(
                "d2",
                depth_mm=10.0,
                stored=99_000,
                capacity=100_000,
            )
        )
        self.assertNotEqual(exact.reason, ts.REASON_CAPACITY_EXCEEDED)

        over = self.verifier.evaluate(
            reading(
                "d2b",
                depth_mm=10.001,
                stored=99_000,
                capacity=100_000,
            )
        )
        self.assertEqual(over.reason, ts.REASON_CAPACITY_EXCEEDED)

        stored_over = self.verifier.evaluate(
            reading("d3", stored=100_001, capacity=100_000)
        )
        self.assertEqual(stored_over.reason, ts.REASON_CAPACITY_EXCEEDED)

    def test_invalid_readings_are_not_recorded(self) -> None:
        self.verifier.evaluate(reading("d", depth_mm=-5))
        self.assertEqual(self.verifier.cell_ids(), [])


class TestPeerRules(unittest.TestCase):
    def setUp(self) -> None:
        self.verifier = ts.ThreeStateVerifier()

    def test_insufficient_peers_fallback(self) -> None:
        accept = self.verifier.evaluate(reading("d", depth_mm=1.0, radar_mm=0.0))
        self.assertEqual(accept.state, ts.ACCEPT)
        self.assertEqual(accept.reason, ts.REASON_RADAR_PHYSICS_FALLBACK)

        review = self.verifier.evaluate(
            reading("d2", depth_mm=3.0, radar_mm=0.0)
        )
        self.assertEqual(review.state, ts.QUARANTINE)
        self.assertEqual(review.reason, ts.REASON_INSUFFICIENT_PEERS_REVIEW)

    def test_three_peers_are_not_enough(self) -> None:
        add_peers(self.verifier, [1.0, 1.0, 1.0], radar_mm=10.0)
        decision = self.verifier.evaluate(
            reading("target", at=10, depth_mm=1.0, radar_mm=10.0)
        )
        self.assertEqual(decision.peer_count, 3)
        self.assertEqual(decision.reason, ts.REASON_RADAR_PHYSICS_FALLBACK)

    def test_four_coherent_peers_agree(self) -> None:
        add_peers(self.verifier, [1.0, 1.0, 1.0, 1.0], radar_mm=5.0)
        decision = self.verifier.evaluate(
            reading("target", at=10, depth_mm=1.1, radar_mm=5.0)
        )
        self.assertEqual(decision.state, ts.ACCEPT)
        self.assertEqual(decision.reason, ts.REASON_RADAR_AND_PEERS_AGREE)
        self.assertEqual(decision.peer_count, 4)
        self.assertAlmostEqual(decision.peer_median_mm, 1.0)
        self.assertAlmostEqual(decision.peer_mad_mm, 0.0)
        self.assertAlmostEqual(
            decision.z_score, 0.6745 * 0.1 / 0.15, places=12
        )

    def test_positive_outlier(self) -> None:
        add_peers(self.verifier, [1.0, 1.0, 1.0, 1.0], radar_mm=5.0)
        decision = self.verifier.evaluate(
            reading("target", at=10, depth_mm=10.0, radar_mm=5.0)
        )
        self.assertEqual(decision.reason, ts.REASON_SPATIAL_PEER_OUTLIER)

    def test_outlier_z_boundary_is_strict(self) -> None:
        edge = (
            ts.DEFAULT_CONFIG.outlier_z
            * ts.DEFAULT_CONFIG.mad_floor_mm
            / ts.DEFAULT_CONFIG.z_scale
        )
        add_peers(self.verifier, [0.0, 0.0, 0.0, 0.0], radar_mm=100.0)
        below = self.verifier.evaluate(
            reading(
                "below",
                at=10,
                depth_mm=math.nextafter(edge, 0.0),
                radar_mm=100.0,
            )
        )
        self.assertFalse(
            below.reason == ts.REASON_SPATIAL_PEER_OUTLIER,
            below,
        )
        above = self.verifier.evaluate(
            reading("above", at=11, depth_mm=edge + 1e-6, radar_mm=100.0)
        )
        self.assertEqual(above.reason, ts.REASON_SPATIAL_PEER_OUTLIER)

    def test_negative_deviation_is_not_an_outlier(self) -> None:
        add_peers(
            self.verifier,
            [8.0, 8.25, 7.75, 8.0],
            radar_mm=100.0,
        )
        decision = self.verifier.evaluate(
            reading("target", at=10, depth_mm=1.0, radar_mm=100.0)
        )
        self.assertNotEqual(decision.reason, ts.REASON_SPATIAL_PEER_OUTLIER)

    def test_physical_bound_after_peers(self) -> None:
        add_peers(
            self.verifier,
            [7.0, 7.5, 8.0, 8.5],
            radar_mm=5.0,
        )
        decision = self.verifier.evaluate(
            reading("target", at=10, depth_mm=8.0, radar_mm=5.0)
        )
        self.assertEqual(decision.reason, ts.REASON_PHYSICAL_BOUND_EXCEEDED)

    def test_peer_age_boundary(self) -> None:
        self.verifier.evaluate(reading("old", at=0.0, depth_mm=1.0))
        at_edge = self.verifier.evaluate(
            reading("target", at=600.0, depth_mm=1.0, radar_mm=10.0)
        )
        self.assertEqual(at_edge.peer_count, 1)

        fresh = ts.ThreeStateVerifier()
        fresh.evaluate(reading("old", at=0.0, depth_mm=1.0))
        just_over = fresh.evaluate(
            reading("target", at=600.000001, depth_mm=1.0, radar_mm=10.0)
        )
        self.assertEqual(just_over.peer_count, 0)

    def test_same_device_is_never_its_own_peer(self) -> None:
        self.verifier.evaluate(reading("d", at=0.0, depth_mm=1.0))
        decision = self.verifier.evaluate(
            reading("d", at=10.0, depth_mm=1.0)
        )
        self.assertEqual(decision.peer_count, 0)

    def test_older_reading_does_not_replace_newer(self) -> None:
        self.verifier.evaluate(reading("d", at=100.0, depth_mm=5.0))
        self.verifier.evaluate(reading("d", at=50.0, depth_mm=1.0))
        observation = self.verifier._cells["cell-a"].observations[0]
        self.assertAlmostEqual(observation.depth_mm, 5.0)
        self.assertEqual(
            ts.from_ns(observation.ts_ns),
            BASE + timedelta(seconds=100),
        )


class TestEscrow(unittest.TestCase):
    def setUp(self) -> None:
        self.verifier = ts.ThreeStateVerifier()

    def _make_escrow(self, *, cell: str = "cell-a", at: float = 100.0) -> ts.Decision:
        add_peers(
            self.verifier,
            [9.0, 9.2, 8.8, 9.0],
            cell=cell,
            radar_mm=0.0,
            start=at - 10,
        )
        decision = self.verifier.evaluate(
            reading(
                "target",
                at=at,
                depth_mm=9.0,
                radar_mm=0.0,
                cell=cell,
            )
        )
        self.assertEqual(decision.state, ts.PROVISIONAL_ESCROW)
        self.assertEqual(decision.reason, ts.REASON_RADAR_PEER_CONFLICT_ESCROW)
        return decision

    def test_escrow_is_pending_at_exact_ttl(self) -> None:
        self._make_escrow(at=100.0)
        self.assertEqual(self.verifier.pending_escrows(), 1)
        resolved = self.verifier.resolve_escrows(
            BASE + timedelta(seconds=100.0 + 900.0)
        )
        self.assertEqual(resolved, [])
        self.assertEqual(self.verifier.pending_escrows(), 1)

    def test_escrow_expires_after_ttl(self) -> None:
        self._make_escrow(at=100.0)
        resolved = self.verifier.resolve_escrows(
            BASE + timedelta(seconds=100.0 + 901.0)
        )
        self.assertEqual(len(resolved), 1)
        self.assertEqual(resolved[0].state, ts.QUARANTINE)
        self.assertEqual(resolved[0].reason, ts.REASON_ESCROW_EXPIRED)
        self.assertEqual(self.verifier.pending_escrows(), 0)

    def test_later_radar_corroborates(self) -> None:
        self._make_escrow(at=100.0)
        self.verifier.evaluate(
            reading(
                "radar",
                at=160.0,
                depth_mm=9.0,
                radar_mm=1.0,
            )
        )
        resolved = self.verifier.resolve_escrows(
            BASE + timedelta(seconds=100.0 + 901.0)
        )
        self.assertEqual(len(resolved), 1)
        self.assertEqual(resolved[0].state, ts.ACCEPT)
        self.assertEqual(resolved[0].reason, ts.REASON_ESCROW_CORROBORATED)

    def test_radar_at_exactly_dry_threshold_does_not_corroborate(self) -> None:
        self._make_escrow(at=100.0)
        self.verifier.evaluate(
            reading(
                "radar",
                at=160.0,
                depth_mm=9.0,
                radar_mm=0.5,
            )
        )
        resolved = self.verifier.resolve_escrows(
            BASE + timedelta(seconds=100.0 + 901.0)
        )
        self.assertEqual(resolved[0].reason, ts.REASON_ESCROW_EXPIRED)

    def test_radar_must_be_strictly_later(self) -> None:
        self._make_escrow(at=100.0)
        self.verifier.evaluate(
            reading(
                "radar",
                at=100.0,
                depth_mm=9.0,
                radar_mm=1.0,
            )
        )
        resolved = self.verifier.resolve_escrows(
            BASE + timedelta(seconds=100.0 + 901.0)
        )
        self.assertEqual(resolved[0].reason, ts.REASON_ESCROW_EXPIRED)

    def test_neighbor_cells_corroborate(self) -> None:
        self._make_escrow(at=100.0)
        for cell in ("n1", "n2"):
            for index in range(3):
                self.verifier.evaluate(
                    reading(
                        f"{cell}-d{index}",
                        at=160.0,
                        depth_mm=3.0,
                        radar_mm=0.0,
                        cell=cell,
                    )
                )
        self.verifier.set_neighbors("cell-a", ["n1", "n2"])
        resolved = self.verifier.resolve_escrows(
            BASE + timedelta(seconds=160.0)
        )
        self.assertEqual(len(resolved), 1)
        self.assertEqual(resolved[0].reason, ts.REASON_ESCROW_CORROBORATED)

    def test_one_neighbor_cell_is_not_enough(self) -> None:
        self._make_escrow(at=100.0)
        for index in range(3):
            self.verifier.evaluate(
                reading(
                    f"n1-d{index}",
                    at=160.0,
                    depth_mm=3.0,
                    radar_mm=0.0,
                    cell="n1",
                )
            )
        self.verifier.set_neighbors("cell-a", ["n1"])
        self.assertEqual(
            self.verifier.resolve_escrows(BASE + timedelta(seconds=160.0)),
            [],
        )
        resolved = self.verifier.resolve_escrows(
            BASE + timedelta(seconds=100.0 + 901.0)
        )
        self.assertEqual(resolved[0].reason, ts.REASON_ESCROW_EXPIRED)

    def test_neighbor_relation_is_directed(self) -> None:
        self._make_escrow(at=100.0)
        for cell in ("n1", "n2"):
            for index in range(3):
                self.verifier.evaluate(
                    reading(
                        f"{cell}-d{index}",
                        at=160.0,
                        depth_mm=3.0,
                        radar_mm=0.0,
                        cell=cell,
                    )
                )
        self.verifier.set_neighbors("n1", ["cell-a", "n2"])
        self.assertEqual(
            self.verifier.resolve_escrows(BASE + timedelta(seconds=160.0)),
            [],
        )

    def test_set_neighbors_drops_self_empty_and_duplicates(self) -> None:
        self.verifier.set_neighbors("a", ["b", "b", "", "a", "c"])
        self.assertEqual(self.verifier._neighbors["a"], ["b", "c"])
        self.verifier.set_neighbors("a", [])
        self.assertNotIn("a", self.verifier._neighbors)

    def test_each_escrow_returned_once(self) -> None:
        self._make_escrow(at=100.0)
        first = self.verifier.resolve_escrows(
            BASE + timedelta(seconds=100.0 + 901.0)
        )
        second = self.verifier.resolve_escrows(
            BASE + timedelta(seconds=100.0 + 902.0)
        )
        self.assertEqual(len(first), 1)
        self.assertEqual(second, [])

    def test_future_escrow_is_kept(self) -> None:
        self._make_escrow(at=100.0)
        self.assertEqual(
            self.verifier.resolve_escrows(BASE + timedelta(seconds=50.0)),
            [],
        )
        self.assertEqual(self.verifier.pending_escrows(), 1)


class TestTimestampHelpers(unittest.TestCase):
    def test_ns_round_trip(self) -> None:
        value = BASE + timedelta(seconds=12.345678)
        self.assertEqual(ts.from_ns(ts.to_ns(value)), value)

    def test_naive_timestamp_rejected(self) -> None:
        with self.assertRaises(ValueError):
            ts.to_ns(datetime(2024, 1, 1))


if __name__ == "__main__":
    unittest.main()
