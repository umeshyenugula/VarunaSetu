#!/usr/bin/env bash
# Regenerate Phase 2 simulation, rainfall replay and evaluation results.
#
# Usage (from anywhere):
#   bash sim/run_all.sh
#
# Environment overrides:
#   PYTHON=python3.12 bash sim/run_all.sh
#   OFFLINE=1 bash sim/run_all.sh          # use cached rainfall raw JSON only
#   SKIP_ABLATION=1 bash sim/run_all.sh    # do not rerun full 7M-event ablation
#   QUICK=1 bash sim/run_all.sh            # smaller local verification run
#
# The script runs offline unit tests first. It does not modify Go code.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${ROOT_DIR}"

PYTHON="${PYTHON:-python3}"
PHASE2_DIR="docs/results/phase2"
mkdir -p "${PHASE2_DIR}"

LOG="${PHASE2_DIR}/RUN_LOG.md"
{
  echo "# Phase 2 run log"
  echo
  echo "- Started (UTC): $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "- Python: $("${PYTHON}" --version 2>&1)"
  echo "- Platform: $(uname -a 2>/dev/null || echo windows)"
  echo "- Go: $(go version 2>/dev/null || echo 'not found')"
  echo "- OFFLINE=${OFFLINE:-0} SKIP_ABLATION=${SKIP_ABLATION:-0} QUICK=${QUICK:-0}"
  echo
} > "${LOG}"

echo "== HydroLedger Phase 2 =="
echo "root:   ${ROOT_DIR}"
echo "python: $("${PYTHON}" --version 2>&1)"
echo "go:     $(go version 2>&1 || echo 'not found')"
echo

echo "== unit tests (offline) =="
"${PYTHON}" -m unittest discover -s sim/tests -t . -v 2>&1 | tee -a "${LOG}"

echo
echo "== phase 2 pipeline =="
ARGS=()
[ "${OFFLINE:-0}" = "1" ] && ARGS+=(--offline)
[ "${SKIP_ABLATION:-0}" = "1" ] && ARGS+=(--skip-ablation)
[ "${QUICK:-0}" = "1" ] && ARGS+=(--quick)
"${PYTHON}" -m sim.phase2_runner "${ARGS[@]}" 2>&1 | tee -a "${LOG}"

echo
echo "== report index =="
"${PYTHON}" -m sim.phase2_index 2>&1 | tee -a "${LOG}"

{
  echo
  echo "- Finished (UTC): $(date -u +%Y-%m-%dT%H:%M:%SZ)"
} >> "${LOG}"

echo
echo "== outputs =="
find docs/results/phase2 -maxdepth 3 -type f | sort
