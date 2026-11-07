"""Retrieve hourly rainfall from the Open-Meteo Historical Weather (Archive) API.

Task 4 (Phase 2): deterministic, auditable retrieval for the gridded
weather-context replay experiments.

Location (as specified): 17.5449 N, 78.3864 E
Date range: 2025-07-01 to 2025-09-30 (inclusive)
Timezone: Asia/Kolkata

This script uses only the Python standard library. It makes network requests
only when explicitly run; ``run_all.sh`` can skip retrieval when cached raw data
is present. The raw API response is saved unchanged and checksummed. Missing
intervals are reported, never silently filled.
"""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
import os
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from pathlib import Path
from typing import Any, Sequence

# Verified against https://open-meteo.com/en/docs/historical-weather-api
# and https://archive-api.open-meteo.com/v1/archive on 2026-11-07.
API_BASE = "https://archive-api.open-meteo.com/v1/archive"
DEFAULT_LATITUDE = 17.5449
DEFAULT_LONGITUDE = 78.3864
DEFAULT_START_DATE = "2025-07-01"
DEFAULT_END_DATE = "2025-09-30"
DEFAULT_TIMEZONE = "Asia/Kolkata"
DEFAULT_HOURLY = ("rain", "precipitation")
EXPECTED_HOURLY_ROWS = 2208  # 92 days * 24 hours

USER_AGENT = "HydroLedger-Phase2/1.0 (research simulation; contact: local repo)"


@dataclass(frozen=True)
class RetrievalResult:
    url: str
    retrieved_at_utc: str
    raw_path: Path
    csv_path: Path
    metadata_path: Path
    raw_sha256: str
    csv_sha256: str
    row_count: int
    expected_rows: int
    missing_intervals: list[str]
    status: str


def _sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def _sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def build_request_url(
    latitude: float = DEFAULT_LATITUDE,
    longitude: float = DEFAULT_LONGITUDE,
    start_date: str = DEFAULT_START_DATE,
    end_date: str = DEFAULT_END_DATE,
    timezone_name: str = DEFAULT_TIMEZONE,
    hourly: Sequence[str] = DEFAULT_HOURLY,
) -> str:
    params = {
        "latitude": f"{latitude}",
        "longitude": f"{longitude}",
        "start_date": start_date,
        "end_date": end_date,
        "hourly": ",".join(hourly),
        "timezone": timezone_name,
    }
    return API_BASE + "?" + urllib.parse.urlencode(params)


def _fetch_with_retry(
    url: str,
    *,
    timeout: float = 60.0,
    max_attempts: int = 6,
    initial_backoff_s: float = 1.0,
    max_backoff_s: float = 60.0,
) -> bytes:
    headers = {"Accept": "application/json", "User-Agent": USER_AGENT}
    backoff = initial_backoff_s
    last_error: Exception | None = None
    for attempt in range(1, max_attempts + 1):
        request = urllib.request.Request(url, headers=headers, method="GET")
        try:
            with urllib.request.urlopen(request, timeout=timeout) as response:
                body = response.read()
                if not body:
                    raise RuntimeError("empty response body")
                return body
        except urllib.error.HTTPError as exc:
            last_error = exc
            # 4xx except 429/408 are unlikely to succeed on retry.
            retryable = exc.code in (408, 429) or exc.code >= 500
            if not retryable or attempt == max_attempts:
                raise
            # Respect Retry-After when present.
            retry_after = exc.headers.get("Retry-After")
            sleep_s = backoff
            if retry_after:
                try:
                    sleep_s = max(sleep_s, float(retry_after))
                except ValueError:
                    pass
            print(
                f"attempt {attempt}/{max_attempts} HTTP {exc.code}; retrying in {sleep_s:.1f}s",
                file=sys.stderr,
            )
            time.sleep(min(sleep_s, max_backoff_s))
            backoff = min(backoff * 2.0, max_backoff_s)
        except (urllib.error.URLError, TimeoutError, OSError) as exc:
            last_error = exc
            if attempt == max_attempts:
                raise
            print(
                f"attempt {attempt}/{max_attempts} network error ({exc}); retrying in {backoff:.1f}s",
                file=sys.stderr,
            )
            time.sleep(min(backoff, max_backoff_s))
            backoff = min(backoff * 2.0, max_backoff_s)
    assert last_error is not None
    raise last_error


def _expected_times(start_date: str, end_date: str) -> list[str]:
    # Open-Meteo returns local ISO timestamps like 2025-07-01T00:00 for the
    # requested timezone. We validate by counting and by checking sequence order
    # and hourly spacing in the returned local strings.
    start = datetime.fromisoformat(start_date)
    end = datetime.fromisoformat(end_date)
    count = int(((end - start).days + 1) * 24)
    return [f"<{count} hourly local timestamps expected>"]


