<div align="center">

<img src="banner.svg" alt="VarunaSetu — Zero-Trust Spatial-Physics Oracle and Cryptographic Ledger for Urban Water Resilience" width="100%">

<br>

**Verifiable rooftop-rainwater telemetry: every claim is authenticated, physically bounded, cross-checked against its H3 neighbourhood, and written to a tamper-evident ledger before it can earn credit.**

<br>

[![Status](https://img.shields.io/badge/status-research%20prototype-f59e0b?style=for-the-badge)](#security-model--known-limitations)
[![Go](https://img.shields.io/badge/Go-verifier%20%26%20service-00ADD8?style=for-the-badge&logo=go&logoColor=white)](#repository-structure)
[![React](https://img.shields.io/badge/React-dashboard-61DAFB?style=for-the-badge&logo=react&logoColor=0b1f33)](#operations-dashboard)
[![PostGIS](https://img.shields.io/badge/PostgreSQL-PostGIS%2016-336791?style=for-the-badge&logo=postgresql&logoColor=white)](#data-layer)
[![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?style=for-the-badge&logo=docker&logoColor=white)](#getting-started)

[**Project site**](https://varunasetu.umeshyenugula2007.workers.dev) &nbsp;·&nbsp; [**DeepWiki**](https://deepwiki.com/umeshyenugula/VarunaSetu) &nbsp;·&nbsp; [**Architecture**](docs/ARCHITECTURE.md) &nbsp;·&nbsp; [**Demo guide**](docs/DEMO.md) &nbsp;·&nbsp; [**Measured results**](docs/DEMO_RESULTS.md)

</div>

> [!IMPORTANT]
> **Status — integrated prototype / research codebase. Not production-ready.**
> VarunaSetu makes no Sybil-resistance claim and no real-world water-savings or flood-reduction claim. Its limits are documented and reproduced on purpose; see [Security model & known limitations](#security-model--known-limitations).
>
> A mitigation path is planned for each known limitation, but **none is implemented or tested yet**. Each will need testing before any claim is made. See [Planned mitigations (not yet tested)](#planned-mitigations-not-yet-tested).

---

## Table of contents

- [Overview](#overview)
- [Highlights](#highlights)
- [System architecture](#system-architecture)
- [Life of a reading](#life-of-a-reading)
- [Verification engine](#verification-engine)
- [Escrow lifecycle](#escrow-lifecycle)
- [Device identity & replay protection](#device-identity--replay-protection)
- [Audit ledger](#audit-ledger)
- [Data layer](#data-layer)
- [HTTP API](#http-api)
- [Operations dashboard](#operations-dashboard)
- [CitySim — the virtual city](#citysim--the-virtual-city)
- [Water allocation (research)](#water-allocation-research)
- [Repository structure](#repository-structure)
- [Getting started](#getting-started)
- [Testing](#testing)
- [Deployment topology](#deployment-topology)
- [Security model & known limitations](#security-model--known-limitations)
- [Planned mitigations (not yet tested)](#planned-mitigations-not-yet-tested)
- [Documentation](#documentation)
- [Glossary](#glossary)

---

## Overview

Cities that incentivise rooftop rainwater harvesting need to know whether a household's reported harvest is **real**. A fixed cap is trivially gamed, a radar-only check rejects honest people whenever the radar is blind, and a peer-only check is fooled by a coordinated neighbourhood.

**VarunaSetu** (*Varuna* — the Vedic guardian of waters; *Setu* — bridge) bridges that gap. It treats every telemetry claim as untrusted input and only credits it after four independent lines of evidence agree:

| Line of evidence | Question it answers | Mechanism |
| --- | --- | --- |
| **Identity** | Who signed this, and is it fresh? | Ed25519 signatures, enrolment + revocation, timestamp window, per-device nonce replay protection |
| **Physics** | Can this roof physically have caught that much? | Radar-derived upper bound `R_max = 1.15 · radar + 2 mm`, capacity checks |
| **Spatial consensus** | Do neighbours on the same H3 cell see the same sky? | Median/MAD robust z-score over peers in the last 10 minutes |
| **Escrow + neighbours** | Radar and peers disagree — who is right? | Hold in provisional escrow; release via late radar or corroborating H3 neighbour cells, otherwise expire |

Everything that survives is persisted transactionally in PostGIS and appended to a **SHA-256 hash-chained audit ledger** with Ed25519-signed checkpoints.

<div align="center">
<img src="/pipeline.svg" alt="One reading, seven checkpoints: sign, authenticate, verify, decide, persist, chain, observe" width="100%">
</div>

## Highlights

- **Physics-informed verification** — a stateless Go library (`hydroledger/verifier`) combining a radar physical bound with robust median/MAD statistics.
- **Three explicit outcomes** — `ACCEPT`, `PROVISIONAL_ESCROW`, `QUARANTINE`, each with a machine-readable reason code.
- **H3 spatial consensus** — resolution-9 hexagonal cells give locality-aware peer groups and neighbour corroboration.
- **Escrow instead of rejection** — real rain under a radar blind spot is held, not thrown away, and released by evidence.
- **Zero-trust ingest** — signed envelopes, device enrolment/revocation read from the database, durable idempotency.
- **Tamper-evident audit trail** — canonical-serialised, hash-chained ledger written in the same transaction as the decision.
- **A city you can watch** — CitySim, a virtual Hyderabad, drives the *real* service with signed traffic across 12 scenarios to show what city adoption looks like, including four where VarunaSetu is *documented to fail*.
- **Live data only** — the dashboard shows backend projections or an explicit error; it never fabricates readings.

---

## System architecture

The runtime path connects device-signed readings to the Go service, which delegates decisions to the verifier library and persists through PostGIS-backed repository operations. The dashboard only consumes read APIs. The `alloc/` prototype is intentionally standalone.

```mermaid
flowchart TB
    classDef edge fill:#e0f2fe,stroke:#0284c7,color:#0c4a6e,stroke-width:1.5px
    classDef svc fill:#dcfce7,stroke:#16a34a,color:#14532d,stroke-width:1.5px
    classDef core fill:#fef3c7,stroke:#d97706,color:#78350f,stroke-width:1.5px
    classDef data fill:#ede9fe,stroke:#7c3aed,color:#3b0764,stroke-width:1.5px
    classDef ui fill:#fce7f3,stroke:#db2777,color:#831843,stroke-width:1.5px
    classDef lab fill:#f1f5f9,stroke:#64748b,color:#0f172a,stroke-width:1.5px,stroke-dasharray:4 3

    subgraph EDGE["Edge · rooftop devices"]
        DEV["Metered rooftop tank<br/>holds an Ed25519 device key"]:::edge
    end

    subgraph SVC["Backend service · Go · cmd/server + service/"]
        MW["Signed-ingest middleware<br/>identity checks + durable idempotency"]:::svc
        API["HTTP API<br/>ingest + dashboard read endpoints"]:::svc
        SWP["Escrow sweeper<br/>neighbour corroboration + expiry"]:::svc
    end

    subgraph CORE["Verification core · hydroledger/verifier"]
        IDN["identity<br/>Ed25519 · registry · replay window"]:::core
        DEC["Decision engine<br/>physical bound + median/MAD z-score"]:::core
        SPA["spatial<br/>H3 cells + neighbour rings"]:::core
        ESC["Escrow state machine<br/>radar / neighbour release, TTL expiry"]:::core
    end

    subgraph DATA["Durable state"]
        PG[("PostgreSQL + PostGIS<br/>building · device · telemetry_reading<br/>verification_decision · escrow_record")]:::data
        LED["Audit ledger<br/>SHA-256 hash chain + Ed25519 checkpoints"]:::data
    end

    subgraph OBS["Observation"]
        DASH["React dashboard<br/>Operations view + Story Mode"]:::ui
    end

    subgraph LAB["Demonstration and research"]
        CITY["CitySim · virtual Hyderabad<br/>37 H3 cells · 210 devices · 12 scenarios"]:::lab
        SIM["sim/ · Python replay + ablation"]:::lab
        ALLOC["alloc/ · NetworkX min-cost flow<br/>standalone, ACCEPT-only"]:::lab
    end

    DEV -->|"POST /telemetry/readings"| MW
    MW --> IDN
    MW --> API
    API --> DEC
    DEC --> SPA
    DEC --> ESC
    API -->|"one transaction"| PG
    API -->|"append event"| LED
    LED --> PG
    SWP --> ESC
    SWP --> PG
    DASH -->|"GET readings · escrows · cells<br/>ledger · analytics · healthz"| API
    CITY -->|"real signed traffic"| MW
```

| Subsystem | Path | Responsibility |
| --- | --- | --- |
| **Verification core** | `hydroledger/verifier/` | Physical bound, robust statistics, reason codes, escrow state machine |
| ↳ Spatial | `hydroledger/verifier/spatial/` | H3 indexing and neighbour rings |
| ↳ Identity | `hydroledger/verifier/identity/` | Ed25519 envelopes, registry, revocation, replay protection |
| **Backend service** | `service/`, `cmd/server/` | HTTP API, signed-ingest middleware, repository transactions, escrow sweeper |
| **Database** | `migrations/` | PostGIS schema, immutability triggers, ledger tables, retention function |
| **Audit ledger** | `ledger/` | Canonical hash chain and signed checkpoints |
| **Dashboard** | `dashboard/` | Vite + React operations views, H3 map, interactive Story Mode |
| **Virtual city** | `citysim/`, `cmd/citysim/` | Adoption demo: a virtual Hyderabad with 12 self-checking scenarios driving the real service |
| **Research** | `sim/`, `alloc/` | Python replay/ablation harness and min-cost-flow allocation |

---

## Life of a reading

The service authenticates a signed request, loads recent database-backed peers for the H3/time window, evaluates the reading with a **scratch verifier** (keeping persistence concerns out of decision logic), then persists the result and related records together.

```mermaid
sequenceDiagram
    autonumber
    participant D as Device
    participant M as Signed-ingest middleware
    participant R as Repository · PostGIS
    participant V as Scratch verifier
    participant L as Audit ledger

    D->>M: POST /telemetry/readings (Ed25519 envelope)
    M->>R: Load device public key + revocation state
    R-->>M: enrolled key, not revoked
    M->>M: Check signature, timestamp window, nonce
    alt replay with identical payload
        M-->>D: 200 with the stored result (idempotent)
    else forged / unenrolled / revoked / stale
        M-->>D: 401
    else nonce reused with different numbers
        M-->>D: 409
    end
    M->>R: Load recent peers for the H3 cell and time window
    R-->>M: peer observations
    M->>V: Evaluate(reading, peers)
    V-->>M: Decision = state + reason code
    rect rgb(237, 233, 254)
        Note over M,L: single database transaction
        M->>R: Reserve capacity (not for QUARANTINE)
        M->>R: Insert reading + decision
        opt state = PROVISIONAL_ESCROW
            M->>R: Insert escrow_record (PENDING)
        end
        M->>L: Append hash-chained event
        L->>R: Persist ledger entry
    end
    M-->>D: Decision + reason code
```

| Stage | Implementation | Notes |
| --- | --- | --- |
| Ingest | `POST /telemetry/readings` | Signed-ingest middleware + durable idempotency |
| Identity | `hydroledger/verifier/identity` + `device` table | Signature, enrolment, revocation, timestamp, replay |
| Verify | `hydroledger/verifier` | Peers for the H3/time window evaluated on a scratch verifier |
| Persist | `service/` + PostGIS | Reading, decision, capacity reservation, and escrow (if provisional) written transactionally |
| Escrow | Sweeper + service transitions | Corroborate (radar / H3 neighbours) or expire |
| Ledger | `ledger/` | Ingest, escrow corroboration and escrow expiry events appended transactionally |
| Dashboard | Read APIs + `dashboard/` | Persisted projections only |

---

## Verification engine

`Verifier.Evaluate()` is a thread-safe, stateless-by-contract library call that turns one `Reading` into one `Decision`.

```mermaid
flowchart TD
    classDef step fill:#e0f2fe,stroke:#0284c7,color:#0c4a6e
    classDef ok fill:#dcfce7,stroke:#16a34a,color:#14532d,stroke-width:2px
    classDef esc fill:#fef3c7,stroke:#d97706,color:#78350f,stroke-width:2px
    classDef bad fill:#fee2e2,stroke:#dc2626,color:#7f1d1d,stroke-width:2px
    classDef q fill:#f8fafc,stroke:#475569,color:#0f172a

    S(["Reading arrives"]):::step --> V1["Validate fields<br/>finite, in physical range"]:::step
    V1 -->|"invalid"| Q0["QUARANTINE<br/>INVALID_INPUT"]:::bad
    V1 --> OBS["R_obs = claimed litres / roof area × runoff coeff<br/>R_max = 1.15 × radar + 2.0 mm"]:::step
    OBS --> CAP{"Within tank capacity<br/>and catchment?"}:::q
    CAP -->|"no"| Q1["QUARANTINE<br/>CAPACITY_EXCEEDED"]:::bad
    CAP -->|"yes"| PEERS{"At least 4 peers<br/>in the last 10 min?"}:::q

    PEERS -->|"no · radar-physics fallback"| FB{"R_obs ≤ R_max ?"}:::q
    FB -->|"yes"| A1["ACCEPT<br/>RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS"]:::ok
    FB -->|"no"| Q2["QUARANTINE<br/>INSUFFICIENT_PEERS_REVIEW"]:::bad

    PEERS -->|"yes"| STAT["Median M and MAD of peers<br/>Z = 0.6745 × abs(R_obs − M) / (MAD + 0.15)"]:::step
    STAT --> OUT{"Z > 3.5<br/>and R_obs > M ?"}:::q
    OUT -->|"yes"| Q3["QUARANTINE<br/>SPATIAL_PEER_OUTLIER"]:::bad
    OUT -->|"no"| CONF{"Radar dry ≤ 0.5 mm, but<br/>coherent peers report ≥ 2.0 mm ?"}:::q
    CONF -->|"yes"| E1["PROVISIONAL_ESCROW<br/>RADAR_PEER_CONFLICT_ESCROW"]:::esc
    CONF -->|"no"| PB{"R_obs ≤ R_max ?"}:::q
    PB -->|"no"| Q4["QUARANTINE<br/>PHYSICAL_BOUND_EXCEEDED"]:::bad
    PB -->|"yes"| A2["ACCEPT<br/>RADAR_AND_PEERS_AGREE"]:::ok
```

### Decision states

| State | Meaning | Earns credit? | Reserves capacity? |
| --- | --- | :---: | :---: |
| `ACCEPT` | Physics and peers agree (or physics alone, when peers are too few) | Yes | Yes |
| `PROVISIONAL_ESCROW` | Radar says dry while a coherent peer group reports rain; held pending corroboration | After release | Yes |
| `QUARANTINE` | Fails capacity, physical bound, or statistical-outlier checks | No | No |

### Reason codes

| Code | Outcome | Trigger |
| --- | --- | --- |
| `RADAR_AND_PEERS_AGREE` | ACCEPT | Within the radar bound and consistent with peers |
| `RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS` | ACCEPT | Fewer than `MinPeers`; within the radar physical bound |
| `INSUFFICIENT_PEERS_REVIEW` | QUARANTINE | Fewer than `MinPeers`; above the physical bound |
| `SPATIAL_PEER_OUTLIER` | QUARANTINE | Positive outlier versus same-cell peers |
| `PHYSICAL_BOUND_EXCEEDED` | QUARANTINE | Above `R_max` |
| `CAPACITY_EXCEEDED` / `STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED` | QUARANTINE / 422 | Claim larger than the tank or catchment can hold |
| `RADAR_PEER_CONFLICT_ESCROW` | PROVISIONAL_ESCROW | Dry radar versus coherent raining peers |
| `ESCROW_CORROBORATED` | ACCEPT | Escrow released by radar or H3 neighbours |
| `ESCROW_EXPIRED_UNCORROBORATED` | QUARANTINE | Escrow TTL elapsed without corroboration |
| `INVALID_INPUT` | 422 | Non-finite, negative, or impossible input |

### Tunable parameters (`verifier.Config`)

| Parameter | Default | Purpose |
| --- | :---: | --- |
| `Alpha` / `BetaMM` | `1.15` / `2.0 mm` | Physical bound `R_max = Alpha · radar + BetaMM` |
| `MinPeers` | `4` | Minimum peers for robust statistics |
| `PeerMaxAge` | `10m` | Age limit for peer observations |
| `WindowMaxDevices` | `128` | Per-cell device window capacity |
| `ZScale` / `MADFloorMM` / `OutlierZ` | `0.6745` / `0.15` / `3.5` | Robust z-score and outlier threshold |
| `PeerRainMM` | `2.0 mm` | Median depth at which peers "report rain" |
| `CoherenceAbsMM` / `CoherenceRelFrac` | `1.0 mm` / `0.25` | Peer-coherence spread limits |
| `RadarDryMM` | `0.5 mm` | Radar at or below this counts as dry |
| `EscrowTTL` | `15m` | Lifetime of a provisional escrow |
| `NeighborMinCells` / `NeighborMinDevices` | `2` / `3` | Neighbour corroboration quorum |

---

## Escrow lifecycle

Radar can be wrong in both directions. Rather than reject honest households under a radar blind spot, VarunaSetu parks the claim and lets evidence arrive. Nothing resolves an escrow by itself: the service's expiry worker drives `ResolveEscrows`.

```mermaid
stateDiagram-v2
    direction LR

    [*] --> PENDING: PROVISIONAL_ESCROW<br/>radar dry, coherent peers report rain

    PENDING --> CORROBORATED: radar later reads above 0.5 mm<br/>within the TTL (via RADAR)
    PENDING --> CORROBORATED: at least 2 neighbour cells, each with 3+ devices,<br/>report median ≥ 2.0 mm (via H3_NEIGHBORS)
    PENDING --> EXPIRED_QUARANTINED: 15 min TTL elapsed,<br/>no corroboration

    CORROBORATED --> [*]: ACCEPT · ESCROW_CORROBORATED<br/>credit released
    EXPIRED_QUARANTINED --> [*]: QUARANTINE · ESCROW_EXPIRED_UNCORROBORATED<br/>no credit

    note right of PENDING
        Same evidence, opposite truth:
        a real storm has raining neighbours,
        a staged claim does not.
    end note
```

> A quarantined claim never reserves capacity, and escrow transitions are persisted and ledgered transactionally.

---

## Device identity & replay protection

Every telemetry message is an Ed25519-signed envelope. The signature covers a domain separator, the device ID, the timestamp, a nonce and the payload — so tampering with *any* of them invalidates it.

```mermaid
flowchart LR
    classDef step fill:#e0f2fe,stroke:#0284c7,color:#0c4a6e
    classDef ok fill:#dcfce7,stroke:#16a34a,color:#14532d,stroke-width:2px
    classDef bad fill:#fee2e2,stroke:#dc2626,color:#7f1d1d

    IN(["Signed envelope"]):::step --> K{"Device enrolled?"}:::step
    K -->|"no"| E1["ErrUnknownDevice<br/>401"]:::bad
    K -->|"yes"| RV{"Revoked?"}:::step
    RV -->|"yes"| E2["ErrDeviceRevoked<br/>401"]:::bad
    RV -->|"no"| SG{"Signature valid?"}:::step
    SG -->|"no"| E3["ErrSignatureInvalid<br/>401"]:::bad
    SG -->|"yes"| TS{"Timestamp within<br/>5 min old / 30 s future?"}:::step
    TS -->|"no"| E4["ErrTimestampTooOld / TooFuture<br/>401"]:::bad
    TS -->|"yes"| NC{"Nonce seen for<br/>this device?"}:::step
    NC -->|"yes"| E5["ErrNonceReused<br/>409 or stored result"]:::bad
    NC -->|"no"| OKK["Authenticated → verifier"]:::ok
```

- **Revocation is checked before the signature**, so a revoked key is useless even with a valid signature.
- Nonces are scoped **per device** and retained for `Window + FutureSkew`; concurrent replays of one envelope accept exactly one.
- Revocation is permanent and idempotent: a compromised key cannot be re-enrolled.

### Hostile-message matrix (all verified by CitySim)

| Hostile request | Expected | Result |
| --- | :---: | :---: |
| Replayed message | `200` stored result | ✅ |
| Same nonce, different numbers | `409` | ✅ |
| Tampered in transit | `401` | ✅ |
| Device never enrolled | `401` | ✅ |
| Revoked device | `401` | ✅ |
| Stale message (30 min old) | `401` | ✅ |
| Clock in the future (+10 min) | `401` | ✅ |
| Unknown field in payload | `400` | ✅ |
| Invalid H3 index | `400` | ✅ |
| Unknown building | `404` | ✅ |
| Impossible roof (area 0) / negative litres | `422 INVALID_INPUT` | ✅ |
| Claim larger than the tank can hold | `422 STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED` | ✅ |

---

## Audit ledger

Every ingest, escrow corroboration and escrow expiry appends an entry to an **append-only SHA-256 hash chain** in the same transaction as the state change. Checkpoints signed with Ed25519 let a verifier detect truncation or rollback without replaying history.

```mermaid
flowchart LR
    classDef blk fill:#ede9fe,stroke:#7c3aed,color:#3b0764,stroke-width:1.5px
    classDef chk fill:#fef3c7,stroke:#d97706,color:#78350f,stroke-width:1.5px
    classDef key fill:#dcfce7,stroke:#16a34a,color:#14532d,stroke-width:1.5px

    G["seq 0 · genesis<br/>prev_hash = 64 zeros<br/>entry_hash = H0"]:::blk
    E1["seq 1<br/>prev_hash = H0<br/>entry_hash = H1"]:::blk
    E2["seq 2<br/>prev_hash = H1<br/>entry_hash = H2"]:::blk
    EN["seq n<br/>prev_hash = H(n-1)<br/>entry_hash = Hn"]:::blk

    G -->|"H0"| E1 -->|"H1"| E2 -.->|"…"| EN

    EN --> CP["Checkpoint<br/>schema · seq · head_hash<br/>timestamp · key_id"]:::chk
    KEY["Ed25519 signing key<br/>HSM / KMS in production"]:::key -->|"signs"| CP
    CP -->|"Witness.Publish"| W[("External witness")]:::key
```

**Canonical entry** — five fields joined by `\n` (no trailing newline), then hashed with SHA-256 and rendered as lower-case hex:

| # | Field | Format |
| :-: | --- | --- |
| 1 | `schema` | `hydroledger.ledger.entry.v1` |
| 2 | `seq` | Base-10 unsigned integer |
| 3 | `event_ref` | UTF-8, no `\n` / `\r` |
| 4 | `prev_hash` | 64 lower-case hex chars (64 zeros for genesis) |
| 5 | `timestamp` | RFC 3339 Nano, UTC |

`Ledger.Verify()` recomputes every hash and checks sequence continuity and linkage (`ErrHashMismatch`, `ErrPrevHashMismatch`). Checkpoints use the schema `hydroledger.ledger.checkpoint.v1`.

> [!NOTE]
> The chain **detects** tampering against a trusted checkpoint. A privileged administrator who rewrites the *entire* chain is only stopped by external witnessing — see [limitations](#security-model--known-limitations). The dashboard's ledger view is a persisted projection; authoritative cryptographic verification is client-side, not a production server endpoint.

---

## Data layer

PostgreSQL with PostGIS and `pgcrypto`, defined as additive migrations that initialise automatically in the Compose database container.

```mermaid
flowchart TB
    classDef mig fill:#ede9fe,stroke:#7c3aed,color:#3b0764,stroke-width:1.5px
    classDef item fill:#f8fafc,stroke:#64748b,color:#0f172a

    subgraph M1["migrations/001_service.sql · core service"]
        direction TB
        X1["Extensions<br/>postgis · pgcrypto"]:::item
        X2["Enums<br/>decision_state: ACCEPT · PROVISIONAL_ESCROW · QUARANTINE<br/>escrow_state: PENDING · CORROBORATED · EXPIRED_QUARANTINED"]:::item
        X3["Tables<br/>building · device · telemetry_reading<br/>verification_decision · escrow_record · ledger_entry"]:::item
        X4["Append-only triggers<br/>prevent_telemetry_mutation()<br/>prevent_decision_mutation()"]:::item
        X5["Retention<br/>purge_telemetry_readings(interval)"]:::item
        X1 --> X2 --> X3 --> X4 --> X5
    end

    subgraph M2["migrations/002_ledger.sql · audit ledger"]
        direction TB
        L1["audit_ledger_entry<br/>seq PK · event_ref · prev_hash · entry_hash<br/>hex-checked hashes · index on event_time"]:::item
        L2["audit_ledger_checkpoint<br/>Ed25519 signature · witness reference<br/>FK to audit_ledger_entry(seq)"]:::item
        L1 --> L2
    end

    subgraph M3["migrations/003_audit_event_ref_idx.sql · performance"]
        I1["Index on audit event_ref<br/>faster escrow joins under load"]:::item
    end

    M1 --> M2 --> M3
    class M1,M2,M3 mig
```

- **Immutability** — readings and verified decisions cannot be mutated once written.
- **Retention** — `purge_telemetry_readings(retention interval)` implements sliding-window pruning.
- **Supabase** — run the migrations in the Supabase SQL editor and point `DATABASE_URL` at the pooler or direct string with `sslmode=require`.

---

## HTTP API

| Method | Endpoint | Purpose |
| :-: | --- | --- |
| `POST` | `/telemetry/readings` | **Signed** telemetry ingest — identity checks + durable idempotency |
| `GET` | `/telemetry/readings` · `/telemetry/readings/{id}` | List / fetch persisted readings with decisions |
| `GET` | `/escrows` · `/escrows/{id}` | List / fetch escrows |
| `GET` | `/cells` · `/cells/{h3_index}/status` | Per-H3-cell summaries and status |
| `GET` | `/ledger/entries` | Persisted hash-chain projection (needs migration `002`) |
| `GET` | `/analytics/summary` | Aggregate analytics for the dashboard |
| `GET` | `/healthz` | Liveness / health |
| — | `/simulator/*` | Device-enrolment routes for Story Mode, only when `SIMULATOR_ENABLED=true` |

**Status codes** — `200` accepted or stored idempotent result · `400` malformed payload · `401` authentication failure · `404` unknown building · `409` nonce conflict or capacity-reservation collision · `422` impossible input or capacity exceeded.

`cmd/server` resolves enrolled device keys and revocation state from the database. Device enrolment administration is **not** an HTTP workflow; provisioning is external to the runtime API.

---

## Operations dashboard

A Vite + React application with two modes:

| Mode | What it shows |
| --- | --- |
| **Operations** (`#operations`) | Live readings, escrows with per-second countdowns to expiry, H3 cell map (Leaflet + `h3-js`), ledger entries and analytics — straight from the service read APIs |
| **Story Mode** | An interactive simulator that walks through how H3 neighbourhoods separate a real storm from a staged claim |

If the backend is unreachable the UI reports it explicitly — it **never** substitutes fixture data.

```mermaid
flowchart LR
    classDef ui fill:#fce7f3,stroke:#db2777,color:#831843
    classDef api fill:#dcfce7,stroke:#16a34a,color:#14532d

    APP["App.tsx<br/>story ⇄ operations tabs"]:::ui --> CL["api/client.ts<br/>typed fetch wrappers"]:::ui
    CL -->|"listReadings → /telemetry/readings?limit=500"| S["Go service"]:::api
    CL -->|"listEscrows → /escrows?limit=500"| S
    CL -->|"listCells → /cells?limit=500"| S
    CL -->|"listLedger → /ledger/entries?limit=500"| S
    CL -->|"getAnalytics → /analytics/summary"| S
    APP --> MAP["H3Map<br/>Leaflet + h3-js boundaries"]:::ui
    APP --> CD["useCountdown<br/>escrow expiry timers"]:::ui
```

<!--
  SCREENSHOTS — drop real captures into docs/assets/ and uncomment:

  <p align="center">
    <img src="docs/assets/dashboard-operations.png" alt="Operations dashboard" width="48%">
    <img src="docs/assets/story-mode.png" alt="Story Mode simulator" width="48%">
  </p>
-->

---

## CitySim — the virtual city

**CitySim is a virtual city built to demonstrate what it looks like when a real city adopts VarunaSetu.** Instead of asking people to imagine the system running across a city, CitySim runs it: a virtual **Hyderabad** of **37 real H3 cells (resolution 9)** and **210 enrolled households** sends Ed25519-signed telemetry to the **actual service**, through a full day of storms, a radar outage, cheating households and attacks on the API.

| Real city adoption | In CitySim |
| --- | --- |
| Households fit metered rooftop tanks with enrolled, key-holding devices | 210 virtual devices enrolled in the real `device` table, each signing its own messages |
| Weather and rain radar | Simulated weather and radar, including blind spots and outages |
| Time passes | A virtual clock, so a two-hour storm runs in seconds |
| Honest households, cheaters, colluders, attackers | Scripted behaviours, including staged storms and forged or replayed messages |
| **The VarunaSetu service** (identity, verifier, escrow, PostGIS, ledger, dashboard) | **The same, unmodified: nothing here is mocked** |

The people, the sky and the clock are simulated; **the system under demonstration is real.** That is what makes the results credible as a picture of adoption, and it is also the boundary of the claim: CitySim shows how the system behaves, not what a real deployment would save. It makes no real-world water-savings or flood-reduction claim.

To make the value visible, each scenario also runs three simple validators on identical traffic: **Static** (`R_obs ≤ 10 mm`), **Radar-only** and **Peer-only** (robust z-score), next to **VarunaSetu** (the live service, including escrow resolution). The first three are reference baselines in `citysim/behavior.go`, not vendor products.

<div align="center">
<img src="/benchmark.svg" alt="Balanced accuracy of static, radar-only, peer-only and VarunaSetu validators in two CitySim scenarios" width="100%">
</div>

### All 12 scenarios — balanced accuracy

*Balanced = mean of (fraud caught) and (honest readings credited). Generated by `citysim verify` on 2026-11-10; every scenario passes its own self-checks.*

| Scenario | Readings | Static | Radar-only | Peer-only | **VarunaSetu** |
| --- | --: | --: | --: | --: | --: |
| A storm crosses the city *(control)* | 1,955 | 100.0% | 100.0% | 99.8% | **99.8%** |
| Two neighbours inflate their harvest | 2,231 | 50.0% | 66.7% | 87.5% | **87.5%** |
| The radar goes blind over the city centre | 2,550 | 100.0% | 87.3% | 99.8% | **98.1%** |
| The radar blinks, then comes back | 652 | 100.0% | 95.9% | 99.5% | **97.4%** |
| Same evidence, opposite truth | 2,311 | 50.0% | 91.2% | 50.0% | **98.3%** |
| A cell with too few neighbours to compare | 2,357 | 75.0% | 90.0% | 50.0% | **90.0%** |
| Attacks on the door, not the data | 0 | — | — | — | **13 / 13 handled** |
| A day in the virtual city *(everything at once)* | 3,313 | 53.9% | 88.3% | 60.8% | **94.1%** |
| A real cloudburst nobody else can see ⚠️ | 78 | 100.0% | 37.2% | 94.9% | **32.1%** |
| Three neighbouring cells collude ⚠️ | 102 | 0.0% | 100.0% | 0.0% | **25.5%** |
| A cheat that also lies about the radar ⚠️ | 2,580 | 75.0% | 50.0% | 50.0% | **50.0%** |
| Rain starts faster than peers can follow ⚠️ | 1,680 | 100.0% | 100.0% | 95.6% | **95.6%** |

> ⚠️ **Documented limits.** These four scenarios are included *on purpose* to show what VarunaSetu does **not** solve. In each, the self-check asserts that the limit reproduces. They are discussed under [limitations](#security-model--known-limitations).

### What the headline scenarios demonstrate

- **Same evidence, opposite truth** — with the radar down city-wide, a real western storm and a staged far-away claim look identical at device level. Both go to escrow; the real storm's cells have raining k-ring neighbours (345 escrows released via `H3_NEIGHBORS`) while the staged cell's escrows expire (70 expired). Result: 42/42 fake claims blocked, 2,190/2,269 honest readings credited.
- **The radar goes blind** — radar-only rejects 324 honest readings; VarunaSetu escrows 278 of them and credits all 278 once neighbours confirm.
- **The radar blinks, then comes back** — the first later reading with a live radar value in the same cell releases every older pending escrow (`ESCROW_CORROBORATED` via `RADAR`); nothing expires.
- **Sparse cells** — with fewer than 4 peers the verifier degrades gracefully to the radar physical bound instead of guessing.

### Run it

```bash
export DATABASE_URL="postgres://USER:PASS@localhost:5432/DB?sslmode=disable"   # PostGIS

go run ./cmd/citysim serve     # judge UI → http://127.0.0.1:8090/
go run ./cmd/citysim verify    # headless: run every scenario and check it
```

Guide and talk track: [`docs/DEMO.md`](docs/DEMO.md) · Measured results: [`docs/DEMO_RESULTS.md`](docs/DEMO_RESULTS.md)

---

## Water allocation (research)

`alloc/` is a **standalone** Python/NetworkX prototype that redistributes captured water under conservation, capacity and priority constraints. It consumes only supplies backed by an `ACCEPT` decision and is **not** exposed through the service API.

```mermaid
flowchart LR
    classDef src fill:#e0f2fe,stroke:#0284c7,color:#0c4a6e
    classDef sup fill:#dcfce7,stroke:#16a34a,color:#14532d
    classDef dem fill:#fef3c7,stroke:#d97706,color:#78350f
    classDef x fill:#f1f5f9,stroke:#64748b,color:#334155,stroke-dasharray:4 3

    SRC(["__source__"]):::src
    S1["S1 · 50 L<br/>ACCEPT"]:::sup
    S2["S2 · 30 L<br/>ACCEPT"]:::sup
    S3["S3 · 20 L<br/>not ACCEPT<br/>excluded"]:::x
    D1["D1 · 40 L<br/>priority 1"]:::dem
    D2["D2 · 35 L<br/>priority 2"]:::dem
    D3["D3 · 25 L<br/>priority 2"]:::dem
    SNK(["__sink__"]):::src

    SRC --> S1 & S2
    SRC -.-x S3
    S1 -->|"capacity, cost"| D1
    S1 -->|"capacity, cost"| D2
    S2 -->|"capacity, cost"| D1
    S2 -->|"capacity, cost"| D3
    D1 & D2 & D3 --> SNK
```

Priority is enforced without a multi-objective solver: each transfer edge to a demand of priority *p* is weighted

```
effective_weight = transfer_cost + 10 000 × (p − 1)
```

so `nx.min_cost_flow()` satisfies priority-1 demand first. Tests (50 randomised iterations) assert water conservation, ACCEPT-only sourcing, capacity bounds and integer integrity. This is allocation logic only — it makes no real-world savings claim and has not been field-validated.

---

## Repository structure

```text
VarunaSetu/
├── hydroledger/verifier/      # Verification core (Go)
│   ├── spatial/               #   H3 indexing + neighbour rings
│   └── identity/              #   Ed25519 envelopes, registry, revocation, replay window
├── service/                   # HTTP API, middleware, repository, escrow sweeper (Go)
├── cmd/
│   ├── server/                #   hydroledger-server entrypoint
│   └── citysim/               #   CitySim runner: `serve` and `verify`
├── citysim/                   # Virtual city, scenarios, baseline validators, judge UI
├── ledger/                    # SHA-256 hash chain + Ed25519 checkpoints
├── migrations/                # 001_service · 002_ledger · 003_audit_event_ref_idx (SQL)
├── dashboard/                 # Vite + React operations dashboard
├── alloc/                     # NetworkX min-cost-flow allocation (Python)
├── sim/                       # Replay / ablation research harness (Python)
├── docs/                      # Setup, testing, architecture, audit, demo, ledger, alloc
├── CONTRACTS.md               # Interface contracts
├── HANDOFF.md                 # Engineering handoff and known gaps
├── Dockerfile · Dockerfile.citysim
├── docker-compose.yml
└── go.mod · go.sum
```

| Area | Path | Status |
| --- | --- | --- |
| Go verifier (core) | `hydroledger/verifier/` | Implemented, tested |
| Spatial / identity | `…/spatial/`, `…/identity/` | Implemented, tested |
| Durable service + API | `service/`, `cmd/server/`, `migrations/` | Implemented ingest / escrow / read APIs; DB integration tests need PostGIS |
| Audit ledger | `ledger/`, `migrations/002_ledger.sql` | Implemented and tested; persisted service event chain |
| Allocation (research) | `alloc/` | Implemented, tested (NetworkX) |
| Simulation / replay (research) | `sim/` | Implemented, tested Python harness |
| Dashboard | `dashboard/` | Implemented, tested; live backend data only |
| Virtual city + live demo | `citysim/`, `cmd/citysim/` | Real signed traffic, 12 self-checking scenarios, judge UI |
| Ops | `Dockerfile`, `docker-compose.yml`, `.env.example` | Config validated; runtime smoke recommended |

---

## Getting started

### Prerequisites

| Tool | Needed for |
| --- | --- |
| Docker + Docker Compose | The quickest path: PostGIS, API, dashboard, CitySim |
| Go (see `go.mod`) | Running the service, CitySim and Go tests natively |
| Node.js 20+ | Dashboard development and tests |
| Python 3 + `networkx` | `sim/` and `alloc/` research code |

### Quick start (Docker Compose)

```bash
cp .env.example .env
docker compose up --build                    # PostGIS + Go API on :8080

# optional: operations dashboard on :5173
docker compose --profile ui up --build

# optional: judge demo (CitySim) on :8090
docker compose --profile demo up --build citysim
```

### Compose profiles

| Service | Image / build | Port | Profile |
| --- | --- | :-: | --- |
| `db` | `postgis/postgis:16-3.4`, mounts `./migrations` for first-run init | `5432` | default |
| `api` | Root `Dockerfile` → `hydroledger-server` | `8080` | default |
| `dashboard` | Node, Vite dev server | `5173` | `ui` |
| `citysim` | `citysim` build target, virtual clock | `8090` | `demo` |

### Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `DATABASE_URL` | local compose DB | PostgreSQL/PostGIS connection string (use `sslmode=require` for Supabase) |
| `HTTP_ADDR` | `:8080` | API listen address |
| `CORS_ALLOW_ORIGIN` | `http://localhost:5173` | Allowed browser origin |
| `SIMULATOR_ENABLED` | `true` | Enables `/simulator/*` enrolment routes for Story Mode |
| `VITE_API_BASE` | `http://localhost:8080` | API base URL used by the dashboard |
| `POSTGRES_USER` · `POSTGRES_PASSWORD` · `POSTGRES_DB` | — | Local PostGIS credentials |
| `API_PORT` · `DASHBOARD_PORT` · `CITYSIM_PORT` · `POSTGRES_PORT` | — | Host port overrides |

### Using Supabase instead of the local database

1. Run `migrations/001_service.sql`, `002_ledger.sql` and `003_audit_event_ref_idx.sql` in the Supabase SQL editor.
2. Set `DATABASE_URL` to the Supabase pooler or direct connection string with `sslmode=require`.
3. Start the API as usual.

Full instructions: [`docs/SETUP.md`](docs/SETUP.md).

---

## Testing

```bash
# Go core + ledger (Docker, WDAC-safe)
docker run --rm -v "$PWD:/src" -w /src golang:1.26.8 \
  go test -race ./hydroledger/... ./ledger/... -count=1

# Service DB integration tests (disposable PostGIS required)
HYDROLEDGER_TEST_DATABASE_URL=... go test ./service -count=1

# Dashboard
cd dashboard && npm test && npm run build

# Simulation harness
python -m unittest discover -s sim/tests -t . -v

# Allocation
docker run --rm -v "$PWD:/src" -w /src python:3.14-slim sh -lc \
  "pip install -q networkx && python -m unittest discover -t . -s alloc -p 'test*.py' -v"

# End-to-end: run every virtual-city scenario against the real service
go run ./cmd/citysim verify
```

On Windows hosts where Application Control blocks freshly compiled Go test binaries, run the suites inside Docker as shown above. More detail in [`docs/TESTING.md`](docs/TESTING.md).

---

## Deployment topology

Multi-stage builds compile static Go binaries (`CGO_ENABLED=0`) and ship them on **distroless, non-root** images with no shell or package manager.

```mermaid
flowchart TB
    classDef build fill:#e0f2fe,stroke:#0284c7,color:#0c4a6e
    classDef img fill:#ede9fe,stroke:#7c3aed,color:#3b0764
    classDef svc fill:#dcfce7,stroke:#16a34a,color:#14532d
    classDef opt fill:#fef3c7,stroke:#d97706,color:#78350f,stroke-dasharray:4 3
    classDef ext fill:#f1f5f9,stroke:#64748b,color:#0f172a

    subgraph BUILD["Docker build · golang:1.26.8"]
        B1["go build ./cmd/server<br/>→ /out/hydroledger-server"]:::build
        B2["go build ./cmd/citysim<br/>→ /out/citysim"]:::build
    end

    subgraph IMG["Runtime images · distroless static nonroot"]
        I1["server target"]:::img
        I2["citysim target · :8090"]:::img
    end

    B1 --> I1
    B2 --> I2

    subgraph COMPOSE["docker-compose.yml"]
        DB[("db · postgis 16-3.4 · :5432<br/>./migrations → initdb.d")]:::svc
        API["api · :8080<br/>healthcheck: -healthcheck"]:::svc
        CS["citysim · :8090<br/>profile: demo"]:::opt
        UI["dashboard · Vite :5173<br/>profile: ui"]:::opt
    end

    I1 --> API
    I2 --> CS
    DB -->|"service_healthy"| API
    UI -->|"VITE_API_BASE"| API
    API -.->|"or DATABASE_URL"| SB[("Supabase PostgreSQL<br/>sslmode=require")]:::ext
```

---

## Security model & known limitations

VarunaSetu is explicit about what it does and does not guarantee.

| Boundary | What holds | What does **not** hold |
| --- | --- | --- |
| **Device identity** | Forged, tampered, replayed, stale, unenrolled and revoked messages are rejected | **Not Sybil-resistant.** A compromised, validly enrolled key can sign abusive readings until it is revoked |
| **Radar input** | Physical bound and capacity checks on every reading | `radar_mm` travels *inside* the signed payload, so a compromised device can set it. The planned fix is a server-side radar feed keyed by H3 cell |
| **Spatial consensus** | Separates real storms from staged claims; degrades to physics with sparse peers | **H3 gives locality, not trust** — three mutually adjacent colluding cells can corroborate each other (reproduced: 25.5% balanced accuracy) |
| **Hyper-local rain** | Fails safe: uncorroborated escrows expire | A genuine one-cell cloudburst the radar misses is not credited (honest users lose credit by design) |
| **Abrupt onset** | — | Peers describe the previous window, so the first reporters of a sudden downpour can be flagged as outliers (74 of 1,680 honest readings in the scenario) |
| **Audit ledger** | Detects modification against a trusted checkpoint | A privileged full-chain rewrite is not prevented without an **external witness**; production needs HSM/KMS key custody |
| **Enrolment** | Keys and revocation read from the `device` table | Enrolment administration is outside the HTTP API; provisioning is external |
| **Allocation** | Conservation, capacity and priority invariants tested | `alloc/` is a mathematical prototype, not field-validated, and not wired to the service API |

Related reading: [`HANDOFF.md`](HANDOFF.md) · [`docs/INTEGRATION_AUDIT.md`](docs/INTEGRATION_AUDIT.md) · [`docs/ERROR_LOG.md`](docs/ERROR_LOG.md)

---

## Planned mitigations (not yet tested)

> [!WARNING]
> Everything in this section is **design intent**. None of it is implemented, and none of it has been tested or measured. The measured results above describe the current prototype only.

Each known limitation has a planned way to close it. The *Basis* column says whether the direction is already recorded in the project docs or is a proposal that still needs design work.

| Limitation (reproduced in CitySim) | Planned mitigation | Basis | Status |
| --- | --- | --- | :---: |
| **Radar is device-supplied.** A compromised device can set `radar_mm` to fit its inflated claim | Replace the device-reported value with a **server-side radar feed keyed by H3 cell**, so the physical bound no longer depends on the claimant | Recorded in `HANDOFF.md` as the intended fix | Not started |
| **Full ledger rewrite by a privileged admin** | Publish signed checkpoints to an **independent external witness** (the `Witness` interface already exists) and keep signing keys in an **HSM/KMS** | Recorded in `docs/ledger.md` as a production requirement | Not started |
| **Colluding adjacent cells corroborate each other** (25.5% balanced accuracy) | Make neighbour corroboration depend on *independent* evidence: require a live server-side radar signal or trusted reference stations alongside the neighbour quorum, and weight neighbours by device history rather than counting them equally | Proposed | Not started |
| **Compromised enrolled key (no Sybil resistance)** | Detect abusive devices faster and revoke them: per-device behaviour tracking across scenarios, plus an enrolment/revocation admin workflow around the existing revocation list | Proposed (enrolment admin is documented as out of scope today) | Not started |
| **Genuine hyper-local cloudburst is not credited** | Add independent local evidence (higher-resolution radar or trusted rain gauges) as a third corroboration route, so uncorroborated honest escrows can still be released | Proposed | Not started |
| **Abrupt rain onset flags honest early reporters** | Make peer comparison onset-aware, for example by letting the radar trend or a shorter peer window relax the outlier test during rapid change | Proposed | Not started |
| **Allocation model is not field-validated** | Validate `alloc/` against real distribution data before any savings claim, and only then consider exposing it through the API | Proposed | Not started |

### How each mitigation will be judged

[CitySim](#citysim--the-virtual-city) was built to demonstrate city adoption, not to test fixes. It is still a useful yardstick: its four documented-limit scenarios (collusion, radar spoofing, hyper-local rain, abrupt onset) each currently assert that the limit **reproduces**. A mitigation would count as working once its scenario is changed to assert the attack is **blocked**, while the control, radar-outage and "day in the virtual city" scenarios keep passing.

Two items cannot be judged that way:

- **Ledger witnessing and key custody** belong in the `ledger/` package tests (for example, a rewritten chain checked against a trusted external checkpoint).
- **Allocation** needs real distribution data. A simulation cannot validate real-world savings.

Mitigations that add new inputs (a server-side radar feed, rain gauges) would also require CitySim to simulate those inputs. Until all of this is done, treat every limitation above as open.

---

## Documentation

| Resource | What you will find |
| --- | --- |
| [`docs/SETUP.md`](docs/SETUP.md) | Environment, Compose profiles, Supabase, local runs |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | System topology and the ingest-to-dashboard pipeline |
| [`docs/TESTING.md`](docs/TESTING.md) | Test suites and harnesses |
| [`docs/DEMO.md`](docs/DEMO.md) | Judge demo guide and talk track |
| [`docs/DEMO_RESULTS.md`](docs/DEMO_RESULTS.md) | Measured CitySim results for all 12 scenarios |
| [`docs/INTEGRATION_AUDIT.md`](docs/INTEGRATION_AUDIT.md) | Integration audit findings |
| [`docs/ERROR_LOG.md`](docs/ERROR_LOG.md) | Stabilisation log |
| [`docs/ledger.md`](docs/ledger.md) | Ledger canonical format, checkpoints, trust assumptions |
| [`docs/alloc.md`](docs/alloc.md) | Allocation model and worked example |
| [`CONTRACTS.md`](CONTRACTS.md) | Verifier interface contracts |
| [`HANDOFF.md`](HANDOFF.md) | Engineering handoff and known gaps |
| [**DeepWiki**](https://deepwiki.com/umeshyenugula/VarunaSetu) | AI-generated deep dives into every subsystem |

**DeepWiki deep dives:**
[Architecture & Data Flow](https://deepwiki.com/umeshyenugula/VarunaSetu/1.2-architecture-and-data-flow) ·
[Decision Engine](https://deepwiki.com/umeshyenugula/VarunaSetu/2.1-decision-engine-and-statistics) ·
[Escrow State Machine](https://deepwiki.com/umeshyenugula/VarunaSetu/2.2-escrow-state-machine) ·
[Spatial Indexing](https://deepwiki.com/umeshyenugula/VarunaSetu/2.3-spatial-indexing-(h3)) ·
[Device Identity](https://deepwiki.com/umeshyenugula/VarunaSetu/2.4-device-identity-and-replay-protection) ·
[Backend Service](https://deepwiki.com/umeshyenugula/VarunaSetu/3-backend-service) ·
[Audit Ledger](https://deepwiki.com/umeshyenugula/VarunaSetu/4.2-audit-ledger-and-checkpoints) ·
[Dashboard](https://deepwiki.com/umeshyenugula/VarunaSetu/5-dashboard-frontend) ·
[CitySim](https://deepwiki.com/umeshyenugula/VarunaSetu/6.1-citysim-virtual-city) ·
[Deployment & Limitations](https://deepwiki.com/umeshyenugula/VarunaSetu/7.2-deployment-containers-and-known-limitations)

---

## Glossary

| Term | Meaning |
| --- | --- |
| **H3** | Uber's hexagonal hierarchical spatial index; VarunaSetu uses resolution 9 cells |
| **k-ring neighbours** | The H3 cells surrounding a given cell, used for escrow corroboration |
| **`R_obs` / `R_max`** | Observed rooftop depth (mm) versus the radar-derived physical upper bound |
| **MAD** | Median absolute deviation — a robust measure of spread resistant to outliers |
| **Robust z-score** | `0.6745 · abs(R_obs − median) / (MAD + floor)` |
| **Escrow** | A provisional hold on a claim whose radar and peer evidence conflict |
| **Corroboration** | Release of an escrow by a later live radar value or by raining neighbour cells |
| **Quarantine** | Terminal rejection: no credit and no capacity reserved |
| **Witness** | An external party that stores ledger checkpoints so a full rewrite is detectable |
| **Sybil attack** | Many fake identities controlled by one actor — *not* defended against here |

---

<div align="center">

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="/logo-light.png">
  <img src="/logo.png" alt="VarunaSetu logo" width="320">
</picture>

A bridge between what devices claim and what the sky and the neighbourhood can confirm.

Built by [**Umesh Yenugula**](https://github.com/umeshyenugula) · Research prototype · No licence file is currently published

</div>
