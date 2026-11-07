"""Offline tests for Phase 2 simulation modules.

These tests are deterministic and do not access the network or require Go.
The Go/Python consistency check is covered by sim.consistency (and run by
run_all.sh when Go is available); the unit test for it is kept lightweight.
"""

from __future__ import annotations

import csv
import json
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path

from sim import adversarial, rainfall_replay, rainfall_retrieval, three_state_eval
from sim import three_state as ts


class TestThreeStateEval(unittest.TestCase):
    def test_generates_all_scenarios(self):
        for sc in three_state_eval.SCENARIOS:
            events, neighbors = three_state_eval._generate_scenario(sc["key"], seed=1, n=200)
            self.assertTrue(events, sc["key"])
            self.assertIsInstance(neighbors, dict)

    def test_evaluate_reports_escrow_treatments(self):
        events, neighbors = three_state_eval._generate_scenario("radar_conflict_expires", 1, 200)
        counts = three_state_eval.evaluate(events, neighbors)
        self.assertEqual(set(counts), {n for n, _ in three_state_eval.ESCROW_TREATMENTS})
        # Expiring fabricated escrows should produce quarantines/resolutions.
        resolved = counts["resolved"]
        self.assertGreater(resolved.escrow, 0)
        self.assertGreater(resolved.total, 0)

    def test_run_writes_outputs(self):
        with tempfile.TemporaryDirectory() as td:
            out = Path(td)
            three_state_eval.run(out, events_per_seed=120, seeds=(1,))
            self.assertTrue((out / "three_state_results.csv").exists())
            self.assertTrue((out / "THREE_STATE_EVAL.md").exists())
            with (out / "three_state_results.csv").open(newline="", encoding="utf-8") as f:
                rows = list(csv.DictReader(f))
            self.assertGreater(len(rows), 0)
            self.assertIn("escrow_treatment", rows[0])


class TestAdversarial(unittest.TestCase):
    def test_precise_case_counts_attacks(self):
        c = adversarial._run_case_precise(
            seed=1, attacker_fraction=0.5, neighbor_compromised=2,
            peer_density=10, radar_lag_key="no_radar", trials=5,
        )
        self.assertEqual(c.attacks, 25)  # 5 attackers * 5 trials
        self.assertGreaterEqual(c.accepted + c.quarantined, c.attacks)

    def test_factor_grid_coverage(self):
        # Smoke: ensure module exposes required factor ranges.
        self.assertEqual(adversarial.ATTACKER_FRACTIONS[0], 0.10)
        self.assertEqual(adversarial.ATTACKER_FRACTIONS[-1], 0.80)
        self.assertEqual(adversarial.NEIGHBOR_COMPROMISED_COUNTS, (0, 1, 2, 3, 4, 5, 6))
        self.assertTrue(any(x["key"] == "lag_after_ttl" for x in adversarial.RADAR_LAG_SCENARIOS))


