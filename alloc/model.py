"""Minimum-cost-flow water allocation prototype.

Inputs are ACCEPT readings with verified stored volume (integer litres) and
demand requests. The model conserves volume, respects capacities, and uses an
explicit priority rule: each demand has priority 1 (highest) to P; flow to
demand d uses edge weight = transfer_cost + PRIORITY_WEIGHT * (priority_d - 1),
so equal transfer costs satisfy higher-priority demand first under min-cost flow.

This is a prototype for allocation logic only. It does not claim real-world
water savings or flood reduction.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Dict, Iterable, List, Tuple

import networkx as nx

PRIORITY_WEIGHT = 10_000  # dominates typical distance costs


@dataclass(frozen=True)
class Supply:
    node_id: str
    verified_litres: int  # from ACCEPT reading/stored volume
    accepted: bool = True

    def __post_init__(self) -> None:
        if self.verified_litres < 0:
            raise ValueError("verified_litres must be >= 0")
        if not isinstance(self.verified_litres, int):
            raise TypeError("verified_litres must be integer litres")


@dataclass(frozen=True)
class Demand:
    node_id: str
    requested_litres: int
    priority: int = 1  # 1 = highest

    def __post_init__(self) -> None:
        if self.requested_litres < 0:
            raise ValueError("requested_litres must be >= 0")
        if self.priority < 1:
            raise ValueError("priority must be >= 1")
        if not isinstance(self.requested_litres, int) or not isinstance(self.priority, int):
            raise TypeError("litres and priority must be integers")


@dataclass(frozen=True)
class Transfer:
    supply_id: str
    demand_id: str
    capacity_litres: int
    cost: int  # distance/transfer cost units

    def __post_init__(self) -> None:
        if self.capacity_litres < 0 or self.cost < 0:
            raise ValueError("capacity and cost must be >= 0")
        if not isinstance(self.capacity_litres, int) or not isinstance(self.cost, int):
            raise TypeError("capacity and cost must be integers")


@dataclass
class AllocationResult:
    flows: Dict[Tuple[str, str], int]
    supplied: Dict[str, int]
    delivered: Dict[str, int]
    total_supplied: int
    total_delivered: int
    feasible: bool


def allocate(supplies: Iterable[Supply], demands: Iterable[Demand], transfers: Iterable[Transfer]) -> AllocationResult:
    supplies = list(supplies)
    demands = list(demands)
    transfers = list(transfers)

    accepted = [s for s in supplies if s.accepted]
    total_supply = sum(s.verified_litres for s in accepted)
    total_demand = sum(d.requested_litres for d in demands)
    total_flow = min(total_supply, total_demand)

    g = nx.DiGraph()
    src, sink = "__source__", "__sink__"
    g.add_node(src, demand=-total_flow)
    g.add_node(sink, demand=total_flow)

    for s in accepted:
        g.add_edge(src, s.node_id, capacity=s.verified_litres, weight=0)
    for d in demands:
        g.add_edge(d.node_id, sink, capacity=d.requested_litres, weight=0)

    accepted_ids = {s.node_id for s in accepted}
    demand_ids = {d.node_id: d for d in demands}
    for t in transfers:
        if t.supply_id not in accepted_ids or t.demand_id not in demand_ids:
            continue
        d = demand_ids[t.demand_id]
        w = t.cost + PRIORITY_WEIGHT * (d.priority - 1)
        g.add_edge(t.supply_id, t.demand_id, capacity=t.capacity_litres, weight=w)

    feasible = True
    try:
        flow_dict = nx.min_cost_flow(g)
    except (nx.NetworkXUnfeasible, nx.NetworkXUnbounded, nx.NetworkXError):
        feasible = False
        flow_dict = {u: {v: 0 for v in g[u]} for u in g}

    flows: Dict[Tuple[str, str], int] = {}
    supplied = {s.node_id: 0 for s in supplies}
    delivered = {d.node_id: 0 for d in demands}
    for t in transfers:
        f = int(flow_dict.get(t.supply_id, {}).get(t.demand_id, 0))
        flows[(t.supply_id, t.demand_id)] = f
        if t.supply_id in supplied:
            supplied[t.supply_id] += f
        if t.demand_id in delivered:
            delivered[t.demand_id] += f

    total_supplied = sum(supplied.values())
    total_delivered = sum(delivered.values())
    return AllocationResult(
        flows=flows, supplied=supplied, delivered=delivered,
        total_supplied=total_supplied, total_delivered=total_delivered, feasible=feasible,
    )


def six_node_worked_example() -> Tuple[List[Supply], List[Demand], List[Transfer], AllocationResult]:
    """Six domain nodes: S1,S2 (supplies) + D1,D2,D3 (demands) + D4? No.
    Total 6 nodes = 2 supply buildings + 4 demand locations = 6, or simpler:
    S1,S2,S3 supplies? Better explicit: 3 supplies and 3 demands = 6 nodes.
    """
    supplies = [
        Supply("S1", 50, accepted=True),   # ACCEPT, 50 L
        Supply("S2", 30, accepted=True),   # ACCEPT, 30 L
        Supply("S3", 20, accepted=False),  # not ACCEPT - excluded
    ]
    demands = [
        Demand("D1", 40, priority=1),  # highest priority
        Demand("D2", 35, priority=2),
        Demand("D3", 25, priority=2),
    ]
    transfers = [
        Transfer("S1", "D1", capacity_litres=40, cost=1),
        Transfer("S1", "D2", capacity_litres=20, cost=2),
        Transfer("S2", "D1", capacity_litres=10, cost=5),
        Transfer("S2", "D2", capacity_litres=30, cost=1),
        Transfer("S2", "D3", capacity_litres=30, cost=2),
        Transfer("S3", "D3", capacity_litres=20, cost=1),  # from non-ACCEPT, ignored
    ]
    result = allocate(supplies, demands, transfers)
    return supplies, demands, transfers, result