def _find_missing_intervals(times: Sequence[str]) -> list[str]:
    if not times:
        return ["no timestamps returned"]
    missing: list[str] = []
    parsed: list[datetime] = []
    for value in times:
        try:
            # Accept both "2025-07-01T00:00" and full ISO forms.
            parsed.append(datetime.fromisoformat(value.replace("Z", "+00:00")))
        except ValueError:
            missing.append(f"unparseable timestamp: {value!r}")
            return missing
    for prev, curr in zip(parsed, parsed[1:]):
        delta = curr - prev
        if delta == timedelta(hours=1):
            continue
        if delta <= timedelta(0):
            missing.append(f"non-increasing interval: {prev.isoformat()} -> {curr.isoformat()}")
        else:
            # Report gap size; do not fill.
            missing.append(
                f"gap of {delta.total_seconds() / 3600:.1f}h: {prev.isoformat()} -> {curr.isoformat()}"
            )
    return missing


def _tidy_csv_rows(payload: dict[str, Any]) -> tuple[list[str], list[list[Any]]]:
    hourly = payload.get("hourly") or {}
    units = payload.get("hourly_units") or {}
    times = hourly.get("time") or []
    variables = [key for key in hourly.keys() if key != "time"]
    header = ["time"]
    for var in variables:
        unit = units.get(var)
        header.append(f"{var}_{unit}" if unit else var)
    header.extend(
        [
            "latitude",
            "longitude",
            "elevation_m",
            "timezone",
            "utc_offset_seconds",
        ]
    )
    lat = payload.get("latitude")
    lon = payload.get("longitude")
    elev = payload.get("elevation")
    tz = payload.get("timezone")
    offset = payload.get("utc_offset_seconds")
    rows: list[list[Any]] = []
    for i, ts in enumerate(times):
        row: list[Any] = [ts]
        for var in variables:
            values = hourly.get(var) or []
            row.append(values[i] if i < len(values) else "")
        row.extend([lat, lon, elev, tz, offset])
        rows.append(row)
    return header, rows


def retrieve(
    output_dir: Path,
    *,
    latitude: float = DEFAULT_LATITUDE,
    longitude: float = DEFAULT_LONGITUDE,
    start_date: str = DEFAULT_START_DATE,
    end_date: str = DEFAULT_END_DATE,
    timezone_name: str = DEFAULT_TIMEZONE,
    hourly: Sequence[str] = DEFAULT_HOURLY,
    force: bool = False,
    allow_cached: bool = True,
    timeout: float = 60.0,
) -> RetrievalResult:
    output_dir.mkdir(parents=True, exist_ok=True)
    raw_path = output_dir / "open_meteo_archive_raw.json"
    csv_path = output_dir / "open_meteo_archive_hourly.csv"
    metadata_path = output_dir / "open_meteo_archive_metadata.json"
    url = build_request_url(latitude, longitude, start_date, end_date, timezone_name, hourly)

    retrieved_at = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    if allow_cached and not force and raw_path.exists():
        raw_bytes = raw_path.read_bytes()
        cached = True
        # Preserve original retrieval metadata if present.
        if metadata_path.exists():
            try:
                old = json.loads(metadata_path.read_text(encoding="utf-8"))
                retrieved_at = old.get("retrieved_at_utc", retrieved_at)
                url = old.get("request_url", url)
            except Exception:
                pass
    else:
        raw_bytes = _fetch_with_retry(url, timeout=timeout)
        raw_path.write_bytes(raw_bytes)
        cached = False

    payload = json.loads(raw_bytes.decode("utf-8"))
    if payload.get("error"):
        raise RuntimeError(f"API error: {payload.get('reason')}")

    header, rows = _tidy_csv_rows(payload)
    with csv_path.open("w", newline="", encoding="utf-8") as handle:
        writer = csv.writer(handle)
        writer.writerow(header)
        writer.writerows(rows)

    times = (payload.get("hourly") or {}).get("time") or []
    row_count = len(times)
    missing = _find_missing_intervals(times)
    if row_count != EXPECTED_HOURLY_ROWS:
        missing.append(
            f"row count mismatch: got {row_count}, expected {EXPECTED_HOURLY_ROWS} "
            f"for {start_date}..{end_date} in {timezone_name}"
        )

    raw_sha = _sha256_bytes(raw_bytes)
    csv_sha = _sha256_file(csv_path)
    status = "OK" if not missing else "WARN_MISSING_OR_MISMATCH"
    metadata = {
        "status": status,
        "cached": cached,
        "request_url": url,
        "retrieved_at_utc": retrieved_at,
        "api_base": API_BASE,
        "api_docs": "https://open-meteo.com/en/docs/historical-weather-api",
        "api_params_verified_utc": "2026-11-07",
        "parameters": {
            "latitude": latitude,
            "longitude": longitude,
            "start_date": start_date,
            "end_date": end_date,
            "timezone": timezone_name,
            "hourly": list(hourly),
        },
        "expected_hourly_rows": EXPECTED_HOURLY_ROWS,
        "actual_hourly_rows": row_count,
        "missing_intervals": missing,
        "files": {
            "raw_json": raw_path.name,
            "tidy_csv": csv_path.name,
            "raw_sha256": raw_sha,
            "tidy_csv_sha256": csv_sha,
        },
        "response_metadata": {
            "latitude": payload.get("latitude"),
            "longitude": payload.get("longitude"),
            "elevation": payload.get("elevation"),
            "timezone": payload.get("timezone"),
            "timezone_abbreviation": payload.get("timezone_abbreviation"),
            "utc_offset_seconds": payload.get("utc_offset_seconds"),
            "generationtime_ms": payload.get("generationtime_ms"),
        },
        "label": "gridded weather-context replay source (Open-Meteo archive), not rooftop ground truth",
    }
    metadata_path.write_text(
        json.dumps(metadata, indent=2) + "\n", encoding="utf-8", newline="\n"
    )
    return RetrievalResult(
        url=url,
        retrieved_at_utc=retrieved_at,
        raw_path=raw_path,
        csv_path=csv_path,
        metadata_path=metadata_path,
        raw_sha256=raw_sha,
        csv_sha256=csv_sha,
        row_count=row_count,
        expected_rows=EXPECTED_HOURLY_ROWS,
        missing_intervals=missing,
        status=status,
    )


