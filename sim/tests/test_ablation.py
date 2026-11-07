"""Table-driven and end-to-end tests for the Phase 0B simulation.

Every test is offline and uses a small event count where generation is
required. The full 100,000 x 10 x 7 run is exercised by ``run_all.sh``;
these tests verify the contract, determinism, metrics and method boundaries.
"""

from __future__ import annotations

import argparse
import csv
import os
import tempfile
import unittest

from sim import ablation_experiment as ae
from sim import config


def event(
    r_obs: float,
    radar_mm: float = 100.0,
    fraudulent: bool = False,
    t: float = 0.0,
    cell: int = 0,
    device: int = 0,
    area_m2: float = 100.0,
    runoff: float = 1.0,
    stored_litres: float = 0.0,
) -> ae.Event:
    """Build one reading whose effective depth is exactly r_obs."""

    return ae.Event(
        t=t,
        cell=cell,
        device=device,
        true_mm=0.0,
        radar_mm=radar_mm,
        claimed_litres=r_obs * area_m2 * runoff,
        area_m2=area_m2,
        runoff=runoff,
        stored_litres=stored_litres,
        fraudulent=fraudulent,
    )


class TestConfigContract(unittest.TestCase):
    def test_phase_0b_size(self) -> None:
        self.assertEqual(config.EVENTS_PER_SEED, 100_000)
        self.assertEqual(len(config.SEEDS), 10)
        self.assertEqual(len(set(config.SEEDS)), 10)
        self.assertEqual(len(config.SCENARIOS), 7)
        self.assertEqual(
            config.METHOD_ORDER, ("static", "physics", "peer", "hybrid")
        )

    def test_go_defaults_are_mirrored(self) -> None:
        self.assertEqual(config.ALPHA, 1.15)
        self.assertEqual(config.BETA_MM, 2.0)
        self.assertEqual(config.MIN_PEERS, 4)
        self.assertEqual(config.PEER_MAX_AGE_S, 600)
        self.assertEqual(config.Z_SCALE, 0.6745)
        self.assertEqual(config.MAD_FLOOR_MM, 0.15)
        self.assertEqual(config.OUTLIER_Z, 3.5)
        self.assertEqual(config.PEER_RAIN_MM, 2.0)
        self.assertEqual(config.COHERENCE_ABS_MM, 1.0)
        self.assertEqual(config.COHERENCE_REL_FRAC, 0.25)
        self.assertEqual(config.RADAR_DRY_MM, 0.5)
        self.assertGreater(config.STATIC_THRESHOLD_MM, 0)

    def test_scenario_keys_are_unique_and_complete(self) -> None:
        keys = [scenario.key for scenario in config.SCENARIOS]
        self.assertEqual(len(keys), len(set(keys)))
        self.assertEqual(
            keys,
            [
                "normal_rainfall",
                "microburst",
                "isolated_spike",
                "dry_weather_spoofing",
                "radar_bounded_spoofing",
                "coordinated_40",
                "coordinated_60",
            ],
        )
        for scenario in config.SCENARIOS:
            self.assertGreaterEqual(scenario.fraud_fraction, 0.0)
            self.assertLessEqual(scenario.fraud_fraction, 1.0)


