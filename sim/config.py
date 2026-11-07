"""Single source of truth for the HydroLedger Phase 0B simulation.

Everything that can change a result lives here: the number of events, the
seeds, the scenario definitions, the distributions used to generate synthetic
telemetry, and the thresholds of the four verification methods.

The parameters were chosen a priori from the written Phase 0B specification
and the Go verifier defaults. They were not searched or tuned against the
evaluation seeds. Changing a value here changes the experiment; re-run
``run_all.sh`` and update the results rather than editing numbers by hand.

Ground truth
------------
``fraudulent=True`` means the reading's claimed depth is deliberately
inconsistent with the synthetic rainfall process (an over-claim or a
fabricated reading). Honest sensor noise and under-claims are not fraud.
Precision, recall, F1 and the false-positive rate are therefore measured for
*fraud detection*: positive = fraudulent reading.

The simulation is synthetic only. Nothing in this directory is evidence of
field accuracy, real-world flood prediction, or performance on Hyderabad
telemetry.
"""

from __future__ import annotations

from dataclasses import dataclass


# ---------------------------------------------------------------------------
# Experiment size and seeds
# ---------------------------------------------------------------------------

EVENTS_PER_SEED = 100_000
SEEDS = (1, 2, 3, 4, 5, 6, 7, 8, 9, 10)

# Stable run order for progress output and CSV rows.
METHOD_ORDER = ("static", "physics", "peer", "hybrid")

DEFAULT_OUTPUT_DIR = "docs/results"
FLOAT_DECIMALS = 6


# ---------------------------------------------------------------------------
# Synthetic world
# ---------------------------------------------------------------------------

CELLS = 16
DEVICES_PER_CELL = 32
DURATION_S = 24 * 60 * 60

# Device characteristics. One device keeps the same roof and runoff
# coefficient for the whole run so its effective-depth conversion is stable.
ROOF_AREA_M2_RANGE = (80.0, 200.0)
RUNOFF_COEFF_RANGE = (0.80, 0.95)

# Capacity is deliberately large enough that the storage/catchment check is
# not the discriminating rule. Capacity validation is part of all four
# methods and is documented as a non-factor in METHOD.md.
TANK_CAPACITY_L = 100_000.0
STORED_LITRES_RANGE = (0.0, 1_000.0)

# Honest sensor noise on the effective depth, in mm.
SENSOR_NOISE_MM = 0.20

# Radar error model for ordinary rain: multiplicative and additive noise.
RADAR_REL_NOISE = 0.05
RADAR_ABS_NOISE_MM = 0.15
RADAR_GAIN_RANGE = (0.75, 1.25)

# Ordinary storms are Gaussian pulses in time. Widths are broad enough that
# peers inside a 10-minute window normally see a coherent depth.
STORMS_PER_CELL = (1, 3)
STORM_CENTRE_FRACTION_RANGE = (0.10, 0.90)
STORM_WIDTH_S = (45 * 60, 120 * 60)
STORM_PEAK_MM = (2.0, 12.0)
BASELINE_MM = (0.0, 0.6)

# Microburst: one cell gets a short, intense pulse that the radar under-reads.
MICROBURST_CENTRE_FRACTION_RANGE = (0.25, 0.75)
MICROBURST_PEAK_MM = (30.0, 45.0)
MICROBURST_WIDTH_S = (15 * 60, 25 * 60)
MICROBURST_RADAR_CATCH_RANGE = (0.30, 0.45)

# Attack amplitudes. These are fabrication magnitudes, not verifier
# thresholds; they are fixed before evaluation.
NORMAL_OVERCLAIM_MM = (8.0, 20.0)
SPIKE_OVERCLAIM_MM = (15.0, 60.0)
DRY_SPOOF_CLAIM_MM = (3.0, 15.0)

# Radar-bounded spoofing: the radar is wet but the true rainfall is light.
# Attackers claim just under the radar-derived physical bound.
RADAR_BOUNDED_TRUE_MM = (0.2, 1.0)
RADAR_BOUNDED_RADAR_MM = (4.0, 8.0)
RADAR_BOUNDED_CLAIM_FRACTION = (0.75, 0.98)
RADAR_BOUNDED_CLAIM_NOISE_MM = 0.15
RADAR_BOUNDED_MAX_FRACTION = 0.99
RADAR_BOUNDED_MIN_OVERCLAIM_MM = 0.5
RADAR_BOUNDED_RADAR_NOISE_MM = 0.25

