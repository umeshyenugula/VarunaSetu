/**
 * Documentation content for the in-app docs section.
 * Every statement is drawn from the repository's own documents (README, docs/*.md, CONTRACTS.md);
 * the "Sources" page links the originals.
 */
export const GITHUB = 'https://github.com/umeshyenugula/VarunaSetu';
export const DEEPWIKI = 'https://deepwiki.com/umeshyenugula/VarunaSetu';
const BLOB = `${GITHUB}/blob/master/`;

export type Block =
  | { t: 'p'; x: string }
  | { t: 'h'; x: string }
  | { t: 'ul' | 'ol'; x: string[] }
  | { t: 'table'; head: string[]; rows: string[][] }
  | { t: 'code'; x: string }
  | { t: 'note'; x: string; warn: boolean }
  | { t: 'steps'; x: { title: string; body: string }[] }
  | { t: 'hex' };

export interface Page { id: string; title: string; summary: string; blocks: Block[] }
export interface Section { id: string; nav: string; title: string; summary: string; pages: Page[] }
export interface Article { id: string; numeral: string; title: string; text: string; source: string; path: string }

const p = (x: string): Block => ({ t: 'p', x });
const h = (x: string): Block => ({ t: 'h', x });
const ul = (...x: string[]): Block => ({ t: 'ul', x });
const ol = (...x: string[]): Block => ({ t: 'ol', x });
const table = (head: string[], ...rows: string[][]): Block => ({ t: 'table', head, rows });
const code = (x: string): Block => ({ t: 'code', x });
const note = (x: string, warn = false): Block => ({ t: 'note', x, warn });
const steps = (...x: [string, string][]): Block => ({ t: 'steps', x: x.map(([title, body]) => ({ title, body })) });

/* ------------------------------------------------------------------ How it works */

