# HydroLedger verifier (prototype)

A Go library that classifies rainwater-harvest readings as `ACCEPT`,
`PROVISIONAL_ESCROW` or `QUARANTINE` by checking each claim against a
radar-derived physical bound and against recent readings from other devices in
the same cell. It was rebuilt from a written specification; there is no earlier
code.

**Status: prototype.** It has not been reviewed for security, offers no
protection against Sybil or coordinated reporting (a test documents one such
case), and keeps all state in memory. Read `HANDOFF.md` before relying on any
behaviour the specification did not spell out.

## Layout

```
hydroledger/verifier/   the package (code, table-driven tests, benchmarks, example)
CONTRACTS.md            public API, states, reason codes, defaults
HANDOFF.md              handoff log: assumptions, known limitations, next steps
docs/baseline_results.txt   raw test and benchmark output from one machine
```

Standard library only; module `hydroledger-prototype`, Go 1.24 directive.

## Use

```go
v, err := verifier.New(verifier.DefaultConfig())
if err != nil { /* invalid config */ }

v.SetNeighbors("cell-a", []string{"cell-b", "cell-c"}) // caller-supplied, no H3

d := v.Evaluate(reading)          // d.State, d.Reason
// d.State == verifier.StateProvisionalEscrow means: held, not final.

// Call periodically; `now` must be on the same time base as Reading.Timestamp.
for _, r := range v.ResolveEscrows(now) {
    // r.State is ACCEPT (ESCROW_CORROBORATED) or QUARANTINE (ESCROW_EXPIRED_UNCORROBORATED)
}
```

`go test ./hydroledger/verifier -run Example` runs a compiled version of the
first call.

## Test and benchmark

```
go test ./... -race
go test ./... -run '^$' -bench . -benchmem
```

`docs/baseline_results.txt` holds the output of both on the machine named in its
header, with Go version and CPU model. Benchmark figures depend on the machine;
none are quoted in these documents.
