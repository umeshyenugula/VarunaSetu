package verifier_test

import (
	"fmt"
	"time"

	"hydroledger-prototype/hydroledger/verifier"
)

func ExampleVerifier_Evaluate() {
	v, err := verifier.New(verifier.DefaultConfig())
	if err != nil {
		panic(err)
	}
	d := v.Evaluate(verifier.Reading{
		DeviceID:       "tank-17",
		Timestamp:      time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC),
		ClaimedLitres:  400, // 400 L over a 100 m2 roof at C=1.0 is 4 mm
		RoofAreaM2:     100,
		RunoffCoeff:    1.0,
		StoredLitres:   100,
		CapacityLitres: 5000,
		RadarMM:        5,
		CellID:         "cell-a",
	})
	// No peers have reported yet, so the radar physics fallback decides:
	// 4 mm <= 1.15*5 + 2 = 7.75 mm.
	fmt.Println(d.State, d.Reason)
	// Output: ACCEPT RADAR_PHYSICS_FALLBACK_INSUFFICIENT_PEERS
}
