// Command gocli is a test harness for cross-language consistency checks.
//
// It is deliberately outside the verifier package and does not change any Go
// implementation. It reads one JSON request from a file (or stdin), replays a
// sequence of verifier.Reading evaluations and ResolveEscrows sweeps with the
// default configuration, and writes the decisions and resolutions as JSON.
//
// Request shape:
//
//	{
//	  "neighbors": {"cell-a": ["cell-b", "cell-c"]},
//	  "steps": [
//	    {"type": "reading", "device_id": "...", "timestamp": "2024-01-01T00:00:00Z",
//	     "claimed_litres": 100, "roof_area_m2": 100, "runoff_coeff": 1,
//	     "stored_litres": 0, "capacity_litres": 100000, "radar_mm": 0,
//	     "cell_id": "cell-a"},
//	    {"type": "sweep", "now": "2024-01-01T00:15:00Z"}
//	  ]
//	}
//
// An empty reading timestamp means the Go zero time (used to exercise
// INVALID_INPUT).
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"hydroledger-prototype/hydroledger/verifier"
)

type request struct {
	Neighbors map[string][]string `json:"neighbors"`
	Steps     []step              `json:"steps"`
}

type step struct {
	Type           string  `json:"type"`
	DeviceID       string  `json:"device_id"`
	Timestamp      string  `json:"timestamp"`
	ClaimedLitres  float64 `json:"claimed_litres"`
	RoofAreaM2     float64 `json:"roof_area_m2"`
	RunoffCoeff    float64 `json:"runoff_coeff"`
	StoredLitres   float64 `json:"stored_litres"`
	CapacityLitres float64 `json:"capacity_litres"`
	RadarMM        float64 `json:"radar_mm"`
	CellID         string  `json:"cell_id"`
	Now            string  `json:"now"`
}

type decision struct {
	Step         int     `json:"step"`
	DeviceID     string  `json:"device_id"`
	CellID       string  `json:"cell_id"`
	State        string  `json:"state"`
	Reason       string  `json:"reason"`
	RObsMM       float64 `json:"r_obs_mm"`
	RMaxMM       float64 `json:"r_max_mm"`
	PeerCount    int     `json:"peer_count"`
	PeerMedianMM float64 `json:"peer_median_mm"`
	PeerMADMM    float64 `json:"peer_mad_mm"`
	ZScore       float64 `json:"z_score"`
}

type resolution struct {
	DeviceID         string `json:"device_id"`
	CellID           string `json:"cell_id"`
	ReadingTimestamp string `json:"reading_timestamp"`
	ResolvedAt       string `json:"resolved_at"`
	State            string `json:"state"`
	Reason           string `json:"reason"`
}

type sweepResult struct {
	Step        int          `json:"step"`
	Now         string       `json:"now"`
	Resolutions []resolution `json:"resolutions"`
}

type response struct {
	Decisions   []decision    `json:"decisions"`
	Resolutions []sweepResult `json:"resolutions"`
}

func parseTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, value)
}

func run(input request, out io.Writer) error {
	v, err := verifier.New(verifier.DefaultConfig())
	if err != nil {
		return err
	}
	for cellID, neighbors := range input.Neighbors {
		v.SetNeighbors(cellID, neighbors)
	}

	response := response{
		Decisions:   []decision{},
		Resolutions: []sweepResult{},
	}
	for index, item := range input.Steps {
		switch item.Type {
		case "reading":
			timestamp, err := parseTime(item.Timestamp)
			if err != nil {
				return fmt.Errorf("step %d: parse timestamp: %w", index, err)
			}
			d := v.Evaluate(verifier.Reading{
				DeviceID:       item.DeviceID,
				Timestamp:      timestamp,
				ClaimedLitres:  item.ClaimedLitres,
				RoofAreaM2:     item.RoofAreaM2,
				RunoffCoeff:    item.RunoffCoeff,
				StoredLitres:   item.StoredLitres,
				CapacityLitres: item.CapacityLitres,
				RadarMM:        item.RadarMM,
				CellID:         item.CellID,
			})
			response.Decisions = append(response.Decisions, decision{
				Step:         index,
				DeviceID:     item.DeviceID,
				CellID:       item.CellID,
				State:        string(d.State),
				Reason:       string(d.Reason),
				RObsMM:       d.RObsMM,
				RMaxMM:       d.RMaxMM,
				PeerCount:    d.PeerCount,
				PeerMedianMM: d.PeerMedianMM,
				PeerMADMM:    d.PeerMADMM,
				ZScore:       d.ZScore,
			})
		case "sweep":
			now, err := parseTime(item.Now)
			if err != nil || now.IsZero() {
				return fmt.Errorf("step %d: sweep needs a non-zero RFC3339 time", index)
			}
			resolved := v.ResolveEscrows(now)
			sweep := sweepResult{
				Step:        index,
				Now:         now.UTC().Format(time.RFC3339Nano),
				Resolutions: make([]resolution, 0, len(resolved)),
			}
			for _, r := range resolved {
				sweep.Resolutions = append(sweep.Resolutions, resolution{
					DeviceID:         r.DeviceID,
					CellID:           r.CellID,
					ReadingTimestamp: r.ReadingTimestamp.UTC().Format(time.RFC3339Nano),
					ResolvedAt:       r.ResolvedAt.UTC().Format(time.RFC3339Nano),
					State:            string(r.State),
					Reason:           string(r.Reason),
				})
			}
			response.Resolutions = append(response.Resolutions, sweep)
		default:
			return fmt.Errorf("step %d: unknown type %q", index, item.Type)
		}
	}

	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(response)
}

func main() {
	var reader io.Reader = os.Stdin
	if len(os.Args) > 1 {
		file, err := os.Open(os.Args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		defer file.Close()
		reader = file
	}

	var input request
	decoder := json.NewDecoder(reader)
	// Allow extra metadata fields (e.g. fixture_version, description) in fixture files.
	if err := decoder.Decode(&input); err != nil {
		fmt.Fprintf(os.Stderr, "decode request: %v\n", err)
		os.Exit(2)
	}
	if err := run(input, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(2)
	}
}