const HOW: Page[] = [
  {
    id: 'pipeline', title: 'The pipeline', summary: 'From a signed reading on a roof to a recorded verdict, in seven checks.',
    blocks: [
      p('A rooftop device measures the water it has collected, signs the reading with its Ed25519 key and posts it to `POST /telemetry/readings`. The service does not trust the number. It asks seven questions in order, and every answer is written down.'),
      steps(
        ['Identity', 'Is the device enrolled and not revoked? Is the signature valid, the timestamp fresh and the nonce unused? Failures are answered with `401` or `409`.'],
        ['H3 cell', 'The building maps to an H3 cell at resolution 9: about 0.1 km², roughly a street block.'],
        ['Radar bound', '`R_obs = claimed litres ÷ (roof m² × runoff)` is the rainfall depth the claim implies. `R_max = 1.15 × radar mm + 2.0 mm` is the most the physics and the radar allow.'],
        ['Peers', 'The latest reading of every other device in the same cell within 10 minutes. With at least 4 peers a robust z-score (median and MAD) is computed. `Z > 3.5` and above the median is `SPATIAL_PEER_OUTLIER`.'],
        ['Conflict', 'Radar is dry (≤ 0.5 mm) yet a coherent group of peers reports rain (median ≥ 2 mm). That is `PROVISIONAL_ESCROW`, not a rejection.'],
        ['Resolution', 'An escrow is released when a later reading in the cell has live radar, or when at least 2 cells of the k=1 ring each have at least 3 devices with median ≥ 2 mm. Otherwise it expires after 15 minutes into `QUARANTINE`.'],
        ['Ledger', 'Every decision and every resolution becomes a hash-chained audit entry.'],
      ),
      note('Field validation and the capacity check run first. A reading that fails them returns immediately and is not recorded (`INVALID_INPUT`, `STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED`). With too few peers the verifier falls back to the radar bound. With enough peers the first match wins: positive outlier, then radar/peer conflict, then `R_obs > R_max`, then accept.'),
      h('Three verdicts'),
      table(['Verdict', 'Meaning', 'Typical reason code'],
        ['`ACCEPT`', 'The claim is credited.', '`RADAR_AND_PEERS_AGREE`'],
        ['`PROVISIONAL_ESCROW`', 'Evidence disagrees, so the claim is held while corroboration is awaited.', '`RADAR_PEER_CONFLICT_ESCROW`'],
        ['`QUARANTINE`', 'The claim is rejected or held for review.', '`SPATIAL_PEER_OUTLIER`, `PHYSICAL_BOUND_EXCEEDED`']),
      h('Watch it run'),
      p('The [Story](#/story) screen runs three of these paths against the live backend: an honest storm (everything accepted), an inflated claim (six times what the roof could collect, quarantined) and a dry radar over wet roofs (held in escrow). The scenario lives in the URL: `#/story?scenario=inflated`.'),
    ],
  },
  {
    id: 'h3-network', title: 'The H3 network', summary: 'Why a neighbourhood can settle what one device, or the radar, cannot.',
    blocks: [
      p('H3 divides the city into hexagonal cells. At resolution 9 a cell is about 0.1 km², roughly a street block. Each device reports under the cell of its building, and each cell has exactly six neighbours: the k=1 ring.'),
      h('Two ideas existing validators do not combine'),
      ul('**Locality as evidence.** A claim is judged against the devices on the same few blocks, not against a citywide threshold.',
        '**Neighbourhood as a tie-breaker.** When radar and peers disagree, VarunaSetu does not guess. It holds the reading and asks the surrounding H3 cells.'),
      h('Try the tie-breaker'),
      p('The centre cell holds an escrowed reading: its radar says dry, its own devices say wet. Click a neighbour to flip it between raining and dry, or start from a preset. An escrow is released when at least 2 neighbours each have at least 3 devices reporting a median of 2 mm or more, or when later radar confirms the rain.'),
      { t: 'hex' },
      h('Same evidence, opposite truth'),
      p('A real storm over the west of the city and a staged claim in the far east produce the same device-level signal: dry radar and a coherent group reporting heavy rain. Radar-only rejects both. Peer-only and the static cap accept both. VarunaSetu holds both, credits the real storm because its k=1 neighbours also show rain, and lets the staged claim expire because its neighbours are dry.'),
      note('Limits, stated on purpose: a coordinated cluster of three neighbouring cells can corroborate itself, and the radar value is device-supplied in this prototype. See [Scenarios](#/how-it-works/scenarios).', true),
    ],
  },
  {
    id: 'scenarios', title: 'Scenarios', summary: 'Twelve cases in the virtual city: what the system handles, and the limits it shows on purpose.',
    blocks: [
      p('Scenarios 1 to 7 and 12 show what the system handles. Scenarios 8 to 11 are documented limits.'),
      table(['#', 'Scenario', 'What it shows', 'Settled by'],
        ['1', 'A storm crosses the city', 'Control: honest data, working radar, nothing flagged beyond ramp noise', 'verifier'],
        ['2', 'Two neighbours inflate their harvest', 'Radar over-reads, so a radar bound alone lets a cheat through; peers catch it', 'peer test'],
        ['3', 'The radar goes blind over the city centre', 'Real rain with dry radar becomes escrow, not rejection', 'H3 neighbours'],
        ['4', 'The radar blinks, then comes back', 'A tight storm that neighbours cannot vouch for', 'radar recovery'],
        ['5', 'Same evidence, opposite truth', 'A real storm and a staged claim look identical at device level; H3 separates them', 'H3 neighbours vs expiry'],
        ['6', 'A cell with too few neighbours', 'Fewer than 4 peers: falls back to the radar bound', 'radar bound'],
        ['7', 'Attacks on the door, not the data', 'Replay, tamper, unknown or revoked key, stale or future time, malformed and impossible readings', 'identity, validation'],
        ['8', 'A real cloudburst nobody else can see', 'Limit: a one-cell storm missed by radar is not credited', 'expiry'],
        ['9', 'Three neighbouring cells collude', 'Limit: a coordinated cluster corroborates itself', 'H3 neighbours (wrongly)'],
        ['10', 'A cheat that also lies about the radar', 'Limit: the radar value is device-supplied in this prototype', 'radar bound (fooled)'],
        ['11', 'Rain starts faster than peers can follow', 'Limit: abrupt onset flags honest first reporters', 'peer test'],
        ['12', 'A day in the virtual city', 'Everything at once for two hours, with attacks', 'all of it']),
      p('Run them with `go run ./cmd/citysim serve` and open `http://127.0.0.1:8090/`, or check every scenario headless with `go run ./cmd/citysim verify`. Measured results are in [DEMO_RESULTS.md](' + BLOB + 'docs/DEMO_RESULTS.md).'),
    ],
  },
  {
    id: 'real-vs-simulated', title: 'Real vs simulated', summary: 'What the live demo actually exercises, and what it pretends.',
    blocks: [
      p('Nothing on the demo screen is a mock of the backend. Every reading is an Ed25519-signed `POST /telemetry/readings`, and every verdict, escrow, reason code and ledger entry comes from the production verifier, identity layer, PostgreSQL/PostGIS persistence and audit ledger.'),
      table(['Real (the system under test)', 'Simulated (the world around it)'],
        ['HTTP API, JSON contract, status codes', 'Rainfall: slow Gaussian storms, uniform inside a cell'],
        ['Ed25519 signatures, nonce replay protection, idempotency', 'The radar product, including injected faults (dry, lagged, biased)'],
        ['Device enrolment lookup and revocation in PostgreSQL', 'The 210 buildings and devices (synthetic, but located in real H3 cells)'],
        ['The Go verifier: radar bound, median/MAD peer test, escrow', 'Cheating behaviour: inflation, collusion, radar spoofing'],
        ['H3 spatial indexing and k-ring neighbours', 'The clock: virtual, so 100 minutes of storm replay in seconds'],
        ['Escrow state machine, TTL, durable neighbour corroboration', '—'],
        ['Capacity reservation and the SHA-256 audit ledger', '—']),
      h('Honest notes on the harness'),
      ul('The embedded service uses a virtual clock and the harness sweeps once per simulated minute. All thresholds (10-minute peer window, 15-minute escrow TTL, 5-minute signature window) keep their real values.',
        'Device enrolment has no HTTP endpoint by design, so the harness writes buildings and device keys straight into the database.',
        'Rain is modelled per H3 cell and storms ramp up slowly. A verifier that compares a reading to its peers’ latest readings cannot follow an abrupt onset; that is a documented limit with its own scenario.',
        'The three comparison validators (Static, Radar-only, Peer-only) are small reference implementations in `citysim/behavior.go`. They are not vendor products.'),
      note('Numbers measured by the harness come from synthetic weather. They prove nothing about real-world water savings.', true),
    ],
  },
];

/* ------------------------------------------------------------------ Documentation */