class TestGeneration(unittest.TestCase):
    def test_deterministic_for_same_seed(self) -> None:
        scenario = config.SCENARIOS[0]
        first = ae.generate_events(scenario, 7, 400)
        second = ae.generate_events(scenario, 7, 400)
        self.assertEqual(first, second)

    def test_different_seeds_differ(self) -> None:
        scenario = config.SCENARIOS[0]
        first = ae.generate_events(scenario, 1, 100)
        second = ae.generate_events(scenario, 2, 100)
        self.assertNotEqual(first, second)

    def test_event_count_order_and_validity(self) -> None:
        for scenario in config.SCENARIOS:
            with self.subTest(scenario=scenario.key):
                events = ae.generate_events(scenario, 3, 300)
                self.assertEqual(len(events), 300)
                self.assertEqual(
                    [e.t for e in events], sorted(e.t for e in events)
                )
                for e in events:
                    self.assertGreaterEqual(e.claimed_litres, 0.0)
                    self.assertGreaterEqual(e.radar_mm, 0.0)
                    self.assertGreaterEqual(e.stored_litres, 0.0)
                    self.assertLessEqual(
                        e.claimed_litres + e.stored_litres,
                        config.TANK_CAPACITY_L + 1e-9,
                    )
                    self.assertGreaterEqual(e.area_m2, config.ROOF_AREA_M2_RANGE[0])
                    self.assertLessEqual(e.area_m2, config.ROOF_AREA_M2_RANGE[1])
                    self.assertGreaterEqual(e.runoff, config.RUNOFF_COEFF_RANGE[0])
                    self.assertLessEqual(e.runoff, config.RUNOFF_COEFF_RANGE[1])

    def test_fraud_proportions_for_random_scenarios(self) -> None:
        for scenario in config.SCENARIOS:
            if scenario.mode == "coordinated":
                continue
            with self.subTest(scenario=scenario.key):
                events = ae.generate_events(scenario, 5, 500)
                fraud = sum(e.fraudulent for e in events)
                self.assertEqual(fraud, round(scenario.fraud_fraction * 500))

    def test_coordinated_scenarios_have_both_labels(self) -> None:
        for scenario in config.SCENARIOS[-2:]:
            with self.subTest(scenario=scenario.key):
                events = ae.generate_events(scenario, 5, 2_000)
                fraud = sum(e.fraudulent for e in events)
                self.assertGreater(fraud, 0)
                self.assertLess(fraud, len(events))
                share = fraud / len(events)
                self.assertGreater(share, 0.30)
                self.assertLess(share, 0.70)

    def test_rejects_bad_event_count(self) -> None:
        with self.assertRaises(ValueError):
            ae.generate_events(config.SCENARIOS[0], 1, 0)


class TestMetrics(unittest.TestCase):
    def test_confusion_metrics(self) -> None:
        metrics = ae.metrics_from_confusion(
            ae.Confusion(tn=8, fp=2, fn=2, tp=8)
        )
        self.assertAlmostEqual(metrics.precision, 0.8)
        self.assertAlmostEqual(metrics.recall, 0.8)
        self.assertAlmostEqual(metrics.f1, 0.8)
        self.assertAlmostEqual(metrics.false_positive_rate, 0.2)

    def test_zero_denominators_are_zero(self) -> None:
        metrics = ae.metrics_from_confusion(ae.Confusion())
        self.assertEqual(
            (metrics.precision, metrics.recall, metrics.f1, metrics.false_positive_rate),
            (0.0, 0.0, 0.0, 0.0),
        )

    def test_perfect_and_inverted_classifiers(self) -> None:
        perfect = ae.metrics_from_confusion(ae.Confusion(tn=5, tp=5))
        self.assertEqual((perfect.precision, perfect.recall, perfect.f1), (1.0, 1.0, 1.0))
        self.assertEqual(perfect.false_positive_rate, 0.0)

        inverted = ae.metrics_from_confusion(ae.Confusion(fp=5, fn=5))
        self.assertEqual((inverted.precision, inverted.recall, inverted.f1), (0.0, 0.0, 0.0))
        self.assertEqual(inverted.false_positive_rate, 1.0)

    def test_mean_and_sample_std(self) -> None:
        mean, std = ae.mean_std([1.0, 2.0, 3.0])
        self.assertAlmostEqual(mean, 2.0)
        self.assertAlmostEqual(std, 1.0)
        self.assertEqual(ae.mean_std([]), (0.0, 0.0))
        self.assertEqual(ae.mean_std([4.0]), (4.0, 0.0))

    def test_confusion_merge(self) -> None:
        total = ae.Confusion(tn=1, fp=2, fn=3, tp=4)
        total.merge(ae.Confusion(tn=10, fp=20, fn=30, tp=40))
        self.assertEqual(
            (total.tn, total.fp, total.fn, total.tp), (11, 22, 33, 44)
        )
        self.assertEqual(total.events, 110)
        self.assertEqual(total.positives, 77)
        self.assertEqual(total.negatives, 33)


