package spatial

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"hydroledger-prototype/hydroledger/verifier"
)

// The reference coordinate is a rooftop location in Hyderabad. Expected H3
// indexes were produced by the pinned H3 implementation and are pinned here so
// a dependency or adapter change that moves a cell is visible.
const (
	refLat = 17.5449
	refLng = 78.3864

	refCellRes7  = "8760b194bffffff"
	refCellRes8  = "8860b194b9fffff"
	refCellRes9  = "8960b194b87ffff"
	refCellRes10 = "8a60b194b86ffff"
	refCellRes11 = "8b60b194b86dfff"
)

var refNeighborsRes9 = []string{
	"8960b19494bffff",
	"8960b194b83ffff",
	"8960b194b8fffff",
	"8960b194b97ffff",
	"8960b194bb3ffff",
	"8960b194bbbffff",
}

// The adapter must be able to drive the real verifier without this package
// importing it in non-test code.
var _ NeighborSetter = (*verifier.Verifier)(nil)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Resolution != DefaultResolution {
		t.Errorf("Resolution = %d, want %d", cfg.Resolution, DefaultResolution)
	}
	if cfg.K != DefaultK {
		t.Errorf("K = %d, want %d", cfg.K, DefaultK)
	}
	if DefaultResolution != 9 {
		t.Errorf("DefaultResolution = %d, want the documented default 9", DefaultResolution)
	}
	if DefaultK != 1 {
		t.Errorf("DefaultK = %d, want the documented default 1", DefaultK)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("default config invalid: %v", err)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr error
	}{
		{name: "default is valid", cfg: DefaultConfig()},
		{name: "resolution 0 is valid", cfg: Config{Resolution: 0, K: 0}},
		{name: "resolution 15 is valid", cfg: Config{Resolution: 15, K: MaxK}},
		{name: "resolution -1 rejected", cfg: Config{Resolution: -1, K: 1}, wantErr: ErrInvalidResolution},
		{name: "resolution 16 rejected", cfg: Config{Resolution: 16, K: 1}, wantErr: ErrInvalidResolution},
		{name: "k -1 rejected", cfg: Config{Resolution: 9, K: -1}, wantErr: ErrInvalidK},
		{name: "k above max rejected", cfg: Config{Resolution: 9, K: MaxK + 1}, wantErr: ErrInvalidK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Validate() = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestNew_RejectsInvalidConfig(t *testing.T) {
	if _, err := New(Config{Resolution: 16, K: 1}); !errors.Is(err, ErrInvalidResolution) {
		t.Fatalf("New() error = %v, want %v", err, ErrInvalidResolution)
	}
	if _, err := New(Config{Resolution: 9, K: MaxK + 1}); !errors.Is(err, ErrInvalidK) {
		t.Fatalf("New() error = %v, want %v", err, ErrInvalidK)
	}
}

func TestCellAt_KnownCoordinate(t *testing.T) {
	tests := []struct {
		resolution int
		want       string
	}{
		{7, refCellRes7},
		{8, refCellRes8},
		{9, refCellRes9},
		{10, refCellRes10},
		{11, refCellRes11},
	}
	for _, tt := range tests {
		got, err := CellAt(refLat, refLng, tt.resolution)
		if err != nil {
			t.Fatalf("CellAt(res %d): %v", tt.resolution, err)
		}
		if got != tt.want {
			t.Errorf("CellAt(res %d) = %s, want %s", tt.resolution, got, tt.want)
		}
	}
}

func TestIndexer_UsesConfiguredResolution(t *testing.T) {
	ix, err := New(Config{Resolution: 10, K: 1})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if ix.Resolution() != 10 || ix.K() != 1 {
		t.Fatalf("Resolution/K = %d/%d, want 10/1", ix.Resolution(), ix.K())
	}
	got, err := ix.Cell(refLat, refLng)
	if err != nil {
		t.Fatalf("Cell: %v", err)
	}
	if got != refCellRes10 {
		t.Errorf("Cell = %s, want %s", got, refCellRes10)
	}
}

func TestNeighbors_KnownCoordinate(t *testing.T) {
	ix, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	cell, neighbors, err := ix.Neighbors(refLat, refLng)
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if cell != refCellRes9 {
		t.Fatalf("cell = %s, want %s", cell, refCellRes9)
	}
	if !reflect.DeepEqual(neighbors, refNeighborsRes9) {
		t.Fatalf("neighbors = %v, want %v", neighbors, refNeighborsRes9)
	}
	if len(neighbors) != 6 {
		t.Fatalf("neighbour count = %d, want 6 for k=1", len(neighbors))
	}
	for _, n := range neighbors {
		if n == cell {
			t.Fatalf("neighbour list contains the centre cell %s", cell)
		}
	}
}

func TestNeighborsOf_CountsAndKZero(t *testing.T) {
	tests := []struct {
		k    int
		want int
	}{
		{0, 0},
		{1, 6},
		{2, 18},
		{3, 36},
	}
	for _, tt := range tests {
		got, err := NeighborsOf(refCellRes9, tt.k)
		if err != nil {
			t.Fatalf("NeighborsOf(k=%d): %v", tt.k, err)
		}
		if len(got) != tt.want {
			t.Errorf("NeighborsOf(k=%d) count = %d, want %d", tt.k, len(got), tt.want)
		}
	}
}

func TestNeighborsOf_SortedAndUnique(t *testing.T) {
	got, err := NeighborsOf(refCellRes9, 2)
	if err != nil {
		t.Fatalf("NeighborsOf: %v", err)
	}
	seen := make(map[string]bool, len(got))
	for i, id := range got {
		if seen[id] {
			t.Fatalf("duplicate neighbour %s", id)
		}
		seen[id] = true
		if i > 0 && got[i-1] > id {
			t.Fatalf("neighbours not sorted at %d: %q > %q", i, got[i-1], id)
		}
	}
}

func TestInvalidCoordinates(t *testing.T) {
	tests := []struct {
		name    string
		lat     float64
		lng     float64
		wantErr error
	}{
		{name: "latitude below -90", lat: -90.0001, lng: 78, wantErr: ErrInvalidLatitude},
		{name: "latitude above 90", lat: 90.0001, lng: 78, wantErr: ErrInvalidLatitude},
		{name: "latitude NaN", lat: math.NaN(), lng: 78, wantErr: ErrInvalidLatitude},
		{name: "latitude +Inf", lat: math.Inf(1), lng: 78, wantErr: ErrInvalidLatitude},
		{name: "latitude -Inf", lat: math.Inf(-1), lng: 78, wantErr: ErrInvalidLatitude},
		{name: "longitude below -180", lat: 17, lng: -180.0001, wantErr: ErrInvalidLongitude},
		{name: "longitude above 180", lat: 17, lng: 180.0001, wantErr: ErrInvalidLongitude},
		{name: "longitude NaN", lat: 17, lng: math.NaN(), wantErr: ErrInvalidLongitude},
		{name: "longitude +Inf", lat: 17, lng: math.Inf(1), wantErr: ErrInvalidLongitude},
		{name: "longitude -Inf", lat: 17, lng: math.Inf(-1), wantErr: ErrInvalidLongitude},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := CellAt(tt.lat, tt.lng, 9); !errors.Is(err, tt.wantErr) {
				t.Fatalf("CellAt(%v, %v) error = %v, want %v", tt.lat, tt.lng, err, tt.wantErr)
			}
		})
	}
}

