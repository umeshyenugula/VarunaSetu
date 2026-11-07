import random
import unittest

from alloc.model import (
    AllocationResult,
    Demand,
    Supply,
    Transfer,
    allocate,
    six_node_worked_example,
)


class AllocTest(unittest.TestCase):
    def assert_invariants(self, supplies, demands, transfers, r: AllocationResult):
        accepted_ids = {s.node_id for s in supplies if s.accepted}
        demand_ids = {d.node_id for d in demands}
        supply_ids = {s.node_id for s in supplies}
        # use only ACCEPT readings: non-ACCEPT supplies contribute zero flow
        for (s, d), f in r.flows.items():
            self.assertIn(s, supply_ids)
            self.assertIn(d, demand_ids)
            self.assertGreaterEqual(f, 0)
            self.assertIsInstance(f, int)
            if s not in accepted_ids:
                self.assertEqual(f, 0)
        # conservation
        self.assertEqual(r.total_supplied, sum(r.supplied.values()))
        self.assertEqual(r.total_delivered, sum(r.delivered.values()))
        self.assertEqual(r.total_supplied, r.total_delivered)
        # supply limitations / capacities
        supply_by_id = {s.node_id: s for s in supplies}
        for sid, sent in r.supplied.items():
            s = supply_by_id[sid]
            if not s.accepted:
                self.assertEqual(sent, 0)
            self.assertLessEqual(sent, s.verified_litres)
        # demand fulfillment bounds
        demand_by_id = {d.node_id: d for d in demands}
        for did, got in r.delivered.items():
            self.assertLessEqual(got, demand_by_id[did].requested_litres)
        # edge capacities
        cap = {(t.supply_id, t.demand_id): t.capacity_litres for t in transfers}
        for k, f in r.flows.items():
            self.assertLessEqual(f, cap.get(k, 0))
        # allocation validity and global bounds
        total_accept = sum(s.verified_litres for s in supplies if s.accepted)
        total_req = sum(d.requested_litres for d in demands)
        self.assertLessEqual(r.total_delivered, min(total_accept, total_req))
        self.assertTrue(r.feasible in (True, False))

    def test_six_node_worked_example(self):
        supplies, demands, transfers, r = six_node_worked_example()
        # 6 domain nodes total
        self.assertEqual(len(supplies) + len(demands), 6)
        self.assert_invariants(supplies, demands, transfers, r)
        # non-ACCEPT supply excluded
        self.assertEqual(r.supplied["S3"], 0)
        self.assertEqual(r.flows[("S3", "D3")], 0)
        # totals: 80 accepted supply, 100 demand, network allows 80 delivered
        self.assertEqual(r.total_delivered, 80)
        # priority 1 demand D1 should be fully satisfied given available feasible flow
        self.assertEqual(r.delivered["D1"], 40)
        # conservation and integer litres
        for f in r.flows.values():
            self.assertIsInstance(f, int)

    def test_only_accept_readings(self):
        supplies = [Supply("S1", 10, accepted=False), Supply("S2", 10, accepted=True)]
        demands = [Demand("D1", 15, priority=1)]
        transfers = [Transfer("S1", "D1", 10, 1), Transfer("S2", "D1", 10, 1)]
        r = allocate(supplies, demands, transfers)
        self.assertEqual(r.supplied["S1"], 0)
        self.assertEqual(r.total_delivered, 10)
        self.assert_invariants(supplies, demands, transfers, r)

    def test_property_randomized(self):
        rng = random.Random(42)
        for _ in range(50):
            ns = rng.randint(1, 5)
            nd = rng.randint(1, 5)
            supplies = [Supply(f"S{i}", rng.randint(0, 30), accepted=rng.random() > 0.2) for i in range(ns)]
            demands = [Demand(f"D{j}", rng.randint(0, 30), priority=rng.randint(1, 3)) for j in range(nd)]
            transfers = []
            for s in supplies:
                for d in demands:
                    if rng.random() > 0.3:
                        transfers.append(Transfer(s.node_id, d.node_id, rng.randint(0, 25), rng.randint(0, 9)))
            r = allocate(supplies, demands, transfers)
            self.assert_invariants(supplies, demands, transfers, r)


if __name__ == "__main__":
    unittest.main()
