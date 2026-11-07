package verifier

import (
	"cmp"
	"slices"
	"time"
)

// SetNeighbors configures which cells may corroborate escrows held in cellID.
// It replaces any earlier list for cellID. The relation is directed (listing B
// as a neighbor of A says nothing about A being a neighbor of B). Empty ids,
// duplicates and cellID itself are dropped. No geometry (such as H3) is
// involved: callers supply the neighbor lists.
func (v *Verifier) SetNeighbors(cellID string, neighbors []string) {
	if cellID == "" {
		return
	}
	out := make([]string, 0, len(neighbors))
	for _, n := range neighbors {
		if n == "" || n == cellID || slices.Contains(out, n) {
			continue
		}
		out = append(out, n)
	}
	v.nbMu.Lock()
	if len(out) == 0 {
		delete(v.neighbors, cellID)
	} else {
		v.neighbors[cellID] = out
	}
	v.nbMu.Unlock()
}

// PendingEscrows returns the number of escrows not yet resolved.
func (v *Verifier) PendingEscrows() int {
	n := 0
	for _, c := range v.cellsSnapshot() {
		c.mu.Lock()
		n += len(c.escrows)
		c.mu.Unlock()
	}
	return n
}

func (v *Verifier) cellsSnapshot() []*cell {
	v.cellsMu.RLock()
	cs := make([]*cell, 0, len(v.cells))
	for _, c := range v.cells {
		cs = append(cs, c)
	}
	v.cellsMu.RUnlock()
	return cs
}

// corroborateByRadar marks pending escrows confirmed by a radar reading taken
// at ts: the reading must be strictly later than the escrowed one and within
// the TTL. c.mu must be held.
func (c *cell) corroborateByRadar(ts time.Time, ttl time.Duration) {
	for i := range c.escrows {
		e := &c.escrows[i]
		if e.radarOK {
			continue
		}
		if d := ts.Sub(e.ts); d > 0 && d <= ttl {
			e.radarOK = true
		}
	}
}

// ResolveEscrows resolves pending escrows as of now and returns the ones that
// were resolved, ordered by cell, reading time, then device. Each escrow is
// returned exactly once; unresolved escrows stay pending.
//
// For each escrow whose reading is not later than now:
//   - corroborated by a later radar reading above Config.RadarDryMM seen by
//     Evaluate within the TTL: ACCEPT / ESCROW_CORROBORATED (even if now is past
//     the TTL, because the radar evidence arrived in time);
//   - otherwise, if now - reading time > Config.EscrowTTL:
//     QUARANTINE / ESCROW_EXPIRED_UNCORROBORATED;
//   - otherwise, if at least Config.NeighborMinCells configured neighbor cells
//     each have at least Config.NeighborMinDevices devices observed within
//     Config.PeerMaxAge of now with a median >= Config.PeerRainMM:
//     ACCEPT / ESCROW_CORROBORATED;
//   - otherwise the escrow stays pending.
//
// Callers must call this periodically; nothing else resolves or expires escrows.
func (v *Verifier) ResolveEscrows(now time.Time) []Resolution {
	now = now.Round(0)
	v.sweepMu.Lock()
	defer v.sweepMu.Unlock()

	var out []Resolution
	for _, c := range v.cellsSnapshot() {
		c.mu.Lock()
		pending := len(c.escrows)
		c.mu.Unlock()
		if pending == 0 {
			continue
		}
		// Neighbor cells are inspected without holding c.mu, and never while
		// holding another cell's lock, so no two cell locks are ever nested.
		nbOK := v.neighborsCorroborate(c.id, now)

		c.mu.Lock()
		kept := c.escrows[:0]
		for _, e := range c.escrows {
			if e.ts.After(now) {
				kept = append(kept, e)
				continue
			}
			res := Resolution{DeviceID: e.device, CellID: c.id, ReadingTimestamp: e.ts, ResolvedAt: now}
			switch {
			case e.radarOK, now.Sub(e.ts) <= v.cfg.EscrowTTL && nbOK:
				res.State, res.Reason = StateAccept, ReasonEscrowCorroborated
			case now.Sub(e.ts) > v.cfg.EscrowTTL:
				res.State, res.Reason = StateQuarantine, ReasonEscrowExpired
			default:
				kept = append(kept, e)
				continue
			}
			out = append(out, res)
		}
		clear(c.escrows[len(kept):]) // drop references held by the tail
		c.escrows = kept
		c.mu.Unlock()
	}

	slices.SortFunc(out, func(a, b Resolution) int {
		return cmp.Or(
			cmp.Compare(a.CellID, b.CellID),
			a.ReadingTimestamp.Compare(b.ReadingTimestamp),
			cmp.Compare(a.DeviceID, b.DeviceID),
		)
	})
	return out
}

// neighborsCorroborate reports whether enough configured neighbors of cellID
// show rain as of now.
func (v *Verifier) neighborsCorroborate(cellID string, now time.Time) bool {
	cfg := &v.cfg
	v.nbMu.RLock()
	ns := v.neighbors[cellID]
	v.nbMu.RUnlock()
	if len(ns) < cfg.NeighborMinCells {
		return false
	}

	sc := v.scratch.Get().(*scratch)
	defer v.scratch.Put(sc)

	qualifying := 0
	for _, id := range ns {
		c := v.lookupCell(id)
		if c == nil {
			continue
		}
		vals := sc.a[:0]
		c.mu.Lock()
		for i := range c.obs {
			if now.Sub(c.obs[i].ts) <= cfg.PeerMaxAge {
				vals = append(vals, c.obs[i].depthMM)
			}
		}
		c.mu.Unlock()
		sc.a = vals
		if len(vals) < cfg.NeighborMinDevices {
			continue
		}
		slices.Sort(vals)
		if medianSorted(vals) >= cfg.PeerRainMM {
			qualifying++
			if qualifying >= cfg.NeighborMinCells {
				return true
			}
		}
	}
	return false
}