func TestCoordinateBoundariesAreValid(t *testing.T) {
	// The endpoints are valid geographic coordinates. The H3 binding itself
	// accepts them, and the adapter must not reject them.
	for _, ll := range [][2]float64{
		{-90, -180}, {-90, 180}, {90, -180}, {90, 180}, {0, 0},
	} {
		if _, err := CellAt(ll[0], ll[1], 9); err != nil {
			t.Errorf("CellAt(%v, %v) = %v, want nil", ll[0], ll[1], err)
		}
	}
}

func TestInvalidResolution(t *testing.T) {
	for _, res := range []int{-1, 16, 100} {
		if _, err := CellAt(refLat, refLng, res); !errors.Is(err, ErrInvalidResolution) {
			t.Errorf("CellAt(res %d) error = %v, want %v", res, err, ErrInvalidResolution)
		}
	}
}

func TestInvalidK(t *testing.T) {
	for _, k := range []int{-1, MaxK + 1} {
		if _, err := NeighborsOf(refCellRes9, k); !errors.Is(err, ErrInvalidK) {
			t.Errorf("NeighborsOf(k %d) error = %v, want %v", k, err, ErrInvalidK)
		}
	}
}

func TestValidateCell(t *testing.T) {
	tests := []struct {
		name    string
		cell    string
		wantErr error
	}{
		{name: "canonical", cell: refCellRes9},
		{name: "empty", cell: "", wantErr: ErrInvalidCell},
		{name: "not hexadecimal", cell: "not-a-cell", wantErr: ErrInvalidCell},
		{name: "uppercase is not canonical", cell: "8960B194B87FFFF", wantErr: ErrInvalidCell},
		{name: "leading zero is not canonical", cell: "08960b194b87ffff", wantErr: ErrInvalidCell},
		{name: "leading space", cell: " 8960b194b87ffff", wantErr: ErrInvalidCell},
		{name: "trailing space", cell: "8960b194b87ffff ", wantErr: ErrInvalidCell},
		{name: "invalid H3 index", cell: "fffffffffffffff", wantErr: ErrInvalidCell},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCell(tt.cell)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateCell(%q) = %v, want nil", tt.cell, err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateCell(%q) = %v, want %v", tt.cell, err, tt.wantErr)
			}
		})
	}
}

