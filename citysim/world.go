// Package citysim drives a virtual city against the real VarunaSetu service.
//
// The city is a set of real H3 cells around a real coordinate. Every building
// has an enrolled Ed25519 device, and every reading is signed and sent over
// real HTTP to POST /telemetry/readings, so the decisions shown to a viewer
// come from the production verifier, identity, persistence and ledger code and
// not from a mock. Only the weather is synthetic, and the clock is virtual so
// a storm can be replayed faster than real time (see Clock).
package citysim

import (
	"crypto/sha256"
	"fmt"
	"math"
	"math/rand"
	"sort"

	h3 "github.com/uber/h3-go/v4"

	"hydroledger-prototype/hydroledger/verifier/identity"
)

// Hyderabad (HITEC City area) is the default centre of the virtual city.
const (
	DefaultLat = 17.4435
	DefaultLng = 78.3772
)

// Cell is one H3 cell of the virtual city.
type Cell struct {
	ID        string       `json:"id"`
	Label     string       `json:"label"`
	Ring      int          `json:"ring"`
	Lat       float64      `json:"lat"`
	Lng       float64      `json:"lng"`
	X         float64      `json:"x"` // metres east of the city centre
	Y         float64      `json:"y"` // metres north of the city centre
	Boundary  [][2]float64 `json:"boundary"`
	Neighbors []string     `json:"neighbors"` // H3 k=1 ring, as used for escrow corroboration
	Devices   int          `json:"devices"`
}

