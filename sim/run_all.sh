#!/usr/bin/env bash
# Regenerate every Phase 0B table from a clean checkout.
#
# Usage (from anywhere):
#   bash sim/run_all.sh
#
# Optional overrides:
#   PYTHON=python3.12 bash sim/run_all.sh
#
# The script runs the offline unit tests first. If they fail, no results are
# written. No network access is required.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${ROOT_DIR}"

PYTHON="${PYTHON:-python3}"

echo "== HydroLedger Phase 0B =="
echo "root:   ${ROOT_DIR}"
echo "python: $("${PYTHON}" --version 2>&1)"
echo

echo "== unit tests =="
"${PYTHON}" -m unittest discover -s sim/tests -t . -v

echo
echo "== full ablation (configuration from sim/config.py) =="
"${PYTHON}" sim/ablation_experiment.py --output-dir docs/results

echo
echo "== outputs =="
ls -l docs/results
