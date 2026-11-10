package citysim

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"hydroledger-prototype/service"
)

// Probe is a scripted API-level request: an attack on the identity layer or a
// malformed/impossible report. Expected outcomes are the documented ones.
type Probe struct {
	At   time.Duration
	Kind string
}

type probeSpec struct {
	Title        string
	What         string
	ExpectStatus int
	ExpectReason string
	ContentOK    bool // a system that only judges the numbers would accept it
}

var probeSpecs = map[string]probeSpec{
	"REPLAY": {"Replayed message", "Exactly the same signed envelope is sent twice (a retry, or a captured packet).",
		200, "", true},
	"NONCE_REUSE": {"Same nonce, different numbers", "A validly signed message re-uses an earlier nonce with a bigger claim.",
		409, "", true},
	"TAMPER": {"Tampered in transit", "A signed message has its litres changed after signing.",
		401, "", true},
	"UNKNOWN_DEVICE": {"Device that was never enrolled", "A well-formed, correctly signed message from a key nobody enrolled.",
		401, "", true},
	"REVOKED": {"Revoked device", "A device whose key has been revoked keeps sending valid-looking readings.",
		401, "", true},
	"STALE": {"Stale message (30 min old)", "A correctly signed message outside the 5-minute acceptance window.",
		401, "", true},
	"FUTURE": {"Clock in the future (+10 min)", "A correctly signed message timestamped ahead of the server clock.",
		401, "", true},
	"BAD_PAYLOAD": {"Unknown field in payload", "Payload carries a field the contract does not define.",
		400, "", false},
	"INVALID_H3": {"Invalid H3 index", "The reading names a cell id that is not a canonical H3 cell.",
		400, "", false},
	"UNKNOWN_BUILDING": {"Unknown building", "A reading for a building that does not exist.",
		404, "", false},
	"INVALID_FIELDS": {"Impossible roof (area 0)", "Zero roof area: the implied rainfall depth is undefined.",
		422, "INVALID_INPUT", false},
	"NEGATIVE_CLAIM": {"Negative litres", "A negative claim.",
		422, "INVALID_INPUT", false},
	"TANK_OVERFLOW": {"Claim larger than the tank can hold", "Claimed litres exceed the free space in the tank.",
		422, "STORAGE_OR_CATCHMENT_CAPACITY_EXCEEDED", false},
}

func (r *Runner) probeBase() (*Building, service.Telemetry) {
	b := r.World.ByCell[r.World.CenterCell][0].Building
	return b, service.Telemetry{
		BuildingID: b.ID, ClaimedLitres: 1, RoofAreaM2: b.RoofM2, RunoffCoeff: b.Runoff,
		StoredLitres: 0, CapacityLitres: b.TankL, RadarMM: 0.5, CellID: b.CellID,
	}
}

func (r *Runner) runProbe(ctx context.Context, p Probe, at time.Duration) ProbeResult {
	spec := probeSpecs[p.Kind]
	pr := ProbeResult{Kind: p.Kind, Title: spec.Title, What: spec.What, SimMin: at.Minutes(),
		ExpectStatus: spec.ExpectStatus, ExpectReason: spec.ExpectReason}
	if spec.ContentOK {
		t := true
		pr.ContentOnlyOK = &t
	}
	now := r.Clock.Now()
	_, tel := r.probeBase()
	ok := probeKey(ProbeOKDevice)
	n := func(s string) string { return fmt.Sprintf("probe-%s-%s-%s", r.tag, p.Kind, s) }
	var res PostResult
	switch p.Kind {
	case "REPLAY":
		w, _ := Sign(ok, now, n("a"), tel)
		first := r.API.Post(ctx, w)
		res = r.API.Post(ctx, w)
		r.reqs += 2
		pr.Extra = fmt.Sprintf("first send: HTTP %d (new), second send: HTTP %d (stored result returned, nothing re-credited)", first.Status, res.Status)
		if first.Status != 201 || res.ID != first.ID {
			pr.Extra += " [UNEXPECTED: ids differ]"
			res.Status = -1
		}
	case "NONCE_REUSE":
		w1, _ := Sign(ok, now, n("a"), tel)
		r.API.Post(ctx, w1)
		bigger := tel
		bigger.ClaimedLitres = 5
		w2, _ := Sign(ok, now, n("a"), bigger)
		res = r.API.Post(ctx, w2)
		r.reqs += 2
	case "TAMPER":
		w, _ := Sign(ok, now, n("a"), tel)
		forged := tel
		forged.ClaimedLitres = 900
		w.Payload, _ = json.Marshal(forged)
		res = r.API.Post(ctx, w)
		r.reqs++
	case "UNKNOWN_DEVICE":
		w, _ := Sign(probeKey(ProbeGhostDevice), now, n("a"), tel)
		res = r.API.Post(ctx, w)
		r.reqs++
	case "REVOKED":
		_ = Revoke(ctx, r.DB, ProbeRevokedDevice)
		w, _ := Sign(probeKey(ProbeRevokedDevice), now, n("a"), tel)
		res = r.API.Post(ctx, w)
		r.reqs++
	case "STALE":
		w, _ := Sign(ok, now.Add(-30*time.Minute), n("a"), tel)
		res = r.API.Post(ctx, w)
		r.reqs++
	case "FUTURE":
		w, _ := Sign(ok, now.Add(10*time.Minute), n("a"), tel)
		res = r.API.Post(ctx, w)
		r.reqs++
	case "BAD_PAYLOAD":
		w, _ := SignRaw(ok, now, n("a"), []byte(`{"building_id":"x","claimed_liters":5}`))
		res = r.API.Post(ctx, w)
		r.reqs++
	case "INVALID_H3":
		bad := tel
		bad.CellID = "not-an-h3-cell"
		w, _ := Sign(ok, now, n("a"), bad)
		res = r.API.Post(ctx, w)
		r.reqs++
	case "UNKNOWN_BUILDING":
		bad := tel
		bad.BuildingID = "bld-does-not-exist"
		w, _ := Sign(ok, now, n("a"), bad)
		res = r.API.Post(ctx, w)
		r.reqs++
	case "INVALID_FIELDS":
		bad := tel
		bad.RoofAreaM2 = 0
		w, _ := Sign(ok, now, n("a"), bad)
		res = r.API.Post(ctx, w)
		r.reqs++
	case "NEGATIVE_CLAIM":
		bad := tel
		bad.ClaimedLitres = -5
		w, _ := Sign(ok, now, n("a"), bad)
		res = r.API.Post(ctx, w)
		r.reqs++
	case "TANK_OVERFLOW":
		bad := tel
		bad.ClaimedLitres = tel.CapacityLitres * 1.5
		w, _ := Sign(ok, now, n("a"), bad)
		res = r.API.Post(ctx, w)
		r.reqs++
	}
	pr.Status, pr.LatencyMs, pr.Message = res.Status, res.LatencyMs, res.Error
	if res.Decision != nil {
		pr.Reason = res.Decision.Reason
	}
	pr.Pass = pr.Status == spec.ExpectStatus && (spec.ExpectReason == "" || strings.EqualFold(pr.Reason, spec.ExpectReason))
	return pr
}
