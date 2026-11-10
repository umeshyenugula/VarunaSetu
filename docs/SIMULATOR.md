# Urban water simulator

The simulator is available at `http://localhost:5173/#simulator` and is
separate from the intelligence dashboard view.

The current vertical slice provides:

- A deterministic demo neighbourhood with residential, apartment, commercial,
  and industrial buildings.
- Building creation and configuration for roof area, runoff coefficient, tanks,
  and recharge pits.
- Drag-and-drop virtual rain gauge, flow meter, and water-level sensors.
- Validation that water-level sensors can only be attached to buildings with a
  tank.
- Adjustable rainfall intensity, start/pause controls, and 1x/5x/10x/30x
  simulation speed.
- Deterministic rooftop runoff using
  `rainfall depth × roof area × runoff coefficient`.
- Separate rainfall, runoff, captured, overflow, recharge, and stored-water
  accounting.
- Explicit sensor fault states and local session save/reset controls.

Simulation state is local to the browser. The UI labels all generated data as
`SIMULATED`. Backend transmission is not faked: the existing telemetry API
requires enrolled Ed25519 device keys and signed payloads, so the next
integration stage must add simulator-device enrollment and signing before
submitting events to `POST /telemetry/readings`.

The simulator intentionally does not calculate or display backend verification
decisions. Those remain authoritative in the Go service and intelligence
dashboard.

## Backend-integrated virtual city

The browser simulator above stays local. The integration stage it mentions now
exists as a separate Go tool, `citysim` (`go run ./cmd/citysim serve`): it
enrols simulated devices, signs every reading with Ed25519 and sends it to the
real `POST /telemetry/readings`. Verdicts shown there are the service's own.
See [`DEMO.md`](DEMO.md) and [`DEMO_RESULTS.md`](DEMO_RESULTS.md).