class TestMethods(unittest.TestCase):
    def test_static_threshold(self) -> None:
        self.assertFalse(ae.classify(10.0, 100.0, []).static)
        self.assertTrue(ae.classify(10.001, 100.0, []).static)

    def test_physics_boundary(self) -> None:
        # radar 0 -> R_max = 2.0 exactly.
        self.assertFalse(ae.classify(2.0, 0.0, []).physics)
        self.assertTrue(ae.classify(2.000001, 0.0, []).physics)
        # alpha * radar + beta
        self.assertFalse(ae.classify(7.75, 5.0, []).physics)
        self.assertTrue(ae.classify(7.750001, 5.0, []).physics)

    def test_peer_requires_four_peers(self) -> None:
        peers_three = [4.0, 4.0, 4.0]
        peers_four = [4.0, 4.0, 4.0, 4.0]
        self.assertFalse(ae.classify(10.0, 100.0, peers_three).peer)
        self.assertTrue(ae.classify(10.0, 100.0, peers_four).peer)

    def test_peer_negative_deviation_is_not_an_outlier(self) -> None:
        peers = [8.0, 8.25, 7.75, 8.0]
        self.assertFalse(ae.classify(1.0, 100.0, peers).peer)

    def test_peer_positive_outlier_and_z_boundary(self) -> None:
        # Peers at 0 give M=0, MAD=0, so Z = 0.6745 * claim / 0.15.
        edge = config.OUTLIER_Z * (config.MAD_FLOOR_MM) / config.Z_SCALE
        peers = [0.0, 0.0, 0.0, 0.0]
        self.assertFalse(ae.classify(edge - 0.001, 100.0, peers).peer)
        self.assertTrue(ae.classify(edge + 0.001, 100.0, peers).peer)

    def test_hybrid_escrow_conflict_is_rejected(self) -> None:
        peers = [8.0, 8.25, 7.75, 8.0]
        decision = ae.classify(8.0, 0.0, peers)
        self.assertFalse(decision.peer)  # not an outlier relative to peers
        self.assertTrue(decision.hybrid)  # dry radar + rainy coherent peers

    def test_hybrid_insufficient_peers_uses_radar_bound(self) -> None:
        self.assertFalse(ae.classify(2.0, 0.0, []).hybrid)
        self.assertTrue(ae.classify(2.000001, 0.0, []).hybrid)

    def test_hybrid_uses_radar_bound_when_peers_are_coherent(self) -> None:
        peers = [8.0, 8.25, 7.75, 8.0]
        # Radar wet enough that the conflict rule does not fire; the claim is
        # above R_max = 1.15*3 + 2 = 5.45.
        decision = ae.classify(8.0, 3.0, peers)
        self.assertFalse(decision.peer)
        self.assertTrue(decision.physics)
        self.assertTrue(decision.hybrid)

    def test_known_peer_majority_limitation(self) -> None:
        # 6 compromised devices report 9 mm while 4 honest devices report
        # 0.1 mm. Peer-only accepts the coherent majority's fabrication.
        events = []
        for device in range(10):
            fraudulent = device < 6
            depth = 9.0 if fraudulent else 0.1
            events.append(event(depth, radar_mm=0.0, fraudulent=fraudulent, device=device))
        results = ae.evaluate_events(events)
        self.assertEqual(results["peer"].tp, 0)
        self.assertEqual(results["peer"].fn, 6)
        # The radar bound still rejects the six over-claims in hybrid, but the
        # first honest reading after the compromised majority is also held as
        # a dry-radar/rainy-peer conflict. That single false positive is the
        # documented escrow-majority limitation, not a test error.
        self.assertEqual(results["hybrid"].tp, 6)
        self.assertEqual(results["hybrid"].fp, 1)