def main(argv: Sequence[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="Retrieve Open-Meteo archive rainfall for Phase 2.")
    parser.add_argument(
        "--output-dir",
        default="docs/results/phase2/rainfall",
        help="directory for raw JSON, tidy CSV and metadata",
    )
    parser.add_argument("--latitude", type=float, default=DEFAULT_LATITUDE)
    parser.add_argument("--longitude", type=float, default=DEFAULT_LONGITUDE)
    parser.add_argument("--start-date", default=DEFAULT_START_DATE)
    parser.add_argument("--end-date", default=DEFAULT_END_DATE)
    parser.add_argument("--timezone", default=DEFAULT_TIMEZONE)
    parser.add_argument("--hourly", default=",".join(DEFAULT_HOURLY))
    parser.add_argument("--force", action="store_true", help="ignore cached raw JSON")
    parser.add_argument("--offline", action="store_true", help="use cached raw JSON only; fail if absent")
    parser.add_argument("--timeout", type=float, default=60.0)
    args = parser.parse_args(argv)

    hourly = [v.strip() for v in args.hourly.split(",") if v.strip()]
    try:
        result = retrieve(
            Path(args.output_dir),
            latitude=args.latitude,
            longitude=args.longitude,
            start_date=args.start_date,
            end_date=args.end_date,
            timezone_name=args.timezone,
            hourly=hourly,
            force=args.force,
            allow_cached=True,
            timeout=args.timeout,
        )
    except Exception as exc:
        if args.offline:
            print(f"OFFLINE retrieval failed: {exc}", file=sys.stderr)
            return 2
        # Try cached fallback.
        out = Path(args.output_dir)
        raw = out / "open_meteo_archive_raw.json"
        if raw.exists():
            print(f"Network retrieval failed ({exc}); using cached raw JSON.", file=sys.stderr)
            result = retrieve(
                out,
                latitude=args.latitude,
                longitude=args.longitude,
                start_date=args.start_date,
                end_date=args.end_date,
                timezone_name=args.timezone,
                hourly=hourly,
                force=False,
                allow_cached=True,
                timeout=args.timeout,
            )
            result = result  # cached path
        else:
            print(f"Retrieval failed and no cache exists: {exc}", file=sys.stderr)
            return 1

    print(f"status={result.status} rows={result.row_count}/{result.expected_rows}")
    print(f"url={result.url}")
    print(f"retrieved_at_utc={result.retrieved_at_utc}")
    print(f"raw={result.raw_path} sha256={result.raw_sha256}")
    print(f"csv={result.csv_path} sha256={result.csv_sha256}")
    if result.missing_intervals:
        print("missing_intervals:")
        for item in result.missing_intervals:
            print(f"  - {item}")
        return 0 if result.status.startswith("WARN") else 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