class TestRainfallRetrieval(unittest.TestCase):
    def test_build_url_has_required_params(self):
        url = rainfall_retrieval.build_request_url()
        self.assertIn("archive-api.open-meteo.com/v1/archive", url)
        self.assertIn("latitude=17.5449", url)
        self.assertIn("longitude=78.3864", url)
        self.assertIn("start_date=2025-07-01", url)
        self.assertIn("end_date=2025-09-30", url)
        self.assertIn("timezone=Asia%2FKolkata", url)
        self.assertIn("hourly=rain%2Cprecipitation", url)

    def test_missing_interval_detection(self):
        missing = rainfall_retrieval._find_missing_intervals([
            "2025-07-01T00:00", "2025-07-01T01:00", "2025-07-01T03:00",
        ])
        self.assertTrue(any("gap" in m for m in missing))

    def test_tidy_csv_from_payload(self):
        payload = {
            "latitude": 17.5449, "longitude": 78.3864, "elevation": 604.0,
            "timezone": "Asia/Kolkata", "utc_offset_seconds": 19800,
            "hourly_units": {"time": "iso8601", "rain": "mm", "precipitation": "mm"},
            "hourly": {"time": ["2025-07-01T00:00"], "rain": [1.2], "precipitation": [1.2]},
        }
        header, rows = rainfall_retrieval._tidy_csv_rows(payload)
        self.assertIn("time", header)
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0][0], "2025-07-01T00:00")

    def test_offline_cache_processing(self):
        payload = {
            "latitude": 17.5449, "longitude": 78.3864, "elevation": 604.0,
            "timezone": "Asia/Kolkata", "utc_offset_seconds": 19800,
            "hourly_units": {"time": "iso8601", "rain": "mm", "precipitation": "mm"},
            "hourly": {
                "time": [f"2025-07-01T{i:02d}:00" for i in range(24)],
                "rain": [0.0] * 24,
                "precipitation": [0.0] * 24,
            },
        }
        with tempfile.TemporaryDirectory() as td:
            out = Path(td)
            raw = out / "open_meteo_archive_raw.json"
            raw.write_text(json.dumps(payload), encoding="utf-8")
            result = rainfall_retrieval.retrieve(out, allow_cached=True, force=False)
            self.assertTrue(result.csv_path.exists())
            self.assertTrue(result.metadata_path.exists())
            self.assertEqual(result.row_count, 24)
            self.assertTrue(result.missing_intervals)  # count mismatch expected


class TestRainfallReplay(unittest.TestCase):
    def test_synthetic_fallback_labeled(self):
        hourly, source = rainfall_replay.load_hourly_rain(Path("nonexistent_archive.csv"))
        self.assertEqual(source, "synthetic_fallback")
        self.assertGreater(len(hourly), 0)
        self.assertIn("rain_mm", hourly[0])

    def test_replay_is_seeded(self):
        hourly = [{"time": "2025-07-01T00:00", "rain_mm": 5.0}] * 24
        cfg = rainfall_replay.ReplayConfig(seed=42, cells=2, devices_per_cell=3)
        a = rainfall_replay.generate_replay(hourly, cfg)
        b = rainfall_replay.generate_replay(hourly, cfg)
        self.assertEqual([e["claimed_mm"] for e in a], [e["claimed_mm"] for e in b])
        self.assertTrue(all(e["label"] == rainfall_replay.LABEL for e in a))

    def test_run_writes_outputs_from_csv(self):
        with tempfile.TemporaryDirectory() as td:
            td = Path(td)
            archive = td / "archive.csv"
            with archive.open("w", newline="", encoding="utf-8") as f:
                w = csv.DictWriter(f, fieldnames=["time", "rain_mm"])
                w.writeheader()
                for i in range(24):
                    w.writerow({"time": f"2025-07-01T{i:02d}:00", "rain_mm": 1.0})
            out = td / "out"
            rainfall_replay.run(out, archive, seeds=(1,), cells=2, devices_per_cell=2)
            self.assertTrue((out / "REPLAY.md").exists())
            self.assertTrue((out / "replay_results.csv").exists())
            md = (out / "REPLAY.md").read_text(encoding="utf-8")
            self.assertIn("gridded weather-context replay", md)
            self.assertIn("not rooftop ground truth", md)


class TestConsistencyFixtureBuilder(unittest.TestCase):
    def test_fixture_has_steps_and_neighbors(self):
        req = __import__("sim.consistency", fromlist=["build_fixture_request"]).build_fixture_request()
        self.assertIn("steps", req)
        self.assertIn("neighbors", req)
        self.assertGreater(len(req["steps"]), 10)

    def test_python_replay_runs(self):
        mod = __import__("sim.consistency", fromlist=["build_fixture_request", "run_python"])
        req = mod.build_fixture_request()
        result = mod.run_python(req)
        self.assertEqual(len(result["decisions"]), sum(1 for s in req["steps"] if s["type"] == "reading"))
        self.assertGreater(len(result["resolutions"]), 0)


if __name__ == "__main__":
    unittest.main()