const DOCS: Page[] = [
  {
    id: 'overview', title: 'Repository map', summary: 'Every subsystem, where it lives, and how finished it is.',
    blocks: [
      p('VarunaSetu is the product name; the README calls the verification prototype inside it HydroLedger, which is also the name the Go packages and migrations use.'),
      table(['Area', 'Path', 'Status'],
        ['Go verifier (core)', '`hydroledger/verifier/`', 'Implemented, tested'],
        ['Spatial / identity', '`hydroledger/verifier/spatial/`, `hydroledger/verifier/identity/`', 'Implemented, tested'],
        ['Durable service + API', '`service/`, `cmd/server/`, `migrations/`', 'Core ingest, escrow and read APIs; DB integration tests require PostGIS'],
        ['Audit ledger', '`ledger/`, `migrations/002_ledger.sql`', 'Tested library and persisted service event chain'],
        ['Allocation (research)', '`alloc/`', 'Implemented, tested (NetworkX)'],
        ['Simulation / replay (research)', '`sim/`', 'Implemented, tested Python harness'],
        ['Dashboard', '`dashboard/`', 'Implemented, tested; live backend data only, with explicit API-unavailable errors'],
        ['Virtual city + live demo', '`citysim/`, `cmd/citysim/`', 'Real signed traffic into the real service, 12 scenarios with self-checks, judge UI'],
        ['Ops', '`Dockerfile`, `docker-compose.yml`, `.env.example`', 'Config validated; runtime smoke recommended']),
      note('Integrated prototype and research codebase. Not production-ready. No Sybil-proof claim and no real-world water-savings or flood-reduction claim.', true),
    ],
  },
  {
    id: 'architecture', title: 'Architecture', summary: 'The layers, and the path a reading takes through them.',
    blocks: [
      p('A water telemetry reading must not be trusted by fixed threshold alone. Validity is evaluated using physical constraints (`R_obs`, a radar-informed bound), robust spatial peer statistics (median and MAD, Z), H3 spatial context, escrow for conflicts, and, where enabled, cryptographic device identity and auditability.'),
      table(['Layer', 'Path', 'Responsibility'],
        ['Verification', '`hydroledger/verifier`', 'Physics and radar bound, peer consensus, escrow decisions, reason codes'],
        ['Spatial', '`hydroledger/verifier/spatial`', 'H3 indexing and neighbour helpers'],
        ['Identity', '`hydroledger/verifier/identity`', 'Ed25519 keys and signatures, stateless verification; runtime lookup uses durable device records'],
        ['Durable service', '`service/`', 'Signed ingest middleware, DB-backed peer retrieval into a scratch verifier, transactional persistence, escrow lifecycle, read and aggregate APIs'],
        ['Storage', '`migrations/`', 'PostGIS-capable schema for telemetry, decisions, escrows and the ledger'],
        ['Audit ledger', '`ledger/`', 'SHA-256 hash chain, Ed25519 signed checkpoints, verification and witness interface'],
        ['Allocation', '`alloc/`', 'Python and NetworkX min-cost-flow prototype; ACCEPT-only, integer litres, explicit priority rule'],
        ['Simulation / research', '`sim/`', 'Deterministic scenarios, replay, ablation and consistency tooling; not on the runtime path'],
        ['Dashboard', '`dashboard/`', 'React and Vite: map, telemetry, escrow, ledger viewer and analytics from live API responses'],
        ['Ops', '`Dockerfile`, `docker-compose.yml`, `cmd/server`', 'Containerised PostGIS, API and optional UI dev server']),
      h('Integration flow'),
      ol('The device signs a telemetry envelope.',
        '`POST /telemetry/readings` verifies signature, enrolment, revocation, timestamp and durable idempotency.',
        'The service loads recent DB-backed peers for the H3 cell and time window and evaluates with a scratch verifier, preserving the verifier’s logic.',
        'Capacity reservation, the reading, the decision, an escrow (if provisional) and the ledger entries persist in one transaction.',
        'Escrows are corroborated or expired through durable transitions: a radar-based path and an expiry worker.',
        'The dashboard reads the list and aggregate endpoints and reports backend failures explicitly.'),
    ],
  },
  {
    id: 'setup', title: 'Setup', summary: 'Run the stack locally with Docker, or each part on its own.',
    blocks: [
      h('Prerequisites'),
      ul('Docker and Docker Compose (recommended for the integrated run)', 'Go 1.25+ or 1.26.x (the Docker build uses `golang:1.26.8`)', 'Node.js 20+ and npm, for the dashboard', 'Python 3.11+, for simulation and allocation (NetworkX is required for the `alloc` tests)'),
      h('Quick start'),
      code('cp .env.example .env   # optional; edit secrets for non-local use\ndocker compose up --build'),
      table(['Service', 'Address', 'Notes'],
        ['PostGIS', '`localhost:5432`', 'Port from `POSTGRES_PORT`'],
        ['API', '`http://localhost:8080`', '`/healthz`, `/telemetry/readings`, `/escrows`, `/cells`, `/ledger/entries`, `/analytics/summary`'],
        ['Dashboard', '`http://localhost:5173`', 'Compose profile `ui`: `docker compose --profile ui up --build`'],
        ['City simulator', '`http://localhost:8090`', 'Compose profile `demo`; standalone, with its own API and database view']),
      p('Migrations in `./migrations` are mounted into the PostGIS init directory on first-time volume initialisation. For an existing volume, apply them manually and in order: `001_service.sql`, `002_ledger.sql`, then `003_audit_event_ref_idx.sql`.'),
      h('Dashboard on its own'),
      code('cd dashboard\nnpm install\nnpm run dev\n# optional live API: VITE_API_BASE=http://localhost:8080 npm run dev'),
      h('Supabase PostgreSQL'),
      p('Run the migrations in the Supabase SQL Editor, then set `DATABASE_URL` in `.env` to the Supabase pooler or direct connection string with `sslmode=require`. The Compose API honours `DATABASE_URL` when it is set; otherwise it uses the local PostGIS container.'),
      h('Go and Python'),
      code('go test ./hydroledger/... ./ledger/...\ngo build ./cmd/server\n\npython -m unittest discover -s sim/tests -t . -v\npip install networkx\npython -m unittest discover -t . -s alloc -p \'test*.py\' -v'),
      note('If host Application Control blocks freshly built Go test binaries (Windows WDAC), run the Go tests in Docker: `docker run --rm -v "${PWD}:/src" -w /src golang:1.26.8 go test -race ./hydroledger/... ./ledger/... -count=1`.'),
    ],
  },
  {
    id: 'verification', title: 'Verification', summary: 'Definitions, order of checks, reason codes and default thresholds.',
    blocks: [
      h('Definitions'),
      ul('Effective depth: `R_obs = ClaimedLitres / (RoofAreaM2 × RunoffCoeff)` mm.',
        'Physical bound: `R_max = Alpha × RadarMM + BetaMM`.',
        'Peers of a reading: observations in the same cell from other devices, the latest per device, no older than `PeerMaxAge`.',
        'Robust score: `M = median(peers)`, `MAD = median(|x − M|)`, `Z = ZScale × |R_obs − M| / (MAD + MADFloorMM)`.',
        'Positive outlier: `Z > OutlierZ` and `R_obs > M`.',
        'Radar dry: `RadarMM ≤ RadarDryMM`. Peers rain: `M ≥ PeerRainMM`. Peers coherent: `max − min ≤ max(CoherenceAbsMM, CoherenceRelFrac × M)`.'),
      h('Order of checks'),
      ol('Field validation, then capacity. Failures return immediately and are not recorded.',
        'Collect peers. With fewer than `MinPeers`, accept on the radar bound or send to review.',
        'Otherwise, first match wins: positive outlier, then radar/peer conflict (escrow), then `R_obs > R_max`, then accept.',
        'Any reading that passed step 1 is stored as its device’s latest observation, whatever the verdict. A reading with live radar also marks older pending escrows in its cell as radar-corroborated.'),
      h('Reason codes'),
      table(['Reason', 'State', 'When'],
        ['`INVALID_INPUT`', 'QUARANTINE', 'Input validation failed.'],
        ['`STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED`', 'QUARANTINE', 'Stored litres exceed capacity, or the claim does not fit in the remaining capacity.'],
        ['`RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS`', 'ACCEPT', 'Fewer than `MinPeers` peers and `R_obs ≤ R_max`.'],
        ['`INSUFFICIENT_PEERS_REVIEW`', 'QUARANTINE', 'Fewer than `MinPeers` peers and `R_obs > R_max`.'],
        ['`SPATIAL_PEER_OUTLIER`', 'QUARANTINE', 'Enough peers and a positive outlier.'],
        ['`RADAR_PEER_CONFLICT_ESCROW`', 'PROVISIONAL_ESCROW', 'Enough peers, no outlier, radar dry, peers rain and are coherent.'],
        ['`PHYSICAL_BOUND_EXCEEDED`', 'QUARANTINE', 'Enough peers, none of the above, `R_obs > R_max`.'],
        ['`RADAR_AND_PEERS_AGREE`', 'ACCEPT', 'Enough peers, none of the above, `R_obs ≤ R_max`.'],
        ['`ESCROW_CORROBORATED`', 'ACCEPT', 'Escrow confirmed by radar or by neighbour cells.'],
        ['`ESCROW_EXPIRED_UNCORROBORATED`', 'QUARANTINE', 'Escrow older than `EscrowTTL` without corroboration.']),
      h('Default thresholds'),
      table(['Field', 'Default', 'Use'],
        ['`Alpha`', '1.15', '`R_max` slope'],
        ['`BetaMM`', '2.0', '`R_max` offset (mm)'],
        ['`MinPeers`', '4', 'Minimum eligible peers'],
        ['`PeerMaxAge`', '10 min', 'Peer eligibility; also “recent” for neighbour devices'],
        ['`WindowMaxDevices`', '128', 'Per-cell bound'],
        ['`ZScale`', '0.6745', 'Z numerator factor'],
        ['`MADFloorMM`', '0.15', 'Added to MAD in the Z divisor'],
        ['`OutlierZ`', '3.5', 'Outlier if `Z >` this'],
        ['`PeerRainMM`', '2.0', '“Peers report rain”; also the neighbour median threshold'],
        ['`CoherenceAbsMM`', '1.0', 'Coherence floor (mm)'],
        ['`CoherenceRelFrac`', '0.25', 'Coherence relative width'],
        ['`RadarDryMM`', '0.5', 'Radar is dry if `≤`; later radar corroborates if `>`'],
        ['`EscrowTTL`', '15 min', 'Escrow lifetime'],
        ['`NeighborMinCells`', '2', 'Neighbour cells needed'],
        ['`NeighborMinDevices`', '3', 'Devices needed per neighbour cell']),
    ],
  },
  {
    id: 'escrow', title: 'Escrow', summary: 'How a held reading ends: corroborated, or quarantined.',
    blocks: [
      p('An escrow is created when the verifier returns `RADAR_PEER_CONFLICT_ESCROW`. Its lifetime starts at the escrowed reading’s timestamp. In the service it is durable, with the states `PENDING`, `CORROBORATED` and `EXPIRED_QUARANTINED`.'),
      h('Resolution order'),
      ol('**Radar-corroborated.** A later reading in the same cell with live radar (above 0.5 mm), strictly later than the escrowed reading and within `EscrowTTL` of it, releases it: `ACCEPT / ESCROW_CORROBORATED`.',
        '**Expired.** If more than `EscrowTTL` has passed without corroboration: `QUARANTINE / ESCROW_EXPIRED_UNCORROBORATED`. Exactly `EscrowTTL` is still pending.',
        '**Neighbour-corroborated.** At least `NeighborMinCells` configured neighbour cells each have at least `NeighborMinDevices` devices observed recently, with a median at or above `PeerRainMM`: `ACCEPT / ESCROW_CORROBORATED`.',
        '**Otherwise** it stays pending.'),
      p('Each escrow is returned exactly once. Nothing else resolves or expires an escrow, so a sweep must run periodically. The service starts a background expiry worker that uses row locking and `SKIP LOCKED` and is safe to run after a restart.'),
      note('Terminal transitions are conditional on `state = \'PENDING\'`, so a race between corroboration and expiry resolves to exactly one terminal state.'),
    ],
  },
  {
    id: 'identity', title: 'Identity', summary: 'Ed25519 signatures, nonces, time windows and revocation.',
    blocks: [
      p('Devices sign telemetry with Ed25519. The package also provides replay protection and an enrolment and revocation registry; at runtime the service resolves enrolled keys and revocation state from durable device records.'),
      h('Signed message'),
      code('"hydroledger/identity/telemetry/v1" 0x00\nuint32(len(DeviceID)) || DeviceID\nint64(Timestamp.Unix()) || uint32(Timestamp.Nanosecond())\nuint32(len(Nonce)) || Nonce\nuint32(len(Payload)) || Payload'),
      p('The domain string separates this protocol from others. A nonce is 16 random bytes, base64url without padding (22 characters), and must be unique per device within the replay retention window.'),
      h('Verification order'),
      ol('The message is well formed: device id 1 to 128 bytes, non-zero timestamp, nonce 1 to 128 bytes, payload at most 64 KiB.',
        'The device id is enrolled.', 'The device is not revoked.', 'The Ed25519 signature verifies with the device’s public key.',
        'The timestamp is within the window: at most 5 minutes old and at most 30 seconds ahead.',
        'The `(device, nonce)` pair has not been accepted inside the retention period.'),
      h('Errors'),
      table(['Error', 'Meaning'],
        ['`ErrUnknownDevice`', 'Device id not enrolled.'], ['`ErrDeviceRevoked`', 'Device id is revoked.'],
        ['`ErrSignatureInvalid`', 'Signature does not verify.'], ['`ErrNonceReused`', '`(device, nonce)` already accepted inside retention.'],
        ['`ErrTimestampTooOld`', 'Message older than the window.'], ['`ErrTimestampTooFuture`', 'Message further ahead than the allowed skew.'],
        ['`ErrNonceInvalid`', 'Nonce empty or longer than 128 bytes.'], ['`ErrPayloadTooLarge`', 'Payload larger than 64 KiB.']),
      note('A compromised but enrolled device remains trusted until it is revoked. Enrolment authority, identity proofing and Sybil cost are out of scope.', true),
    ],
  },
  {
    id: 'api', title: 'HTTP API', summary: 'The service endpoints the dashboard and the devices use.',
    blocks: [
      table(['Method and path', 'Purpose'],
        ['`POST /telemetry/readings`', 'Signed telemetry ingest, with identity checks and durable idempotency'],
        ['`GET /telemetry/readings`, `GET /telemetry/readings/{id}`', 'List readings; fetch one'],
        ['`GET /escrows`, `GET /escrows/{id}`', 'List escrows; fetch one'],
        ['`GET /cells`, `GET /cells/{h3_index}/status`', 'Cell states; one cell (the H3 index is validated)'],
        ['`GET /ledger/entries`', 'Audit ledger entries (needs the migration 002 tables)'],
        ['`GET /analytics/summary`', 'Backend-calculated totals for the Overview screen'],
        ['`GET /healthz`', 'Liveness']),
      h('Ingest contract'),
      p('The signed payload is the exact JSON bytes the device signed. It carries `building_id`, `claimed_litres`, `roof_area_m2`, `runoff_coeff`, `stored_litres`, `capacity_litres`, `radar_mm` and the canonical `h3_index`. Validation is strict: unknown fields and trailing data are rejected.'),
      ul('Reusing a device and nonce returns the persisted result with `200`.', 'Reusing it with different payload bytes returns `409`.'),
      p('`cmd/server` listens on `8080`. Setting `SIMULATOR_ENABLED` (true in the Compose file) exposes `/simulator/*`, the device enrolment the dashboard Story uses. It is for demos only.'),
    ],
  },
  {
    id: 'ledger', title: 'Audit ledger', summary: 'An append-only SHA-256 hash chain with signed checkpoints.',
    blocks: [
      p('Each entry links to the previous entry’s hash. The ledger assigns `seq`, `prev_hash` and `hash`; callers provide `event_ref` and `timestamp`. Service events are persisted transactionally.'),
      h('Entry hash'),
      table(['Order', 'Field', 'Format'],
        ['1', '`schema`', 'literal `hydroledger.ledger.entry.v1`'], ['2', '`seq`', 'base-10 ASCII unsigned integer'],
        ['3', '`event_ref`', 'UTF-8 text; never empty; no line breaks'], ['4', '`prev_hash`', '64 lower-case hex characters; genesis is 64 zeros'],
        ['5', '`timestamp`', 'RFC 3339 with nanoseconds, in UTC']),
      p('The fields are joined by LF with no trailing newline, and `current_hash = hex_lower(SHA-256(canonical_bytes))`.'),
      h('Verification and checkpoints'),
      ul('`Verify` and `VerifyEntries` recompute every hash and check genesis linkage, contiguous sequence numbers and `prev_hash` linkage.',
        'Ed25519 checkpoints attest `(seq, head_hash, timestamp, key_id)` at a configurable interval.',
        'A `Witness` interface allows external anchoring (HSM, KMS, a transparency log).',
        'Durable tables `audit_ledger_entry` and `audit_ledger_checkpoint` are defined in `migrations/002_ledger.sql`.'),
      note('The chain detects tampering given a trusted checkpoint. A privileged full rewrite remains possible without external witnessing.', true),
    ],
  },
  {
    id: 'allocation', title: 'Allocation', summary: 'A research model that redistributes only verified water.',
    blocks: [
      p('`alloc/` is a prototype minimum-cost-flow model in Python and NetworkX. It does not claim real-world water savings or flood reduction.'),
      ul('**Supply nodes:** buildings with ACCEPT readings and verified stored volume, in integer litres. Other verdicts contribute zero.',
        '**Demand nodes:** locations needing water, each with requested litres and an explicit priority (1 is highest).',
        '**Edges:** transfer capacity in litres and a transfer cost, only between ACCEPT supplies and known demands.',
        '**Objective:** minimum total cost flow; the volume moved is the smaller of total ACCEPT supply and total demand, limited by capacities.'),
      p('Priority rule: `effective_weight = c + PRIORITY_WEIGHT × (p_d − 1)` with `PRIORITY_WEIGHT = 10_000`, so higher-priority demands are served first when costs are comparable, and cheaper transfers win within a priority.'),
    ],
  },
  {
    id: 'dashboard', title: 'Dashboard', summary: 'Screens, configuration, and the URL parameters every screen understands.',
    blocks: [
      p('The dashboard is a React 19 and Vite app. It shows live backend data only; when the API is unavailable it says so instead of showing fixtures. These documentation pages are a separate page of the same build, at `/docs/`, and the city simulator links to them too.'),
      h('URL parameters'),
      p('Routes use the hash, so any state below can be bookmarked or shared. Path parameters are positional; query parameters are optional and never appear when they equal their default. The documentation is its own page at `/docs/`, with routes of its own.'),
      table(['Pattern', 'Example', 'Effect'],
        ['`/docs/#/:section/:page`', '`/docs/#/documentation/verification`', 'Opens a documentation page. Sections: `preamble`, `how-it-works`, `constitution`, `documentation`.'],
        ['`/docs/#/constitution/:article`', '`/docs/#/constitution/iii`', 'Highlights an article by its Roman numeral.'],
        ['`?at=:heading`', '`/docs/#/documentation/verification?at=reason-codes`', 'Scrolls to a heading.'],
        ['`?q=:text`', '`/docs/#/?q=escrow`', 'Searches every documentation page.'],
        ['`#/story?scenario=`', '`#/story?scenario=inflated`', 'Preselects `honest`, `inflated` or `conflict`.'],
        ['`#/readings?q&decision&sort&dir&page`', '`#/readings?decision=QUARANTINE&sort=deviceId&dir=asc&page=2`', 'Filters, sorts and pages the readings table.'],
        ['`#/readings/:id`', '`#/readings/r1`', 'Opens one reading’s detail.'],
        ['`#/map/:cell`', '`#/map/8928308280fffff`', 'Highlights an H3 cell and lists its readings.']),
      h('Configuration'),
      table(['Variable', 'Default', 'Purpose'],
        ['`VITE_API_BASE`', '`/api`', 'Where the browser sends API requests; empty means same-origin through the dev proxy'],
        ['`VITE_PROXY_TARGET`', '`http://localhost:8080`', 'Backend the `/api` proxy forwards to'],
        ['`VITE_SIM_URL`', '`http://localhost:8090`', 'Address of the city simulator the dashboard links to']),
      h('Design'),
      p('The interface is built around the wordmark: ink on paper, square corners, hairline rules, and a headline bar over every page and card, echoing the bar that joins the letters of the logo. Tokens live in `src/index.css`; the vector wordmark is `src/brand/Wordmark.tsx`.'),
    ],
  },
  {
    id: 'citysim', title: 'City simulator', summary: 'A virtual Hyderabad that drives the real service.',
    blocks: [
      p('The judge-facing demo builds a virtual Hyderabad from 37 real H3 cells and 210 enrolled devices and drives the actual service with signed telemetry. Only the weather and the clock are simulated.'),
      code('# Option A: Docker (database + demo, one command)\ndocker compose --profile demo up --build citysim db\n# open http://localhost:8090/\n\n# Option B: your own PostGIS\nexport DATABASE_URL="postgres://USER:PASS@localhost:5432/DB?sslmode=disable"\ngo run ./cmd/citysim serve\n\n# Option C: no browser, check every scenario and write the results report\ngo run ./cmd/citysim verify -report docs/DEMO_RESULTS.md'),
      p('`citysim` applies the migrations itself and never deletes data: each run starts at a later virtual time than anything stored, so earlier runs cannot act as peers of later ones. Use a dedicated demo database anyway. The page works offline: the map is drawn from H3 cell boundaries. See the [scenarios](#/how-it-works/scenarios) and the [talk track](' + BLOB + 'docs/DEMO.md).'),
    ],
  },
  {
    id: 'testing', title: 'Testing', summary: 'The commands that verify each part.',
    blocks: [
      table(['Scope', 'Command'],
        ['Go verifier, identity, spatial, ledger', '`docker run --rm -v "$PWD:/src" -w /src golang:1.26.8 go test ./hydroledger/... ./ledger/... -count=1`'],
        ['Go race detector (core)', '`docker run --rm -v "$PWD:/src" -w /src golang:1.26.8 go test -race ./hydroledger/... ./ledger/... -count=1`'],
        ['Go build (service and commands)', '`go build ./cmd/server ./service ./ledger ./hydroledger/...`'],
        ['Python simulation', '`python -m unittest discover -s sim/tests -t . -v`'],
        ['Python allocation', '`python -m unittest discover -t . -s alloc -p \'test*.py\' -v` (needs `networkx`)'],
        ['Dashboard', '`cd dashboard && npm test`'],
        ['Dashboard build', '`cd dashboard && npm run build`'],
        ['Compose config', '`docker compose config --quiet`']),
      p('Service integration tests need a disposable PostGIS: set `HYDROLEDGER_TEST_DATABASE_URL` and run `go test ./service -count=1`. They cover migrations, idempotency, malformed requests and escrow behaviour.'),
    ],
  },
  {
    id: 'limitations', title: 'Known limits', summary: 'What VarunaSetu does not defend against, written down on purpose.',
    blocks: [
      ul('No Sybil-proof claim. Compromised enrolled device keys can sign bad data until they are revoked.',
        'The hash chain detects tampering given a trusted checkpoint; a privileged full rewrite remains possible without external witnessing.',
        'Device enrolment administration is outside the runtime API. Enrolled keys and revocation state must be provisioned in the database.',
        'Neighbour-cell durable corroboration is limited to the service’s current peer-retrieval behaviour.',
        'The radar value is device-supplied in this prototype, so a cheat that also lies about the radar can fool the radar bound.',
        'A real one-cell storm that the radar misses is not credited; it expires.',
        'Three neighbouring cells that collude can corroborate each other.',
        'An abrupt rain onset can flag honest first reporters, because peers cannot yet follow it.',
        'The dashboard never fabricates data, which also means it shows nothing when the API is down.'),
      note('This is an integrated prototype, not production-ready, and makes no claim of real-world water savings or flood reduction.', true),
    ],
  },
  {
    id: 'glossary', title: 'Glossary', summary: 'The vocabulary used across the project.',
    blocks: [
      table(['Term', 'Meaning'],
        ['H3', 'Uber’s hexagonal hierarchical spatial index. Resolution 9 cells are about 0.1 km².'],
        ['k=1 ring', 'The six cells that touch a given cell.'],
        ['Peer', 'Another device in the same cell whose latest reading is recent enough to count.'],
        ['`R_obs`', 'The rainfall depth a claim implies: litres ÷ (roof area × runoff coefficient).'],
        ['`R_max`', 'The most the radar and physics allow: `1.15 × radar + 2.0 mm`.'],
        ['MAD and Z', 'Median absolute deviation, and the robust score built on it, used to spot outliers among peers.'],
        ['Radar dry', 'Radar reports 0.5 mm or less.'],
        ['Escrow', 'A reading held while evidence disagrees, until it is corroborated or expires.'],
        ['Quarantine', 'A rejected reading, or one held for review.'],
        ['Corroboration', 'Later live radar, or rain in enough neighbouring cells, that releases an escrow.'],
        ['Idempotency', 'Sending the same signed reading again returns the recorded result instead of a second evaluation.'],
        ['Nonce', 'A single-use random value inside every signed message, to stop replays.'],
        ['Ledger', 'The hash-chained, append-only audit log of every decision.'],
        ['Checkpoint', 'An Ed25519-signed statement of the ledger’s head at a given sequence number.'],
        ['PostGIS', 'The spatial extension of PostgreSQL the service stores data in.'],
        ['Sybil attack', 'One actor posing as many devices. Not defended against here.']),
    ],
  },
  {
    id: 'sources', title: 'Sources', summary: 'The repository documents these pages are drawn from, and the generated wiki.',
    blocks: [
      p('These pages summarise the documents below. The originals are the full record, including the long engineering handoff.'),
      table(['Document', 'What it holds'],
        ['[README.md](' + BLOB + 'README.md)', 'Overview, repository map, quick start, core API'],
        ['[docs/ARCHITECTURE.md](' + BLOB + 'docs/ARCHITECTURE.md)', 'The integrated architecture and trust boundaries'],
        ['[docs/SETUP.md](' + BLOB + 'docs/SETUP.md)', 'Local and development setup'],
        ['[docs/TESTING.md](' + BLOB + 'docs/TESTING.md)', 'Verified commands and scenario status'],
        ['[docs/DEMO.md](' + BLOB + 'docs/DEMO.md)', 'The judge demo: how the H3 network works, scenarios, talk track'],
        ['[docs/DEMO_RESULTS.md](' + BLOB + 'docs/DEMO_RESULTS.md)', 'Measured demo results'],
        ['[docs/SIMULATOR.md](' + BLOB + 'docs/SIMULATOR.md)', 'The dashboard story simulator'],
        ['[docs/service.md](' + BLOB + 'docs/service.md)', 'The durable backend service'],
        ['[docs/ledger.md](' + BLOB + 'docs/ledger.md)', 'The audit ledger and signed checkpoints'],
        ['[docs/alloc.md](' + BLOB + 'docs/alloc.md)', 'The allocation prototype'],
        ['[docs/INTEGRATION_AUDIT.md](' + BLOB + 'docs/INTEGRATION_AUDIT.md)', 'Integration audit'],
        ['[docs/ERROR_LOG.md](' + BLOB + 'docs/ERROR_LOG.md)', 'Error log'],
        ['[CONTRACTS.md](' + BLOB + 'CONTRACTS.md)', 'Verifier, spatial and identity contracts'],
        ['[HANDOFF.md](' + BLOB + 'HANDOFF.md)', 'Engineering handoff'],
        ['[DeepWiki](' + DEEPWIKI + ')', 'A generated, browsable wiki of the whole codebase']),
    ],
  },
];

