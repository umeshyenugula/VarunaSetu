package citysim

import (
	"math"
	"time"
)

// Storm is a slow-moving convective cell with a Gaussian rain footprint.
// Positions are metres east/north of the city centre; times are offsets from
// the start of the scenario. Slow storms are the flash-flood case: they sit
// over a handful of cells for tens of minutes.
type Storm struct {
	Name    string
	X0, Y0  float64       // centre at Start
	VX, VY  float64       // m/s
	SigmaM  float64       // footprint radius (1 sigma), metres
	PeakMMh float64       // peak intensity, mm/h
	Start   time.Duration // onset
	End     time.Duration // end of rain
	RampDur time.Duration // raised-cosine ramp up and down
}

func (s Storm) centre(t time.Duration) (x, y float64) {
	dt := (t - s.Start).Seconds()
	return s.X0 + s.VX*dt, s.Y0 + s.VY*dt
}

func (s Storm) envelope(t time.Duration) float64 {
	if t < s.Start || t > s.End {
		return 0
	}
	ramp := s.RampDur
	if ramp <= 0 {
		ramp = 12 * time.Minute
	}
	up := float64(t-s.Start) / float64(ramp)
	down := float64(s.End-t) / float64(ramp)
	f := math.Min(1, math.Min(up, down))
	return 0.5 - 0.5*math.Cos(math.Pi*f) // raised cosine
}

// Rate is the rain rate (mm/h) of this storm at a point and time.
func (s Storm) Rate(x, y float64, t time.Duration) float64 {
	e := s.envelope(t)
	if e == 0 {
		return 0
	}
	cx, cy := s.centre(t)
	d2 := (x-cx)*(x-cx) + (y-cy)*(y-cy)
	return s.PeakMMh * e * math.Exp(-d2/(2*s.SigmaM*s.SigmaM))
}

// RadarMode is how a radar fault corrupts what the radar reports for a cell.
type RadarMode string

const (
	RadarDry   RadarMode = "dry"   // reads zero: beam blockage, outage or a cell under the beam
	RadarLag   RadarMode = "lag"   // reports the picture Lag ago (delayed product)
	RadarScale RadarMode = "scale" // over/under-reads by Factor
)

// RadarFault degrades the radar for some cells during a time range.
type RadarFault struct {
	Cells  map[string]bool // nil means every cell
	From   time.Duration
	To     time.Duration
	Mode   RadarMode
	Lag    time.Duration
	Factor float64
}

func (f RadarFault) applies(cell string, t time.Duration) bool {
	if t < f.From || t >= f.To {
		return false
	}
	return f.Cells == nil || f.Cells[cell]
}

// Weather is the ground truth rain field plus the radar that observes it.
type Weather struct {
	Storms []Storm
	Faults []RadarFault
}

// Rate is the true rain rate in mm/h at a point.
func (wx *Weather) Rate(x, y float64, t time.Duration) float64 {
	r := 0.0
	for _, s := range wx.Storms {
		r += s.Rate(x, y, t)
	}
	return r
}

// Depth integrates the true rain over (t-window, t] at a point, in mm.
func (wx *Weather) Depth(x, y float64, t, window time.Duration) float64 {
	const step = 30 * time.Second
	sum := 0.0
	n := 0
	for tt := t - window + step/2; tt < t; tt += step {
		sum += wx.Rate(x, y, tt)
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n) * window.Hours()
}

// Radar is what the radar reports for a cell over (t-window, t], in mm. It
// sees the cell's rain with a small multiplicative error and is then degraded
// by any active fault.
func (wx *Weather) Radar(c *Cell, t, window time.Duration) float64 {
	return wx.radar(c, t, window, true)
}

// RadarClean is Radar without the random measurement error (for the map).
func (wx *Weather) RadarClean(c *Cell, t, window time.Duration) float64 {
	return wx.radar(c, t, window, false)
}

func (wx *Weather) radar(c *Cell, t, window time.Duration, noisy bool) float64 {
	tt := t
	scale := 1.0
	for _, f := range wx.Faults {
		if !f.applies(c.ID, t) {
			continue
		}
		switch f.Mode {
		case RadarDry:
			return 0
		case RadarLag:
			tt = t - f.Lag
		case RadarScale:
			scale *= f.Factor
		}
	}
	sum := wx.Depth(c.X, c.Y, tt, window) // rain is modelled per H3 cell, like a radar product at cell resolution
	n := 1.0
	v := sum / n * scale
	if noisy {
		v *= math.Exp(hashGauss(c.ID, int(t/window)) * 0.05) // same value for every device of the cell
	}
	if v < 0.05 {
		return 0
	}
	return v
}

// StormCentres returns the position of every active storm (for the map).
func (wx *Weather) StormCentres(t time.Duration) []StormPos {
	var out []StormPos
	for _, s := range wx.Storms {
		if e := s.envelope(t); e > 0 {
			x, y := s.centre(t)
			out = append(out, StormPos{Name: s.Name, X: x, Y: y, SigmaM: s.SigmaM, Intensity: s.PeakMMh * e})
		}
	}
	return out
}

// StormPos is the live position of a storm.
type StormPos struct {
	Name      string  `json:"name"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	SigmaM    float64 `json:"sigma"`
	Intensity float64 `json:"mmh"`
}