// Building is a rooftop with a tank.
type Building struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	CellID      string  `json:"cell"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	RoofM2      float64 `json:"roofM2"`
	Runoff      float64 `json:"runoff"`
	TankL       float64 `json:"tankL"`
	ReservableL float64 `json:"reservableL"` // DB capacity_litres (allocation cap)
	storedL     float64
}

// Device is the metering device bolted to a building.
type Device struct {
	ID       string `json:"id"`
	Building *Building
	Key      *identity.KeyPair
	phase    float64 // report jitter, seconds
}

// World is the whole virtual city.
type World struct {
	Lat, Lng   float64
	Resolution int
	Radius     int
	Cells      map[string]*Cell
	CellOrder  []string
	Devices    []*Device
	ByCell     map[string][]*Device
	ByID       map[string]*Device
	CenterCell string
}

// Config selects the shape of the world.
type WorldConfig struct {
	Lat, Lng   float64
	Resolution int // H3 resolution, default 9
	Radius     int // k-ring radius of the city, default 3 (37 cells)
	Seed       int64
}

func (c *WorldConfig) defaults() {
	if c.Lat == 0 && c.Lng == 0 {
		c.Lat, c.Lng = DefaultLat, DefaultLng
	}
	if c.Resolution == 0 {
		c.Resolution = 9
	}
	if c.Radius == 0 {
		c.Radius = 3
	}
	if c.Seed == 0 {
		c.Seed = 7
	}
}

// project converts lat/lng to metres east/north of the city centre.
func (w *World) project(lat, lng float64) (x, y float64) {
	x = (lng - w.Lng) * math.Cos(w.Lat*math.Pi/180) * 111320
	y = (lat - w.Lat) * 110540
	return
}

func (w *World) unproject(x, y float64) (lat, lng float64) {
	lat = w.Lat + y/110540
	lng = w.Lng + x/(111320*math.Cos(w.Lat*math.Pi/180))
	return
}

// NewWorld builds the virtual city deterministically.
func NewWorld(cfg WorldConfig) (*World, error) {
	cfg.defaults()
	w := &World{Lat: cfg.Lat, Lng: cfg.Lng, Resolution: cfg.Resolution, Radius: cfg.Radius,
		Cells: map[string]*Cell{}, ByCell: map[string][]*Device{}, ByID: map[string]*Device{}}
	centre, err := h3.LatLngToCell(h3.NewLatLng(cfg.Lat, cfg.Lng), cfg.Resolution)
	if err != nil {
		return nil, err
	}
	w.CenterCell = centre.String()
	disk, err := centre.GridDisk(cfg.Radius)
	if err != nil {
		return nil, err
	}
	rng := rand.New(rand.NewSource(cfg.Seed))

	type tmp struct {
		c    h3.Cell
		ring int
		ang  float64
		cell *Cell
	}
	var all []*tmp
	for _, c := range disk {
		ring, err := h3.GridDistance(centre, c)
		if err != nil {
			return nil, err
		}
		ll, err := c.LatLng()
		if err != nil {
			return nil, err
		}
		cell := &Cell{ID: c.String(), Ring: ring, Lat: ll.Lat, Lng: ll.Lng}
		cell.X, cell.Y = w.project(ll.Lat, ll.Lng)
		b, err := c.Boundary()
		if err != nil {
			return nil, err
		}
		for _, v := range b {
			cell.Boundary = append(cell.Boundary, [2]float64{v.Lat, v.Lng})
		}
		nb, err := c.GridDisk(1)
		if err != nil {
			return nil, err
		}
		for _, n := range nb {
			if n != c {
				cell.Neighbors = append(cell.Neighbors, n.String())
			}
		}
		sort.Strings(cell.Neighbors)
		all = append(all, &tmp{c: c, ring: ring, ang: math.Atan2(cell.Y, cell.X), cell: cell})
	}
	// Stable order: by ring, then counter-clockwise from east.
	sort.Slice(all, func(i, j int) bool {
		if all[i].ring != all[j].ring {
			return all[i].ring < all[j].ring
		}
		return all[i].ang < all[j].ang
	})

	compass := []string{"E", "NE", "N", "NW", "W", "SW", "S", "SE"}
	ringIdx := map[int]int{}
	for _, t := range all {
		i := ringIdx[t.ring]
		ringIdx[t.ring]++
		cell := t.cell
		if t.ring == 0 {
			cell.Label = "Core"
		} else {
			ang := t.ang
			if ang < 0 {
				ang += 2 * math.Pi
			}
			cell.Label = fmt.Sprintf("R%d·%s", t.ring, compass[int(math.Round(ang/(math.Pi/4)))%8])
		}
		cell.Devices = deviceCount(t.ring, i)
		w.Cells[cell.ID] = cell
		w.CellOrder = append(w.CellOrder, cell.ID)
	}
	// Disambiguate duplicate labels in the outer rings.
	seen := map[string]int{}
	for _, id := range w.CellOrder {
		c := w.Cells[id]
		seen[c.Label]++
		if seen[c.Label] > 1 {
			c.Label = fmt.Sprintf("%s%d", c.Label, seen[c.Label])
		}
	}

	for _, id := range w.CellOrder {
		cell := w.Cells[id]
		hc := h3.CellFromString(id)
		for i := 0; i < cell.Devices; i++ {
			b := w.placeBuilding(rng, hc, cell, i)
			kp, err := identity.NewKeyPairFromSeed("dev-"+b.ID[4:], seedFor("dev-"+b.ID[4:]))
			if err != nil {
				return nil, err
			}
			d := &Device{ID: "dev-" + b.ID[4:], Building: b, Key: kp, phase: rng.Float64() * 45}
			w.Devices = append(w.Devices, d)
			w.ByCell[id] = append(w.ByCell[id], d)
			w.ByID[d.ID] = d
		}
	}
	return w, nil
}

// deviceCount gives the cell its device density: the core is dense, the first
// ring is dense, the outer rings are mixed and a few edge cells are sparse
// (they exercise the "too few peers" paths of the verifier).
func deviceCount(ring, idx int) int {
	switch ring {
	case 0:
		return 9
	case 1:
		return []int{8, 8, 7, 7, 8, 7}[idx%6]
	case 2:
		return []int{6, 3, 7, 6, 2, 7, 6, 7, 6, 7, 3, 6}[idx%12]
	default:
		return []int{5, 6, 5, 4, 6, 5, 3, 6, 5, 6, 5, 4, 6, 5, 2, 6, 5, 6}[idx%18]
	}
}

func seedFor(id string) []byte {
	h := sha256.Sum256([]byte("varunasetu-citysim|" + id))
	return h[:]
}

var kinds = []struct {
	name         string
	weight       float64
	minM2, maxM2 float64
	runoff       float64
}{
	{"HOUSE", 0.55, 80, 140, 0.82},
	{"APARTMENT", 0.20, 300, 480, 0.78},
	{"COMMERCIAL", 0.15, 200, 350, 0.75},
	{"INDUSTRIAL", 0.10, 500, 800, 0.70},
}

func (w *World) placeBuilding(rng *rand.Rand, hc h3.Cell, cell *Cell, i int) *Building {
	r := rng.Float64()
	k := kinds[0]
	acc := 0.0
	for _, kk := range kinds {
		acc += kk.weight
		if r <= acc {
			k = kk
			break
		}
	}
	roof := math.Round(k.minM2 + rng.Float64()*(k.maxM2-k.minM2))
	suffix := cell.ID[len(cell.ID)-6:]
	b := &Building{
		ID:     fmt.Sprintf("bld-%s-%02d", suffix, i),
		Name:   fmt.Sprintf("%s %s-%02d", k.name, cell.Label, i+1),
		Kind:   k.name,
		CellID: cell.ID,
		RoofM2: roof,
		Runoff: k.runoff,
	}
	b.TankL = math.Round(roof * k.runoff * 90) // 90 mm of storage: a tank that does not saturate in a storm
	b.ReservableL = b.TankL * 40
	// Random point inside the hexagon (reject until it maps back to the cell).
	for try := 0; try < 40; try++ {
		dx, dy := (rng.Float64()*2-1)*140, (rng.Float64()*2-1)*140
		lat, lng := w.unproject(cell.X+dx, cell.Y+dy)
		if c, err := h3.LatLngToCell(h3.NewLatLng(lat, lng), w.Resolution); err == nil && c == hc {
			b.Lat, b.Lng, b.X, b.Y = lat, lng, cell.X+dx, cell.Y+dy
			return b
		}
	}
	b.Lat, b.Lng, b.X, b.Y = cell.Lat, cell.Lng, cell.X, cell.Y
	return b
}

// Ring returns the cell ids at the given ring distance from the centre.
func (w *World) Ring(r int) []string {
	var out []string
	for _, id := range w.CellOrder {
		if w.Cells[id].Ring == r {
			out = append(out, id)
		}
	}
	return out
}

// DeviceIDs lists the device ids of a cell in stable order.
func (w *World) DeviceIDs(cell string) []string {
	var out []string
	for _, d := range w.ByCell[cell] {
		out = append(out, d.ID)
	}
	return out
}

// SparseCells returns cells with fewer than minPeers+1 devices.
func (w *World) SparseCells() []string {
	var out []string
	for _, id := range w.CellOrder {
		if w.Cells[id].Devices < 5 {
			out = append(out, id)
		}
	}
	return out
}

// DenseNeighbours returns the k=1 neighbours of cell that are inside the city
// and have at least minDevices devices.
func (w *World) DenseNeighbours(cell string, minDevices int) []string {
	var out []string
	for _, n := range w.Cells[cell].Neighbors {
		if c, ok := w.Cells[n]; ok && c.Devices >= minDevices {
			out = append(out, n)
		}
	}
	return out
}
