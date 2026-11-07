// Package spatial maps geographic coordinates to H3 cells and derives the
// k-ring of neighbouring cells that the verifier uses for escrow
// corroboration.
//
// It is a thin adapter over the pinned pure-Go H3 binding. It does not decide
// anything about water claims, and H3 does not make the verifier resistant to
// collusion: devices in neighbouring cells can still agree on a false claim.
// See HANDOFF.md for assumptions and known limitations.
//
// Cell identifiers are canonical lower-case hexadecimal H3 indexes as returned
// by h3.Cell.String. Neighbour lists are sorted so callers can rely on stable
// input to SetNeighbors.
package spatial

import (
	"errors"
	"fmt"
	"math"
	"sort"

	h3 "github.com/uber/h3-go/v4"
)

const (
	// MinResolution and MaxResolution are the H3 resolution bounds (0..15).
	MinResolution = 0
	MaxResolution = 15

	// DefaultResolution is the project default. H3 resolution 9 has hexagons
	// of roughly 0.1 km^2 at Hyderabad, which is a neighbourhood-sized cell.
	// The choice is an assumption; see README.md in this package.
	DefaultResolution = 9

	// DefaultK is the default k-ring radius. k = 1 yields the centre cell
	// plus six immediate neighbours (fewer next to a pentagon).
	DefaultK = 1

	// MaxK bounds a single k-ring lookup so a caller-supplied k cannot cause
	// an unbounded allocation (3*k*(k+1)+1 cells including the centre).
	MaxK = 10
)

var (
	// ErrInvalidLatitude means latitude was non-finite or outside [-90, 90].
	ErrInvalidLatitude = errors.New("spatial: latitude must be finite and in [-90, 90]")
	// ErrInvalidLongitude means longitude was non-finite or outside [-180, 180].
	ErrInvalidLongitude = errors.New("spatial: longitude must be finite and in [-180, 180]")
	// ErrInvalidResolution means the H3 resolution was outside [0, 15].
	ErrInvalidResolution = errors.New("spatial: H3 resolution must be in [0, 15]")
	// ErrInvalidK means the k-ring radius was outside [0, 10].
	ErrInvalidK = errors.New("spatial: k must be in [0, 10]")
	// ErrInvalidCell means the cell id was empty, malformed, not a valid H3
	// cell, or not in canonical lower-case hexadecimal form.
	ErrInvalidCell = errors.New("spatial: invalid canonical H3 cell id")
	// ErrNilIndexer means a nil *Indexer was passed to NewAdapter.
	ErrNilIndexer = errors.New("spatial: nil indexer")
	// ErrNilNeighborSetter means a nil NeighborSetter was passed to an adapter.
	ErrNilNeighborSetter = errors.New("spatial: nil neighbor setter")
)

// Config controls the H3 resolution and k-ring radius used by an Indexer.
type Config struct {
	Resolution int // default DefaultResolution (9)
	K          int // default DefaultK (1); 0 disables neighbour lookup
}

// DefaultConfig returns the project defaults (resolution 9, k = 1).
func DefaultConfig() Config {
	return Config{Resolution: DefaultResolution, K: DefaultK}
}

// Validate reports the first problem in c, or nil.
func (c Config) Validate() error {
	if err := validateResolution(c.Resolution); err != nil {
		return err
	}
	if err := validateK(c.K); err != nil {
		return err
	}
	return nil
}

// Indexer converts coordinates to cells and cells to k-ring neighbours using a
// fixed Config. It is immutable and safe for concurrent use.
type Indexer struct {
	resolution int
	k          int
}

// New returns an Indexer using cfg, which must pass cfg.Validate.
func New(cfg Config) (*Indexer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Indexer{resolution: cfg.Resolution, k: cfg.K}, nil
}

// Resolution returns the configured H3 resolution.
func (ix *Indexer) Resolution() int { return ix.resolution }

// K returns the configured k-ring radius.
func (ix *Indexer) K() int { return ix.k }

// CellAt returns the canonical H3 cell containing (lat, lng) at resolution.
// Latitude and longitude are degrees and are validated before conversion,
// because the H3 binding itself only rejects NaN and infinity (it silently
// wraps out-of-range coordinates).
func CellAt(lat, lng float64, resolution int) (string, error) {
	if err := validateLatLng(lat, lng); err != nil {
		return "", err
	}
	if err := validateResolution(resolution); err != nil {
		return "", err
	}
	c, err := h3.LatLngToCell(h3.NewLatLng(lat, lng), resolution)
	if err != nil {
		return "", fmt.Errorf("spatial: lat/lng to cell: %w", err)
	}
	return c.String(), nil
}

