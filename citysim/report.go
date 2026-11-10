package citysim

import (
	"fmt"
	"strings"
	"time"
)

// Report renders a Markdown record of a verification run: every number in it
// was produced by driving the real service, and is reproduced by
// `go run ./cmd/citysim verify -report docs/DEMO_RESULTS.md`.
func Report(w *World, results []*Result) string {
	var b strings.Builder
	pct := func(v float64) string {
		if v < 0 {
			return "—"
		}
		return fmt.Sprintf("%.1f%%", v*100)
	}
	fmt.Fprintf(&b, "# VarunaSetu virtual-city results\n\n")
	fmt.Fprintf(&b, "Generated %s by `citysim verify`. The virtual city has **%d H3 cells** (resolution %d) around Hyderabad (%.4f, %.4f) and **%d enrolled devices**. ", time.Now().UTC().Format("2006-01-02 15:04 UTC"), len(w.Cells), w.Resolution, w.Lat, w.Lng, len(w.Devices))
	b.WriteString("Every reading below is an Ed25519-signed `POST /telemetry/readings` to the real service (verifier, identity, PostgreSQL/PostGIS persistence, hash-chained ledger). Only the weather and the clock are simulated.\n\n")
	b.WriteString("Validators compared: **Static** (R_obs ≤ 10 mm), **Radar-only** (R_obs ≤ 1.15·radar + 2 mm), **Peer-only** (robust z-score vs. same-cell peers), and **VarunaSetu** (the live service, including escrow resolution by radar and H3 neighbours). The first three are reference baselines implemented in `citysim/behavior.go`, not vendor products. *Balanced* = mean of (fraud caught) and (honest readings credited). In scenarios with no fraud, a validator that accepts everything scores 100% by construction, so those rows measure only the false-reject cost.\n\n")
	b.WriteString("## Summary\n\n| Scenario | Readings | Static | Radar-only | Peer-only | **VarunaSetu** | Checks |\n|---|---:|---:|---:|---:|---:|:---|\n")
	for _, r := range results {
		m := map[string]Confusion{}
		for _, c := range r.Scoreboard {
			m[c.Validator] = c
		}
		lim := ""
		if r.Limitation {
			lim = " *(documented limit)*"
		}
		ok := 0
		for _, c := range r.Checks {
			if c.Pass {
				ok++
			}
		}
		status := "✅"
		if !r.Pass {
			status = "❌"
		}
		fmt.Fprintf(&b, "| %s%s | %d | %s | %s | %s | **%s** | %s %d/%d |\n", r.Title, lim, len(r.Outcomes),
			pct(m["STATIC_THRESHOLD"].Balanced), pct(m["RADAR_ONLY"].Balanced), pct(m["PEER_ONLY"].Balanced), pct(m["VARUNASETU"].Balanced), status, ok, len(r.Checks))
	}
	for _, r := range results {
		var scn *Scenario
		for _, s := range Scenarios(w) {
			if s.ID == r.Scenario {
				scn = s
			}
		}
		fmt.Fprintf(&b, "\n## %s\n\n", r.Title)
		if scn != nil {
			fmt.Fprintf(&b, "*%s*\n\n", scn.Tagline)
			if scn.Limitation {
				b.WriteString("> **Documented limit.** This scenario shows something VarunaSetu does *not* solve; it is included on purpose.\n\n")
			}
			fmt.Fprintf(&b, "**Situation.** %s\n\n**Simple validators.** %s\n\n**VarunaSetu.** %s\n\n", scn.Situation, scn.Existing, scn.Varuna)
		}
		fmt.Fprintf(&b, "%d readings, %d HTTP requests, %.1f s wall time.\n\n", len(r.Outcomes), r.Requests, float64(r.WallMs)/1000)
		b.WriteString("| Validator | Fraud caught | Honest credited | Balanced |\n|---|---:|---:|---:|\n")
		for _, c := range r.Scoreboard {
			fmt.Fprintf(&b, "| %s | %d/%d | %d/%d | %s |\n", c.Validator, c.Caught, c.Caught+c.FalseAccepts, c.Credited, c.Credited+c.FalseRejects, pct(c.Balanced))
		}
		b.WriteString("\n**Reason codes returned by the API:** ")
		var parts []string
		for _, k := range SortedReasons(r.Reasons) {
			parts = append(parts, fmt.Sprintf("`%s` %d", k, r.Reasons[k]))
		}
		b.WriteString(strings.Join(parts, ", "))
		fmt.Fprintf(&b, "\n\n**Escrows settled:** radar %d, H3 neighbours %d, expired %d. **Ledger:** %d entries re-verified: %v.\n\n", r.Escrows["RADAR"], r.Escrows["H3_NEIGHBORS"], r.Escrows["EXPIRED"], r.Ledger.Entries, r.Ledger.Verified)
		b.WriteString("**Self-checks**\n\n")
		for _, c := range r.Checks {
			mark := "✅"
			if !c.Pass {
				mark = "❌"
			}
			fmt.Fprintf(&b, "- %s %s — %s\n", mark, c.Name, c.Detail)
		}
		if len(r.Probes) > 0 {
			b.WriteString("\n| Hostile request | Expected | Got | Numbers-only system |\n|---|---|---|---|\n")
			for _, p := range r.Probes {
				exp, got := fmt.Sprint(p.ExpectStatus), fmt.Sprint(p.Status)
				if p.ExpectReason != "" {
					exp += " " + p.ExpectReason
				}
				if p.Reason != "" {
					got += " " + p.Reason
				}
				ok := "—"
				if p.ContentOnlyOK != nil && *p.ContentOnlyOK {
					ok = "would accept"
				}
				mark := "✅"
				if !p.Pass {
					mark = "❌"
				}
				fmt.Fprintf(&b, "| %s %s | %s | %s | %s |\n", mark, p.Title, exp, got, ok)
			}
		}
	}
	return b.String()
}
