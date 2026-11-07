# Water Allocation Prototype (Phase 4)

`alloc/` implements a **prototype** minimum-cost-flow water redistribution model
using Python and NetworkX. It does **not** claim actual real-world water savings
or flood reduction.

## Model

- **Supply nodes**: buildings with **ACCEPT** readings and verified stored
  volume (integer litres). Non-ACCEPT supplies are excluded (zero flow).
- **Demand nodes**: eligible locations requiring water, each with requested
  integer litres and an explicit **priority** (1 = highest).
- **Edges**: transfer capacity (integer litres) and distance/transfer cost
  (integer units). Only edges between ACCEPT supplies and known demands are used.
- **Objective**: minimum total cost flow from a super-source to a super-sink
  through supply/demand nodes.
- **Flow volume**: `total_flow = min(total_ACCEPT_supply, total_demand)`, limited
  by network edge capacities and supply/demand bounds.

## Fairness / priority rule (explicit)

For each transfer edge to demand *d* with priority *p_d* and transfer cost *c*:

`effective_weight = c + PRIORITY_WEIGHT * (p_d - 1)`, where
`PRIORITY_WEIGHT = 10_000` (dominates transfer costs in this prototype).

Thus, under equal/feasible transfer costs, higher-priority demands (lower
*p_d*) are satisfied before lower-priority demands by the min-cost objective.
Within the same priority, lower transfer cost is preferred.

## Constraints

- Uses only ACCEPT readings/supplies.
- Conserves total water volume: `total_supplied == total_delivered`.
- Respects storage/supply capacities and demand request bounds.
- Respects edge transfer capacities.
- Integer litres throughout (`Supply.verified_litres`, `Demand.requested_litres`,
  `Transfer.capacity_litres`, all flows).

## Six-node worked example

`alloc.model.six_node_worked_example()` defines 6 domain nodes: `S1,S2,S3`
(supplies; `S3` non-ACCEPT) and `D1,D2,D3` (demands; `D1` priority 1).
Accepted supply is 80 L, total demand 100 L; the feasible min-cost allocation
delivers 80 L, excludes `S3`, and fully satisfies high-priority `D1` (40 L)
subject to capacities/costs.

## Validation / property tests

`alloc/test_alloc.py` (unittest) validates the worked example and randomized
cases (`random.Random(42)`, 50 cases) for:

- Water conservation
- Capacity constraints (supply, demand, edge)
- Supply limitations / ACCEPT-only sourcing
- Demand fulfillment bounds
- Allocation validity (integer, non-negative, consistent totals)

Run: `python -m unittest discover -t . -s alloc -p 'test*.py' -v` (requires
`networkx`). Docker/WDAC-safe: `docker run --rm -v <repo>:/src -w /src python:3.14-slim sh -lc "pip install --no-cache-dir networkx && python -m unittest discover -t . -s alloc -p 'test*.py' -v"`.

## Data requirements

- Per-building ACCEPT decision and verified stored volume (litres).
- Demand locations, requested litres, and priority class.
- Transfer network capacities and cost/distance metrics.
- Consistent node identifiers and integer units.