class TestExperimentAndOutputs(unittest.TestCase):
    def test_scenario_subset(self) -> None:
        records = ae.run_experiment(
            events_per_seed=100,
            seeds=(1,),
            scenario_keys=("normal_rainfall",),
        )
        self.assertEqual(len(records), len(config.METHOD_ORDER))
        self.assertTrue(all(r.scenario == "normal_rainfall" for r in records))

    def test_unknown_scenario_rejected(self) -> None:
        with self.assertRaises(ValueError):
            ae.run_experiment(
                events_per_seed=10,
                seeds=(1,),
                scenario_keys=("does-not-exist",),
            )

    def test_empty_seed_list_rejected(self) -> None:
        with self.assertRaises(ValueError):
            ae.run_experiment(events_per_seed=10, seeds=())

    def test_end_to_end_outputs(self) -> None:
        seeds = (1, 2)
        events = 200
        records = ae.run_experiment(events_per_seed=events, seeds=seeds)
        self.assertEqual(
            len(records), len(config.SCENARIOS) * len(seeds) * len(config.METHOD_ORDER)
        )

        with tempfile.TemporaryDirectory() as output_dir:
            ae.write_results(records, output_dir, events, seeds, "test-command")

            for name in (
                "METHOD.md",
                "overall_results.csv",
                "scenario_results.csv",
                "confusion_matrices.csv",
            ):
                self.assertTrue(
                    os.path.exists(os.path.join(output_dir, name)), name
                )

            with open(
                os.path.join(output_dir, "overall_results.csv"),
                newline="",
                encoding="utf-8",
            ) as handle:
                rows = list(csv.reader(handle))
            self.assertEqual(len(rows) - 1, len(config.METHOD_ORDER))
            self.assertEqual(
                rows[0][:3], ["method", "seeds", "events"]
            )
            self.assertEqual(
                [row[0] for row in rows[1:]], list(config.METHOD_ORDER)
            )

            with open(
                os.path.join(output_dir, "scenario_results.csv"),
                newline="",
                encoding="utf-8",
            ) as handle:
                rows = list(csv.reader(handle))
            self.assertEqual(
                len(rows) - 1,
                len(config.METHOD_ORDER) * len(config.SCENARIOS),
            )

            with open(
                os.path.join(output_dir, "confusion_matrices.csv"),
                newline="",
                encoding="utf-8",
            ) as handle:
                rows = list(csv.reader(handle))
            expected = (
                len(config.SCENARIOS) * (len(seeds) + 1)
                + (len(seeds) + 1)
            ) * len(config.METHOD_ORDER)
            self.assertEqual(len(rows) - 1, expected)
            self.assertEqual(rows[0], ["scope", "scenario", "seed", "method", "tn", "fp", "fn", "tp"])

            with open(
                os.path.join(output_dir, "METHOD.md"), encoding="utf-8"
            ) as handle:
                method = handle.read()
            self.assertIn("synthetic simulation", method)
            self.assertIn("Known limitations", method)
            self.assertIn("Latest results — overall", method)
            self.assertIn("test-command", method)

    def test_cli_parsers(self) -> None:
        self.assertEqual(ae._parse_seeds("1, 2,3"), (1, 2, 3))
        self.assertEqual(ae._parse_scenarios(""), None)
        self.assertEqual(
            ae._parse_scenarios("normal_rainfall, microburst"),
            ("normal_rainfall", "microburst"),
        )
        with self.assertRaises(argparse.ArgumentTypeError):
            ae._parse_seeds("")
        with self.assertRaises(argparse.ArgumentTypeError):
            ae._parse_seeds("1,nope")


if __name__ == "__main__":
    unittest.main()