func TestNeighborsOf_RejectsInvalidCell(t *testing.T) {
	if _, err := NeighborsOf("not-a-cell", 1); !errors.Is(err, ErrInvalidCell) {
		t.Fatalf("NeighborsOf error = %v, want %v", err, ErrInvalidCell)
	}
}

type spySetter struct {
	calls     int
	cell      string
	neighbors []string
}

func (s *spySetter) SetNeighbors(cellID string, neighbors []string) {
	s.calls++
	s.cell = cellID
	s.neighbors = append([]string(nil), neighbors...)
}

func TestAdapter_SetNeighborsForCoordinate(t *testing.T) {
	ix, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a, err := NewAdapter(ix)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	var spy spySetter
	cell, err := a.SetNeighborsForCoordinate(&spy, refLat, refLng)
	if err != nil {
		t.Fatalf("SetNeighborsForCoordinate: %v", err)
	}
	if cell != refCellRes9 || spy.cell != refCellRes9 {
		t.Fatalf("cell = %s / spy %s, want %s", cell, spy.cell, refCellRes9)
	}
	if spy.calls != 1 {
		t.Fatalf("SetNeighbors calls = %d, want 1", spy.calls)
	}
	if !reflect.DeepEqual(spy.neighbors, refNeighborsRes9) {
		t.Fatalf("setter neighbours = %v, want %v", spy.neighbors, refNeighborsRes9)
	}
}

func TestAdapter_SetNeighborsForCell(t *testing.T) {
	ix, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a, err := NewAdapter(ix)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}

	var spy spySetter
	if err := a.SetNeighborsForCell(&spy, refCellRes9); err != nil {
		t.Fatalf("SetNeighborsForCell: %v", err)
	}
	if spy.cell != refCellRes9 || spy.calls != 1 {
		t.Fatalf("spy = %+v, want one call for %s", spy, refCellRes9)
	}
	if !reflect.DeepEqual(spy.neighbors, refNeighborsRes9) {
		t.Fatalf("setter neighbours = %v, want %v", spy.neighbors, refNeighborsRes9)
	}
	if err := a.SetNeighborsForCell(&spy, "not-a-cell"); !errors.Is(err, ErrInvalidCell) {
		t.Fatalf("invalid cell error = %v, want %v", err, ErrInvalidCell)
	}
}

func TestAdapter_RejectsNil(t *testing.T) {
	if _, err := NewAdapter(nil); !errors.Is(err, ErrNilIndexer) {
		t.Fatalf("NewAdapter(nil) error = %v, want %v", err, ErrNilIndexer)
	}
	ix, err := New(DefaultConfig())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a, err := NewAdapter(ix)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	if _, err := a.SetNeighborsForCoordinate(nil, refLat, refLng); !errors.Is(err, ErrNilNeighborSetter) {
		t.Fatalf("nil setter error = %v, want %v", err, ErrNilNeighborSetter)
	}
	if err := a.SetNeighborsForCell(nil, refCellRes9); !errors.Is(err, ErrNilNeighborSetter) {
		t.Fatalf("nil setter error = %v, want %v", err, ErrNilNeighborSetter)
	}
}
