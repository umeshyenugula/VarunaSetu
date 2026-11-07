"""Cross-language consistency check: Python mirror versus the Go verifier.

The fixture builder creates a deterministic telemetry script that exercises
every reason code, the escrow lifecycle, radar corroboration, neighbour
corroboration and boundary cases. The same script is replayed through:

* ``sim.three_state.ThreeStateVerifier`` (Python), and
* ``sim/gocli`` (a small Go harness around the real verifier package).

The checker asserts that state, reason, diagnostics and escrow resolutions
match. It never modifies the Go implementation and never uses the network: the
Go harness is built with ``GOPROXY=off`` and imports only the local module.

If the Go toolchain is not installed, the CLI writes a clear SKIPPED report and
the unit test is skipped. A machine with Go should treat any mismatch as a
failure.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import shutil
import subprocess
import sys
from dataclasses import dataclass, field
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Sequence

from . import three_state as ts

BASE = datetime(2024, 1, 1, tzinfo=timezone.utc)
ZERO = datetime(1, 1, 1, tzinfo=timezone.utc)
FIXTURE_RELATIVE_PATH = "sim/fixtures/consistency_fixtures.json"


class ConsistencyUnavailable(RuntimeError):
    """Raised when the Go toolchain is not available."""


class ConsistencyError(RuntimeError):
    """Raised when the harness or a comparison fails."""


# ---------------------------------------------------------------------------
# Fixture construction
# ---------------------------------------------------------------------------


def _stamp(offset_s: float) -> str:
    return (BASE + timedelta(seconds=offset_s)).isoformat().replace("+00:00", "Z")


def build_fixture_request() -> dict[str, Any]:
    """Build the deterministic cross-language fixture request."""

    steps: list[dict[str, Any]] = []

    def reading(
        device: str,
        cell: str,
        at: float,
        depth_mm: float = 1.0,
        *,
        radar_mm: float = 10.0,
        area_m2: float = 100.0,
        runoff: float = 1.0,
        stored: float = 0.0,
        capacity: float = 100_000.0,
        timestamp: str | None = None,
    ) -> None:
        steps.append(
            {
                "type": "reading",
                "device_id": device,
                "timestamp": _stamp(at) if timestamp is None else timestamp,
                "claimed_litres": depth_mm * area_m2 * runoff,
                "roof_area_m2": area_m2,
                "runoff_coeff": runoff,
                "stored_litres": stored,
                "capacity_litres": capacity,
                "radar_mm": radar_mm,
                "cell_id": cell,
            }
        )

    def sweep(at: float) -> None:
        steps.append({"type": "sweep", "now": _stamp(at)})

    # Invalid input, including the Go zero timestamp.
    reading("", "c-invalid", 0.0)
    reading("d-zero", "c-invalid", 0.0, timestamp="")
    reading("d-negative", "c-invalid", 0.0, depth_mm=-1.0)

    # Capacity boundary: exactly zero free space is not an overflow.
    reading("cap-ok", "c-capacity", 0.0, depth_mm=0.0, stored=100_000.0)
    reading("cap-over", "c-capacity", 1.0, depth_mm=1.0, stored=100_000.0)

    # No peers: radar fallback accept and insufficient-peer review.
    reading("fallback-ok", "c-fallback", 0.0, depth_mm=1.0, radar_mm=0.0)
    reading("fallback-review", "c-fallback", 1.0, depth_mm=3.0, radar_mm=0.0)

    # Four peers: agreement.
    for index in range(4):
        reading(f"agree-peer-{index}", "c-agree", float(index), radar_mm=5.0)
    reading("agree-target", "c-agree", 4.0, depth_mm=1.1, radar_mm=5.0)

    # Four peers: positive outlier.
    for index in range(4):
        reading(f"outlier-peer-{index}", "c-outlier", float(index), radar_mm=5.0)
    reading("outlier-target", "c-outlier", 4.0, depth_mm=10.0, radar_mm=5.0)

    # Four peers: physical bound exceeded without being a positive outlier.
    for index, depth in enumerate((7.0, 7.5, 8.0, 8.5)):
        reading(f"bound-peer-{index}", "c-bound", float(index), depth_mm=depth, radar_mm=5.0)
    reading("bound-target", "c-bound", 4.0, depth_mm=8.0, radar_mm=5.0)

    # Escrow corroborated by a later radar reading above 0.5 mm.
    for index in range(4):
        reading(f"radar-peer-{index}", "c-escrow-radar", float(index), depth_mm=9.0, radar_mm=0.0)
    reading("radar-target", "c-escrow-radar", 4.0, depth_mm=9.0, radar_mm=0.0)
    reading("radar-later", "c-escrow-radar", 64.0, depth_mm=9.0, radar_mm=1.0)
    sweep(65.0)
    sweep(120.0)

    # Escrow expiry boundary: exactly TTL is still pending, one second later expires.
    for index in range(4):
        reading(f"expire-peer-{index}", "c-escrow-expire", float(index), depth_mm=9.0, radar_mm=0.0)
    reading("expire-target", "c-escrow-expire", 4.0, depth_mm=9.0, radar_mm=0.0)
    sweep(904.0)  # exactly 900 s after the escrowed reading
    sweep(905.001)

    # Escrow corroborated by two configured neighbour cells with three devices each.
    for index in range(4):
        reading(f"nb-peer-{index}", "c-neighbors", float(index), depth_mm=9.0, radar_mm=0.0)
    reading("nb-target", "c-neighbors", 4.0, depth_mm=9.0, radar_mm=0.0)
    for cell in ("n1", "n2"):
        for index in range(3):
            reading(f"{cell}-d{index}", cell, 64.0, depth_mm=3.0, radar_mm=0.0)
    sweep(64.0)

    # Three peers are not enough.
    for index in range(3):
        reading(f"three-peer-{index}", "c-three", float(index), radar_mm=0.0)
    reading("three-target", "c-three", 3.0, depth_mm=5.0, radar_mm=0.0)

    # Peer age boundary: exactly 600 s is eligible, one microsecond beyond is not.
    reading("age-old", "c-age-edge", 0.0, radar_mm=100.0)
    reading("age-target", "c-age-edge", 600.0, radar_mm=100.0)
    reading("age-old-2", "c-age-over", 0.0, radar_mm=100.0)
    reading("age-target-2", "c-age-over", 600.000001, radar_mm=100.0)

    # Exactly 0.5 mm radar is dry and a later 0.5 mm reading does not corroborate.
    for index in range(4):
        reading(f"dry-peer-{index}", "c-radar-half", float(index), depth_mm=9.0, radar_mm=0.5)
    reading("dry-target", "c-radar-half", 4.0, depth_mm=9.0, radar_mm=0.5)
    reading("dry-later", "c-radar-half", 64.0, depth_mm=9.0, radar_mm=0.5)
    sweep(905.001)

    return {
        "fixture_version": 1,
        "description": (
            "HydroLedger Phase 2 cross-language fixtures. One deterministic "
            "script exercising every verifier reason code, escrow creation, "
            "radar corroboration, neighbour corroboration and the documented "
            "boundaries. Synthetic telemetry only."
        ),
        "neighbors": {
            "c-neighbors": ["n1", "n2"],
        },
        "steps": steps,
    }


# ---------------------------------------------------------------------------
# Python replay
# ---------------------------------------------------------------------------


def _parse_time(value: str) -> datetime:
    if not value:
        return ZERO
    text = value[:-1] + "+00:00" if value.endswith("Z") else value
    parsed = datetime.fromisoformat(text)
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed


def _format_time(value: datetime) -> str:
    return value.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")


def run_python(request: dict[str, Any]) -> dict[str, Any]:
    verifier = ts.ThreeStateVerifier()
    for cell, neighbors in request.get("neighbors", {}).items():
        verifier.set_neighbors(cell, neighbors)

    decisions: list[dict[str, Any]] = []
    resolutions: list[dict[str, Any]] = []
    for index, step in enumerate(request["steps"]):
        if step["type"] == "reading":
            decision = verifier.evaluate(
                ts.Reading(
                    device_id=step["device_id"],
                    timestamp=_parse_time(step["timestamp"]),
                    claimed_litres=float(step["claimed_litres"]),
                    roof_area_m2=float(step["roof_area_m2"]),
                    runoff_coeff=float(step["runoff_coeff"]),
                    stored_litres=float(step["stored_litres"]),
                    capacity_litres=float(step["capacity_litres"]),
                    radar_mm=float(step["radar_mm"]),
                    cell_id=step["cell_id"],
                )
            )
            decisions.append(
                {
                    "step": index,
                    "device_id": step["device_id"],
                    "cell_id": step["cell_id"],
                    "state": decision.state,
                    "reason": decision.reason,
                    "r_obs_mm": decision.r_obs_mm,
                    "r_max_mm": decision.r_max_mm,
                    "peer_count": decision.peer_count,
                    "peer_median_mm": decision.peer_median_mm,
                    "peer_mad_mm": decision.peer_mad_mm,
                    "z_score": decision.z_score,
                }
            )
        elif step["type"] == "sweep":
            now = _parse_time(step["now"])
            resolved = verifier.resolve_escrows(now)
            resolutions.append(
                {
                    "step": index,
                    "now": _format_time(now),
                    "resolutions": [
                        {
                            "device_id": item.device_id,
                            "cell_id": item.cell_id,
                            "reading_timestamp": _format_time(item.reading_timestamp),
                            "resolved_at": _format_time(item.resolved_at),
                            "state": item.state,
                            "reason": item.reason,
                        }
                        for item in resolved
                    ],
                }
            )
        else:
            raise ConsistencyError(f"unknown step type at {index}: {step['type']}")
    return {"decisions": decisions, "resolutions": resolutions}


# ---------------------------------------------------------------------------
# Go replay
# ---------------------------------------------------------------------------


def repo_root() -> Path:
    return Path(__file__).resolve().parent.parent


def go_executable() -> str:
    found = shutil.which("go")
    if not found:
        raise ConsistencyUnavailable("go executable not found on PATH")
    return found


def run_go(
    request: dict[str, Any],
    *,
    root: Path | None = None,
    timeout: float = 180.0,
) -> dict[str, Any]:
    go = go_executable()
    root = root or repo_root()
    env = os.environ.copy()
    env["GOPROXY"] = "off"
    env["GOTOOLCHAIN"] = "local"
    env.pop("GOFLAGS", None)
    process = subprocess.run(
        [go, "run", "./sim/gocli"],
        input=json.dumps(request),
        text=True,
        capture_output=True,
        cwd=str(root),
        env=env,
        timeout=timeout,
    )
    if process.returncode != 0:
        raise ConsistencyError(
            "go harness failed "
            f"(exit {process.returncode}): {process.stderr.strip()}"
        )
    try:
        return json.loads(process.stdout)
    except json.JSONDecodeError as exc:
        raise ConsistencyError(f"go harness returned invalid JSON: {exc}") from exc


# ---------------------------------------------------------------------------
# Comparison
# ---------------------------------------------------------------------------


@dataclass
class ComparisonResult:
    decisions_compared: int = 0
    resolutions_compared: int = 0
    mismatches: list[str] = field(default_factory=list)

    @property
    def ok(self) -> bool:
        return not self.mismatches


def _float_equal(left: float, right: float) -> bool:
    return math.isclose(left, right, rel_tol=1e-9, abs_tol=1e-9)


def compare(python: dict[str, Any], go: dict[str, Any]) -> ComparisonResult:
    result = ComparisonResult()
    py_decisions = python["decisions"]
    go_decisions = go["decisions"]
    if len(py_decisions) != len(go_decisions):
        result.mismatches.append(
            f"decision count differs: python={len(py_decisions)} go={len(go_decisions)}"
        )
        return result
    for py_item, go_item in zip(py_decisions, go_decisions):
        result.decisions_compared += 1
        for key in ("step", "device_id", "cell_id", "state", "reason", "peer_count"):
            if py_item[key] != go_item[key]:
                result.mismatches.append(
                    f"decision step {py_item['step']} {key}: "
                    f"python={py_item[key]!r} go={go_item[key]!r}"
                )
        for key in (
            "r_obs_mm",
            "r_max_mm",
            "peer_median_mm",
            "peer_mad_mm",
            "z_score",
        ):
            if not _float_equal(float(py_item[key]), float(go_item[key])):
                result.mismatches.append(
                    f"decision step {py_item['step']} {key}: "
                    f"python={py_item[key]!r} go={go_item[key]!r}"
                )

    py_sweeps = python["resolutions"]
    go_sweeps = go["resolutions"]
    if len(py_sweeps) != len(go_sweeps):
        result.mismatches.append(
            f"sweep count differs: python={len(py_sweeps)} go={len(go_sweeps)}"
        )
        return result
    for py_sweep, go_sweep in zip(py_sweeps, go_sweeps):
        if py_sweep["step"] != go_sweep["step"]:
            result.mismatches.append(
                f"sweep step differs: python={py_sweep['step']} go={go_sweep['step']}"
            )
        if _parse_time(py_sweep["now"]) != _parse_time(go_sweep["now"]):
            result.mismatches.append(
                f"sweep now differs at step {py_sweep['step']}: "
                f"python={py_sweep['now']} go={go_sweep['now']}"
            )
        py_items = py_sweep["resolutions"]
        go_items = go_sweep["resolutions"]
        if len(py_items) != len(go_items):
            result.mismatches.append(
                f"sweep {py_sweep['step']} resolution count differs: "
                f"python={len(py_items)} go={len(go_items)}"
            )
            continue
        for py_item, go_item in zip(py_items, go_items):
            result.resolutions_compared += 1
            for key in ("device_id", "cell_id", "state", "reason"):
                if py_item[key] != go_item[key]:
                    result.mismatches.append(
                        f"sweep {py_sweep['step']} resolution {key}: "
                        f"python={py_item[key]!r} go={go_item[key]!r}"
                    )
            for key in ("reading_timestamp", "resolved_at"):
                if _parse_time(py_item[key]) != _parse_time(go_item[key]):
                    result.mismatches.append(
                        f"sweep {py_sweep['step']} resolution {key}: "
                        f"python={py_item[key]} go={go_item[key]}"
                    )
    return result


# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------


def _sha256(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def write_fixtures(path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(
        json.dumps(build_fixture_request(), indent=2) + "\n",
        encoding="utf-8",
        newline="\n",
    )


def _write_report(
    output_dir: Path,
    request: dict[str, Any],
    fixture_path: Path | None,
    result: ComparisonResult | None,
    unavailable: str | None,
) -> None:
    output_dir.mkdir(parents=True, exist_ok=True)
    payload: dict[str, Any] = {
        "fixture": FIXTURE_RELATIVE_PATH,
        "fixture_sha256": _sha256(fixture_path) if fixture_path and fixture_path.exists() else None,
        "fixture_steps": len(request["steps"]),
        "go_harness": "sim/gocli/main.go",
        "network": "GOPROXY=off; no external Go modules are imported",
        "status": "SKIPPED" if unavailable else ("PASS" if result and result.ok else "FAIL"),
        "unavailable": unavailable,
        "decisions_compared": result.decisions_compared if result else 0,
        "resolutions_compared": result.resolutions_compared if result else 0,
        "mismatches": result.mismatches if result else [],
    }
    (output_dir / "consistency_result.json").write_text(
        json.dumps(payload, indent=2) + "\n", encoding="utf-8", newline="\n"
    )

    lines = [
        "# Go / Python consistency check",
        "",
        "Generated by `sim/consistency.py`.",
        "",
        f"- Status: **{payload['status']}**",
        f"- Fixture: `{FIXTURE_RELATIVE_PATH}`",
        f"- Fixture SHA-256: `{payload['fixture_sha256']}`",
        f"- Steps: {payload['fixture_steps']}",
        f"- Decisions compared: {payload['decisions_compared']}",
        f"- Escrow resolutions compared: {payload['resolutions_compared']}",
        "",
        "The Go harness replays the same script through the real ",
        "`hydroledger/verifier` package with `go run ./sim/gocli`, `GOPROXY=off` ",
        "and no external Go module imports. The Python side is ",
        "`sim.three_state.ThreeStateVerifier`. State, reason code, diagnostics, ",
        "escrow outcomes and resolution timestamps must match.",
        "",
    ]
    if unavailable:
        lines += [
            "## Unavailable",
            "",
            "```text",
            unavailable,
            "```",
            "",
            "The checker did not run. This is a skipped check, not a pass.",
        ]
    elif result and result.ok:
        lines += [
            "## Result",
            "",
            "All decisions and resolutions matched.",
        ]
    else:
        lines += ["## Mismatches", ""]
        for mismatch in (result.mismatches if result else ["not run"]):
            lines.append(f"- {mismatch}")
    lines += [
        "",
        "## Scope",
        "",
        "This checks algorithmic consistency on synthetic fixtures. It is not ",
        "field validation, not a security proof and not evidence that the ",
        "simulator is a complete model of the Go package's concurrency or ",
        "persistence behaviour.",
        "",
    ]
    (output_dir / "CONSISTENCY.md").write_text(
        "\n".join(lines), encoding="utf-8", newline="\n"
    )


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Compare the Python verifier mirror with the Go verifier."
    )
    parser.add_argument(
        "--output-dir",
        default="docs/results/phase2/consistency",
        help="directory for CONSISTENCY.md and consistency_result.json",
    )
    parser.add_argument(
        "--fixtures",
        default=FIXTURE_RELATIVE_PATH,
        help=f"fixture path (default {FIXTURE_RELATIVE_PATH})",
    )
    parser.add_argument(
        "--write-fixtures",
        action="store_true",
        help="write the built-in fixture request to --fixtures and exit",
    )
    args = parser.parse_args(argv)

    root = repo_root()
    fixture_path = root / args.fixtures if not Path(args.fixtures).is_absolute() else Path(args.fixtures)
    request = build_fixture_request()
    if args.write_fixtures:
        write_fixtures(fixture_path)
        print(f"wrote fixtures to {fixture_path}")
        return 0

    # Prefer the committed fixture file, so the check exercises exactly the
    # reviewed script. Fall back to the in-memory builder if it is absent.
    if fixture_path.exists():
        request = json.loads(fixture_path.read_text(encoding="utf-8"))

    python_result = run_python(request)
    try:
        go_result = run_go(request, root=root)
    except ConsistencyUnavailable as exc:
        _write_report(Path(args.output_dir), request, fixture_path, None, str(exc))
        print(f"SKIPPED: {exc}", file=sys.stderr)
        return 0
    except ConsistencyError as exc:
        _write_report(Path(args.output_dir), request, fixture_path, None, str(exc))
        print(f"FAILED: {exc}", file=sys.stderr)
        return 1

    result = compare(python_result, go_result)
    _write_report(Path(args.output_dir), request, fixture_path, result, None)
    if not result.ok:
        for mismatch in result.mismatches:
            print(mismatch, file=sys.stderr)
        print(f"FAILED: {len(result.mismatches)} mismatch(es)", file=sys.stderr)
        return 1
    print(
        f"PASS: {result.decisions_compared} decisions and "
        f"{result.resolutions_compared} resolutions matched"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