/* ------------------------------------------------------------------ Constitution */

export const ARTICLES: Article[] = [
  { id: 'i', numeral: 'I', title: 'Evidence, not thresholds', text: 'A telemetry reading is never trusted by a fixed threshold alone. It is weighed against physical limits, robust peer statistics, H3 spatial context and, where enabled, device identity and auditability.', source: 'ARCHITECTURE.md · Core principle', path: 'docs/ARCHITECTURE.md' },
  { id: 'ii', numeral: 'II', title: 'Locality is evidence', text: 'A claim is judged against the devices on the same few blocks, not against a citywide figure.', source: 'DEMO.md · How the H3 network works', path: 'docs/DEMO.md' },
  { id: 'iii', numeral: 'III', title: 'Disagreement is held, not guessed', text: 'When radar and peers disagree, the reading goes to escrow and the surrounding H3 cells are asked. It is neither rejected nor credited on a guess.', source: 'DEMO.md · How the H3 network works', path: 'docs/DEMO.md' },
  { id: 'iv', numeral: 'IV', title: 'Every escrow ends', text: 'A held reading reaches exactly one terminal state: corroborated, or quarantined once its lifetime (15 minutes by default) runs out. A race between corroboration and expiry resolves to one outcome.', source: 'CONTRACTS.md · Escrow lifecycle; service.md', path: 'CONTRACTS.md' },
  { id: 'v', numeral: 'V', title: 'Identity before judgement', text: 'Only enrolled, un-revoked devices whose signature, timestamp and nonce check out are heard at all. A retried message returns its recorded result instead of being evaluated again.', source: 'CONTRACTS.md · identity; service.md', path: 'CONTRACTS.md' },
  { id: 'vi', numeral: 'VI', title: 'The record resists quiet rewriting', text: 'Readings and decisions are append-only, and every decision is a hash-chained ledger entry with signed checkpoints. Tampering is detectable against a trusted checkpoint.', source: 'ledger.md; service.md', path: 'docs/ledger.md' },
  { id: 'vii', numeral: 'VII', title: 'Water is allocated only from verified supply', text: 'Allocation considers only accepted readings, in whole litres, under an explicit priority rule.', source: 'alloc.md', path: 'docs/alloc.md' },
  { id: 'viii', numeral: 'VIII', title: 'Nothing on screen is fabricated', text: 'The dashboard shows live backend data and nothing else. When the API is unavailable it says so, and never fills the gap with fixtures.', source: 'README.md; ARCHITECTURE.md', path: 'docs/ARCHITECTURE.md' },
  { id: 'ix', numeral: 'IX', title: 'Real is real; simulated is labelled', text: 'The live demo sends signed traffic to the actual service. Only the weather and the clock are simulated, and the comparison validators are small reference implementations, not vendor products.', source: 'DEMO.md · What is real and what is simulated', path: 'docs/DEMO.md' },
  { id: 'x', numeral: 'X', title: 'Limits are published', text: 'Cases the system gets wrong have their own scenarios and are shown on purpose: a one-cell cloudburst, three colluding cells, a radar lie, an abrupt onset.', source: 'DEMO.md · The scenarios', path: 'docs/DEMO.md' },
  { id: 'xi', numeral: 'XI', title: 'A prototype, said plainly', text: 'VarunaSetu is not production-ready. It does not claim to be Sybil-proof, and it makes no claim of real-world water savings or flood reduction.', source: 'README.md; ARCHITECTURE.md', path: 'README.md' },
];

