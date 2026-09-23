# Testing & QA — avdslim

## Commands (all verified in `Makefile` / `release.yml`)

- `go build ./...` / `make build` → `bin/avdslim` (`-s -w` ldflags, version
  injected via `-X main.version`; `VERSION=1.0.15` in `Makefile` — bump together
  with the `version` var in `cmd/avdslim/main.go` and `install.sh`).
- `go vet ./...` — run before any PR; no lint config exists.
- `go test ./...` / `make test` — all against the fake bash `adb` in
  `internal/adbtest` (skipped on Windows), no emulator needed:
  - `internal/adb/client_test.go`: Slim/Restore exact inverse, target
    resolution, selective feature/app enabling (`TestEnableTarget`).
  - `cmd/avdslim/main_test.go`: end-to-end CLI — the test binary re-runs the
    real `main()` as a subprocess (`AVDSLIM_RUN_MAIN=1`) with the fake adb first
    on `PATH` and an isolated `HOME`. Covers `on`/`off`/`list`/`enable`/
    `doctor`, flags, bad input. Assert on output, the fake's state files, and
    its `calls` log (every adb invocation).
- `make cross` — CGO-free builds for darwin-arm64/amd64, linux-amd64,
  windows-amd64.
- `gofmt -l .` — must be clean; no formatter config in repo.

## CI (`.github/workflows/`)

- `ci.yml` — on push & pull_request to `main`: runs `gofmt` verification,
  `go vet ./...`, `go test -v ./...`, and binary compilation.
- `release.yml` — on tag `v*` only: builds 4 platform tarballs
  (`avdslim_<ver>_<os>_<arch>.tar.gz` + README/LICENSE), writes
  `checksums.txt`, publishes via `softprops/action-gh-release`. Go 1.22.
- `github-repo-stats.yml` — monitoring workflow, unrelated to code quality.

## Standard to hold

- Guest-mutating commands are tested against the fake adb: when a command
  makes a new adb call, teach `internal/adbtest` to answer it and add a
  `main_test.go` case. Failure paths use the fake's switch files
  (`pm_broken`, `enable_fails`, `readonly`, `snapshot_fails`, see
  `adbtest.go`). `start`/`bake` use the fake `emulator` in `main_test.go`
  (`emulator_crash` simulates a startup death); the shim is tested in
  `internal/shim/shim_test.go` against a temp SDK and by running the
  generated script. Still not covered: `bench`, `watch` (infinite loop)
  and host memory probes (`lsof`/`ps`/`footprint`).
- New failure handling gets a test that fails without the fix (RED first).
  Keep new pure logic (package-list filtering, arg parsing, ini editing,
  GPU-mode selection) in `internal/` packages so it is testable without adb.
- Check new tests can fail: break the code with Edit, run, revert with Edit,
  confirm `git diff` on the file is empty. Never revert with `git checkout`.
- `doctor` must stay read-only; `Slim`/`Restore` must stay exact inverses.
  Guest settings live in the `tweaks` table (`internal/adb/client.go`); Slim
  records each original value in the state JSON before changing it and
  Restore puts it back, so add new settings to that table, not as raw
  `settings put` calls.
- The fake adb only proves avdslim sends the right commands, not that a real
  guest accepts them. When touching device-mutating code, also run
  `avdslim doctor`, `avdslim on` / `avdslim off`, `avdslim measure` against a
  running emulator. State this was done (or why it wasn't possible) in the
  PR/summary.
