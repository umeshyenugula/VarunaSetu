// Command citysim runs the VarunaSetu virtual city.
//
//	citysim verify   run every scenario headless against the real service and check the results
//	citysim serve    start the live judge-facing demo (map, API log, scoreboards)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"hydroledger-prototype/citysim"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	db := fs.String("db", os.Getenv("DATABASE_URL"), "PostgreSQL/PostGIS connection URL (default $DATABASE_URL)")
	addr := fs.String("addr", "127.0.0.1:8080", "listen address of the service (and UI)")
	only := fs.String("only", "", "verify: comma-separated scenario ids")
	out := fs.String("out", "", "verify: write the full JSON report to this file")
	report := fs.String("report", "", "verify: write a Markdown results report to this file")
	res := fs.Int("res", 9, "H3 resolution of the city")
	radius := fs.Int("radius", 3, "city radius in H3 rings")
	_ = fs.Parse(os.Args[2:])
	if *db == "" {
		fmt.Fprintln(os.Stderr, "citysim: set DATABASE_URL or pass -db")
		os.Exit(2)
	}
	ctx := context.Background()
	switch cmd {
	case "verify":
		os.Exit(verify(ctx, *db, *only, *out, *report, *res, *radius))
	case "serve":
		serve(ctx, *db, *addr, *res, *radius)
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: citysim verify|serve [flags]")
	os.Exit(2)
}

func verify(ctx context.Context, db, only, out, report string, res, radius int) int {
	st, err := citysim.StartStack(ctx, db, "127.0.0.1:0", citysim.WorldConfig{Resolution: res, Radius: radius}, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start:", err)
		return 1
	}
	defer st.Close()
	fmt.Printf("virtual city: %d H3 cells (res %d), %d devices, API at %s\n\n", len(st.World.Cells), st.World.Resolution, len(st.World.Devices), st.Base)
	want := map[string]bool{}
	for _, s := range strings.Split(only, ",") {
		if s = strings.TrimSpace(s); s != "" {
			want[s] = true
		}
	}
	var all []*citysim.Result
	failed := 0
	for _, scn := range citysim.Scenarios(st.World) {
		if len(want) > 0 && !want[scn.ID] {
			continue
		}
		start := time.Now()
		r, err := st.Runner.Run(ctx, scn)
		if err != nil {
			fmt.Printf("✗ %-22s run error: %v\n", scn.ID, err)
			failed++
			continue
		}
		all = append(all, r)
		mark := "✓"
		if !r.Pass {
			mark = "✗"
			failed++
		}
		tag := ""
		if r.Limitation {
			tag = " [documented limit]"
		}
		fmt.Printf("%s %-22s %4d readings, %4d requests, %5.1fs%s\n", mark, scn.ID, len(r.Outcomes), r.Requests, time.Since(start).Seconds(), tag)
		for _, c := range r.Checks {
			m := "  ok  "
			if !c.Pass {
				m = "  FAIL"
			}
			fmt.Printf("   %s %s — %s\n", m, c.Name, c.Detail)
		}
		for _, p := range r.Probes {
			if !p.Pass {
				fmt.Printf("   FAIL probe %s: HTTP %d (want %d) %s %s\n", p.Kind, p.Status, p.ExpectStatus, p.Reason, p.Message)
			}
		}
		fmt.Printf("   reasons: ")
		for _, k := range citysim.SortedReasons(r.Reasons) {
			fmt.Printf("%s=%d ", k, r.Reasons[k])
		}
		fmt.Printf("\n   finals: %v escrows: %v ledger verified=%v (%d entries)\n", r.Finals, r.Escrows, r.Ledger.Verified, r.Ledger.Entries)
		for _, c := range r.Scoreboard {
			fmt.Printf("   %-17s balanced %5.1f%%  fraud caught %3d/%-3d  honest credited %4d/%-4d\n", c.Validator, c.Balanced*100, c.Caught, c.Caught+c.FalseAccepts, c.Credited, c.Credited+c.FalseRejects)
		}
		fmt.Println()
	}
	if out != "" {
		b, _ := json.MarshalIndent(all, "", " ")
		_ = os.WriteFile(out, b, 0o644)
	}
	if report != "" {
		_ = os.WriteFile(report, []byte(citysim.Report(st.World, all)), 0o644)
		fmt.Println("report written to", report)
	}
	if failed > 0 {
		fmt.Printf("%d scenario(s) failed\n", failed)
		return 1
	}
	fmt.Println("all scenarios passed")
	return 0
}

func serve(ctx context.Context, db, addr string, res, radius int) {
	st, err := citysim.StartStack(ctx, db, addr, citysim.WorldConfig{Resolution: res, Radius: radius}, false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "start:", err)
		os.Exit(1)
	}
	defer st.Close()
	st.MountUI()
	fmt.Printf("VarunaSetu virtual city: %d H3 cells, %d devices\n", len(st.World.Cells), len(st.World.Devices))
	fmt.Printf("  live demo : %s/\n  real API  : %s/telemetry/readings  /escrows  /cells  /ledger/entries  /analytics/summary\n", st.Base, st.Base)
	fmt.Println("  Ctrl-C to stop.")
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
}