/** Heading text to anchor id: "Reason codes" becomes "reason-codes" (used by ?at=). */
export const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');

export const articleUrl = (a: Article) => BLOB + a.path;

/* ------------------------------------------------------------------ Sections */

export const SECTIONS: Section[] = [
  { id: 'preamble', nav: 'Preamble', title: 'Preamble', summary: 'Why VarunaSetu exists, and what it promises.', pages: [] },
  { id: 'how-it-works', nav: 'How it works', title: 'How it works', summary: 'The pipeline, the H3 network, and the scenarios that test them.', pages: HOW },
  { id: 'constitution', nav: 'Constitution', title: 'Constitution', summary: 'Eleven articles the system holds itself to, each traced to its source document.', pages: [] },
  { id: 'documentation', nav: 'Documentation', title: 'Documentation', summary: 'Architecture, setup, verification rules, API, ledger and more.', pages: DOCS },
];

export const sectionOf = (id?: string): Section => SECTIONS.find((s) => s.id === id) ?? SECTIONS[0];

/* ------------------------------------------------------------------ Search */

const strip = (s: string) => s.replace(/\[([^\]]+)\]\([^)]+\)/g, '$1').replace(/[`*]/g, '');

function blockText(b: Block): string {
  switch (b.t) {
    case 'p': case 'h': case 'note': case 'code': return b.x;
    case 'ul': case 'ol': return b.x.join(' ');
    case 'table': return [...b.head, ...b.rows.flat()].join(' ');
    case 'steps': return b.x.map((s) => `${s.title} ${s.body}`).join(' ');
    case 'hex': return 'H3 neighbours explainer';
  }
}

export interface Hit { path: string[]; title: string; where: string; snippet: string }

export function searchDocs(query: string): Hit[] {
  const terms = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (!terms.length) return [];
  const hits: Hit[] = [];
  const consider = (path: string[], title: string, where: string, text: string) => {
    const plain = strip(text);
    const low = plain.toLowerCase();
    if (!terms.every((t) => low.includes(t))) return;
    const at = Math.max(0, low.indexOf(terms[0]) - 60);
    hits.push({ path, title, where, snippet: `${at > 0 ? '…' : ''}${plain.slice(at, at + 170)}…` });
  };
  for (const s of SECTIONS) for (const pg of s.pages) consider([s.id, pg.id], pg.title, s.nav, `${pg.title} ${pg.summary} ${pg.blocks.map(blockText).join(' ')}`);
  for (const a of ARTICLES) consider(['constitution', a.id], `Article ${a.numeral}. ${a.title}`, 'Constitution', `${a.title} ${a.text}`);
  return hits;
}
