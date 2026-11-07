package verifier

import (
	"math"
	"slices"
)

// medianSorted returns the median of a sorted, non-empty slice. For an even
// count it is the mean of the two middle values (halved first to avoid
// overflow).
func medianSorted(s []float64) float64 {
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return s[n/2-1]/2 + s[n/2]/2
}

// peerStats are the robust statistics of a set of peer depths.
type peerStats struct {
	median float64
	mad    float64 // median(|x - median|)
	spread float64 // max - min
}

// computeStats sorts peers in place (it must be non-empty) and uses dev as
// scratch space. It returns the statistics and the (possibly grown) scratch.
func computeStats(peers, dev []float64) (peerStats, []float64) {
	slices.Sort(peers)
	m := medianSorted(peers)
	dev = dev[:0]
	for _, x := range peers {
		dev = append(dev, math.Abs(x-m))
	}
	slices.Sort(dev)
	return peerStats{
		median: m,
		mad:    medianSorted(dev),
		spread: peers[len(peers)-1] - peers[0],
	}, dev
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// scratch holds reusable buffers so Evaluate does not allocate per call.
type scratch struct{ a, b []float64 }
