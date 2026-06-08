Fix remaining conformance test failures in umpire-go-gen. The repo is at /home/douglasbrown/Code/umpire-go-gen.

Current status: 17/51 conformance subtests pass. Key issues to fix:

1. **oneOf branch disabling**: `outfieldSignal.Enabled` should be `false` when `bullpenPhone` branch is active (bullpen-structural fixture)
2. **Expression compiler syntax errors**: Missing if conditions, unexpected `)` in generated code (dsl-matrix, scoreboard-source-checks)
3. **depSatisfied for map[string]any**: Returns `true` for object types (pitcherCard) instead of checking non-empty
4. **requires propagation**: Fields not disabling when dependencies unsatisfied (rain-delay-chain, rotation-cascade)
5. **disables rule processing**: Not implemented (stale-signal-disables)
6. **Schema validation**: Missing error for unknown fields, missing runtime conditions (failures fixtures)

Files in scope: internal/codegen/*.go (check.go, expr.go, rules.go, infer.go, codegen.go, conformance_test.go)

Verification: `make test` (syncs spec then runs `go test -v ./...`) — 100% of conformance cases pass.

Commit each fix phase separately with emoji-prefixed messages.