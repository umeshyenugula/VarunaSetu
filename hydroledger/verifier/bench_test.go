package verifier

import (
	"fmt"
	"sync/atomic"
	"testing"
)

// benchFixture returns a verifier whose `cells` cells each hold `devices`
// observations of 4 mm under a 5 mm radar, and one ready-made reading per
// device. Re-evaluating those readings is the steady-state "agree -> ACCEPT"
// path: every evaluation sees devices-1 eligible peers, replaces the device's
// own observation in place, and creates no escrow, so state does not grow
// during the benchmark.
func benchFixture(b *testing.B, cells, devices int) (*Verifier, [][]Reading) {
	b.Helper()
	v := newVerifier(b)
	rs := make([][]Reading, cells)
	for c := range rs {
		for d := 0; d < devices; d++ {
			r := rd(fmt.Sprintf("c%d-d%d", c, d), fmt.Sprintf("cell-%d", c), t0, 4, 5)
			rs[c] = append(rs[c], r)
			v.Evaluate(r)
		}
	}
	got := v.Evaluate(rs[0][0])
	if got.State != StateAccept || got.Reason != ReasonRadarAndPeersAgree || got.PeerCount != devices-1 {
		b.Fatalf("fixture is not on the intended path: %+v", got)
	}
	return v, rs
}

func BenchmarkEvaluate(b *testing.B) {
	v, rs := benchFixture(b, 1, 32)
	readings := rs[0]
	b.ReportAllocs()
	b.ResetTimer()
	accepted := 0
	for i := 0; i < b.N; i++ {
		if v.Evaluate(readings[i%len(readings)]).State == StateAccept {
			accepted++
		}
	}
	b.StopTimer()
	if accepted != b.N {
		b.Fatalf("accepted %d of %d", accepted, b.N)
	}
}

// BenchmarkEvaluateParallel runs Evaluate from GOMAXPROCS goroutines (see
// RunParallel). "same_cell" funnels every goroutine through one cell lock;
// "many_cells" gives each goroutine its own cell. The numbers only say
// something about scaling when the machine has more than one CPU; the
// benchmark output records GOMAXPROCS in its name suffix.
func BenchmarkEvaluateParallel(b *testing.B) {
	const devices = 32
	b.Run("same_cell", func(b *testing.B) {
		v, rs := benchFixture(b, 1, devices)
		readings := rs[0]
		var bad atomic.Int64
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i, notAccepted := 0, int64(0)
			for pb.Next() {
				if v.Evaluate(readings[i%len(readings)]).State != StateAccept {
					notAccepted++
				}
				i++
			}
			bad.Add(notAccepted)
		})
		b.StopTimer()
		if bad.Load() != 0 {
			b.Fatalf("%d evaluations were not ACCEPT", bad.Load())
		}
	})
	b.Run("many_cells", func(b *testing.B) {
		const cells = 64
		v, rs := benchFixture(b, cells, devices)
		var next, bad atomic.Int64
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			readings := rs[int(next.Add(1))%cells]
			i, notAccepted := 0, int64(0)
			for pb.Next() {
				if v.Evaluate(readings[i%len(readings)]).State != StateAccept {
					notAccepted++
				}
				i++
			}
			bad.Add(notAccepted)
		})
		b.StopTimer()
		if bad.Load() != 0 {
			b.Fatalf("%d evaluations were not ACCEPT", bad.Load())
		}
	})
}