# Coordinated peer compromise: a fixed fraction of devices per cell fabricate
# a coherent rainy reading while the radar is dry.
COORDINATED_CLAIM_MEAN_MM = 9.0
COORDINATED_CLAIM_SD_MM = 0.3
COORDINATED_MIN_OVERCLAIM_MM = 1.0
COORDINATED_TRUE_MM = (0.05, 0.15)

# Stable mapping from (seed, scenario) to one integer RNG seed.
SEED_STRIDE = 1000

# Dry radar values used by dry-weather and coordinated scenarios.
DRY_RADAR_MM = (0.0, 0.4)


# ---------------------------------------------------------------------------
# Scenario definitions
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class Scenario:
    """One synthetic scenario.

    ``fraud_fraction`` is the a-priori share of fraudulent events for
    scenarios 1-5. For the coordinated scenarios the realised share is set by
    ``compromised_fraction`` (the share of compromised devices per cell), so
    ``fraud_fraction`` is informational there.
    """

    key: str
    label: str
    fraud_fraction: float
    mode: str
    radar_mode: str
    compromised_fraction: float
    description: str


SCENARIOS = (
    Scenario(
        key="normal_rainfall",
        label="Normal rainfall",
        fraud_fraction=0.02,
        mode="normal",
        radar_mode="normal",
        compromised_fraction=0.0,
        description=(
            "Ordinary spatially correlated storms, radar approximately "
            "consistent with the true depth, 2% random over-claims."
        ),
    ),
    Scenario(
        key="microburst",
        label="Microburst",
        fraud_fraction=0.03,
        mode="microburst",
        radar_mode="microburst",
        compromised_fraction=0.0,
        description=(
            "One cell receives a short 30-45 mm pulse while the radar "
            "under-reads that cell (30-45% catch); 3% random over-claims."
        ),
    ),
    Scenario(
        key="isolated_spike",
        label="Isolated spike",
        fraud_fraction=0.08,
        mode="isolated_spike",
        radar_mode="normal",
        compromised_fraction=0.0,
        description=(
            "Ordinary storms plus 8% single-reading over-claims of "
            "15-60 mm above the local true depth."
        ),
    ),
    Scenario(
        key="dry_weather_spoofing",
        label="Dry-weather spoofing",
        fraud_fraction=0.50,
        mode="dry_spoof",
        radar_mode="dry",
        compromised_fraction=0.0,
        description=(
            "No true rain and a dry radar; 50% of readings fabricate "
            "3-15 mm."
        ),
    ),
    Scenario(
        key="radar_bounded_spoofing",
        label="Radar-bounded spoofing",
        fraud_fraction=0.50,
        mode="radar_bounded",
        radar_mode="bounded",
        compromised_fraction=0.0,
        description=(
            "True depth is 0.2-1.0 mm but the radar reads 4-8 mm; 50% of "
            "readings claim 75-98% of the radar-derived bound, so a "
            "physics-only check cannot see them."
        ),
    ),
    Scenario(
        key="coordinated_40",
        label="Coordinated peer compromise 40%",
        fraud_fraction=0.40,
        mode="coordinated",
        radar_mode="dry",
        compromised_fraction=0.40,
        description=(
            "40% of devices in every cell are compromised and report a "
            "coherent fabricated 9 mm under a dry radar; 60% are honest."
        ),
    ),
    Scenario(
        key="coordinated_60",
        label="Coordinated peer compromise 60%",
        fraud_fraction=0.60,
        mode="coordinated",
        radar_mode="dry",
        compromised_fraction=0.60,
        description=(
            "60% of devices in every cell are compromised and report a "
            "coherent fabricated 9 mm under a dry radar; 40% are honest. "
            "This is the peer-majority stress case."
        ),
    ),
)


# ---------------------------------------------------------------------------
# Verifier parameters (mirroring the Go defaults where applicable)
# ---------------------------------------------------------------------------

ALPHA = 1.15
BETA_MM = 2.0

MIN_PEERS = 4
PEER_MAX_AGE_S = 10 * 60

Z_SCALE = 0.6745
MAD_FLOOR_MM = 0.15
OUTLIER_Z = 3.5

PEER_RAIN_MM = 2.0
COHERENCE_ABS_MM = 1.0
COHERENCE_REL_FRAC = 0.25

RADAR_DRY_MM = 0.5
STATIC_THRESHOLD_MM = 10.0