// Cell returns the canonical H3 cell containing (lat, lng) at the configured
// resolution.
func (ix *Indexer) Cell(lat, lng float64) (string, error) {
	return CellAt(lat, lng, ix.resolution)
}

// Neighbors returns the cell containing (lat, lng) and its k-ring neighbours
// (the centre is excluded). The neighbour slice is never nil and is sorted
// lexicographically.
func (ix *Indexer) Neighbors(lat, lng float64) (cell string, neighbors []string, err error) {
	cell, err = ix.Cell(lat, lng)
	if err != nil {
		return "", nil, err
	}
	neighbors, err = NeighborsOf(cell, ix.k)
	if err != nil {
		return "", nil, err
	}
	return cell, neighbors, nil
}

// NeighborsOf returns the k-ring neighbours of cellID, excluding the centre.
// k = 0 returns an empty slice. The result is sorted lexicographically.
func NeighborsOf(cellID string, k int) ([]string, error) {
	if err := ValidateCell(cellID); err != nil {
		return nil, err
	}
	if err := validateK(k); err != nil {
		return nil, err
	}
	if k == 0 {
		return []string{}, nil
	}

	centre := h3.CellFromString(cellID)
	disk, err := centre.GridDisk(k)
	if err != nil {
		return nil, fmt.Errorf("spatial: H3 grid disk: %w", err)
	}

	out := make([]string, 0, len(disk)-1)
	for _, c := range disk {
		id := c.String()
		if id != cellID {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out, nil
}

// (ix *Indexer) NeighborsOf returns the configured k-ring neighbours of cellID.
func (ix *Indexer) NeighborsOf(cellID string) ([]string, error) {
	return NeighborsOf(cellID, ix.k)
}

// ValidateCell reports whether cellID is a canonical lower-case hexadecimal H3
// cell index. Requiring one spelling per cell prevents two strings from naming
// the same cell in the verifier's maps and in persisted data.
func ValidateCell(cellID string) error {
	if cellID == "" {
		return ErrInvalidCell
	}
	c := h3.CellFromString(cellID)
	if !c.IsValid() || c.String() != cellID {
		return ErrInvalidCell
	}
	return nil
}

// NeighborSetter is the subset of the verifier API used by the adapter. The
// verifier.Verifier type satisfies it; the interface keeps this package free of
// an import cycle and makes the adapter easy to test with a spy.
type NeighborSetter interface {
	SetNeighbors(cellID string, neighbors []string)
}

// Adapter computes H3 neighbours and passes them to a NeighborSetter.
type Adapter struct {
	indexer *Indexer
}

// NewAdapter wraps ix. A nil Indexer is rejected.
func NewAdapter(ix *Indexer) (*Adapter, error) {
	if ix == nil {
		return nil, ErrNilIndexer
	}
	return &Adapter{indexer: ix}, nil
}

// SetNeighborsForCoordinate computes the cell containing (lat, lng), derives
// its configured k-ring, calls setter.SetNeighbors(cell, neighbours), and
// returns the cell id. It is the one-call wiring used by ingestion.
func (a *Adapter) SetNeighborsForCoordinate(setter NeighborSetter, lat, lng float64) (string, error) {
	if setter == nil {
		return "", ErrNilNeighborSetter
	}
	cell, neighbors, err := a.indexer.Neighbors(lat, lng)
	if err != nil {
		return "", err
	}
	setter.SetNeighbors(cell, neighbors)
	return cell, nil
}

// SetNeighborsForCell derives the configured k-ring of an already known cell
// and calls setter.SetNeighbors. Use it when the caller already has a cell id
// from another H3 source.
func (a *Adapter) SetNeighborsForCell(setter NeighborSetter, cellID string) error {
	if setter == nil {
		return ErrNilNeighborSetter
	}
	neighbors, err := a.indexer.NeighborsOf(cellID)
	if err != nil {
		return err
	}
	setter.SetNeighbors(cellID, neighbors)
	return nil
}

func validateLatLng(lat, lng float64) error {
	if math.IsNaN(lat) || math.IsInf(lat, 0) || lat < -90 || lat > 90 {
		return fmt.Errorf("%w: got %v", ErrInvalidLatitude, lat)
	}
	if math.IsNaN(lng) || math.IsInf(lng, 0) || lng < -180 || lng > 180 {
		return fmt.Errorf("%w: got %v", ErrInvalidLongitude, lng)
	}
	return nil
}

func validateResolution(resolution int) error {
	if resolution < MinResolution || resolution > MaxResolution {
		return fmt.Errorf("%w: got %d", ErrInvalidResolution, resolution)
	}
	return nil
}

func validateK(k int) error {
	if k < 0 || k > MaxK {
		return fmt.Errorf("%w: got %d", ErrInvalidK, k)
	}
	return nil
}
