# Architecture — avdslim

`avdslim` is a zero-dependency (stdlib-only) Go CLI that cuts Android emulator
host RAM (~8 GB → ~1.5 GB) by launching QEMU with `-lowram` + hardware GPU,
disabling ~50 bloat packages via adb, and restoring a pre-baked `avdslim_clean`
snapshot for ~1.5 s boots.

## Layout

- `cmd/avdslim/main.go` — only entrypoint. Hand-rolled arg parsing
  (`os.Args` switch, no cobra/flag lib). All 16+ commands live here as
  `handle*` functions (~1400 lines, the God-file).
- `internal/adb/` — adb subprocess wrapper and guest mutations. `Slim` disables
  selected user 0 packages and records their original overrides in
  `/data/local/tmp/avdslim_state.json`. `Restore`, `Enable` and `--keep` share
  the package restore logic. `Restore` also handles guest settings. A missing
  journal makes `Restore` a no-op.
- `internal/bloat/packages.go` — source of truth for package lists:
  `StandardBloatCategories` (map, safe) + `AggressiveBloatPackages` (slice,
  incl. Play Store updater + Chrome). Never disable anything not listed here;
  GMS core / WebView / sockets / photopicker are protected by omission.
- `internal/config/` — host-side SDK/AVD knowledge. `paths.go` locates SDK
  (`ANDROID_HOME`/`ANDROID_SDK_ROOT` → OS-default paths), adb/emulator binaries
  (`LookPath` → SDK fallback → bare name), and per-OS GPU mode
  (`host` everywhere except headless Linux → `swiftshader_indirect`).
  `tuner.go` edits `~/.android/avd/<name>.avd/config.ini` (RAM/heap/GPU,
  no camera/audio), keeps a one-time `config.ini.bak`, and purges
  `hardware-qemu.ini{,.lock}` + `snapshots/` to kill stale 4 GB/lavapipe state.
  `features.go` owns host features (audio, camera, dpad, bootanim): it writes an
  `avdslim.<feature>=yes|no` marker plus the matching `hw.*` keys into
  `config.ini`. That marker is the single source of truth — `start`, `bake`,
  `TuneAvd` (via `ApplySlimHardware`) and the shim all read it, so
  `avdslim enable audio` survives a re-tune.
  `create.go` picks images for `avdslim create`: only
  `system-images/android-<N>/google_apis/<host abi>` (never Play Store or
  `_ps16k`), newest first, and locates `avdmanager` (PATH →
  `cmdline-tools/latest` → highest versioned dir). `create` never runs
  `sdkmanager` or accepts licenses; it shells out to `avdmanager` then `TuneAvd`.
- `internal/host/` (`process.go`, `memory.go` + `process_unix.go` / `process_windows.go`) —
  host QEMU PID discovery (`lsof -i :<port>` → `ps` fallback), host RAM/swap inspection
  (`os.Getpagesize()`, `vm_stat`, `vm.swapusage`, `/proc/meminfo`), and memory
  probes (`footprint --format bytes` on macOS → RSS fallback; `/proc/<pid>/status` on Linux).
  `SetDetached` is the only platform-split code (build tags).
- `internal/shim/shim.go` — renames SDK `emulator` → `emulator.real`, writes a
  shell/batch shim injecting `-memory/-lowram/-no-audio/no-camera` + snapshot
  restore; `-no-audio`/`-camera-* none` are skipped when the AVD's `config.ini`
  carries `avdslim.audio=yes` / `avdslim.camera=yes` (`avdslim_flag` in the
  script — `IsShimOutdated` treats a shim without it as stale). Honors caller's explicit `-memory/-lowram`, `--no-lowram`,
  `--no-slim` passthroughs. Rollback on write failure.
- `internal/doctor/doctor.go` — read-only audit (toolchain, AVD image type,
  RAM/GPU, running emulators, snapshot presence). Never mutates.
- `internal/adbtest/adbtest.go` — fake bash `adb` for tests; imported only by
  `_test.go` files, never by shipped code. A leaf (stdlib only).

## Dependency direction

`main` → `adb` → (`bloat`, `config`); `main` → `config`, `doctor`, `host`,
`shim`; `shim`/`doctor` → `config`. `bloat` is a leaf (no imports).
`host` and `config` never import `adb` — host process probing and SDK paths
stay independent of device state.

## Invariants

- Stdlib only (`go.mod` has zero requires). Do not add dependencies.
- `go.mod` module path is `github.com/kdbhalala/avdslim`, matching the GitHub
  remote. Keep them in sync or `go install` breaks.
- PID detection must exclude own process: filter `avdslim`, `crashpad`,
  `netsimd` and skip `os.Getpid()` or `measure` reports its own 5 MB RSS.
- `launch`/`bake` must pass `-no-snapshot-load` (cold) vs
  `-snapshot avdslim_clean -no-snapshot-save` (golden) — never save over the
  golden snapshot during dev sessions; `stop --snap` is the only path that
  overwrites it.
- `background_process_limit = 4` and `max_cached_processes = 4`
  (eliminates cached app bloat while protecting OAuth/biometrics);
  animations keep their existing scale unless `--no-anim` is selected.
  Restore deletes settings that were originally unset.
- `hw.cpu.ncore = 2` enforced in `TuneAvd` to curb host thread overhead and
  ART compilation worker thread memory footprint.
- `TuneAvd` rewrites `config.ini` from a map — key order is not preserved.
  Acceptable; do not "fix" by adding ordering unless a bug requires it.
- `Slim` journals original overrides before changing packages. Restore only
  undoes a recorded disable while user 0's override is still `disabled-user`.
  It waits for the package manager's pending write and checks each override
  before releasing the journal entry. Unconfirmed entries stay for retry.
  Keeping the journal in the guest lets `-wipe-data` and snapshot loads reset
  the journal and package state together.
- Every host file avdslim deletes or replaces must survive failure: tune and
  host features refuse without `config.ini.bak`; `bake` keeps the previous
  Golden Snapshot until the new one is saved; `uninstall-shim` refuses when
  an SDK update already replaced the shim.
- AVD paths come from `config.AvdDir` / `GoldenSnapshotDir` (honor
  `ANDROID_AVD_HOME`), never a hand-built `~/.android/avd`. The shim script
  does the same via `${ANDROID_AVD_HOME:-$HOME/.android/avd}`.
- Every `❌` exits 1 (scripts and the Action depend on it).
- Launching goes through `spawnEmulator` + `waitForBoot`: emulator output to
  a temp log, startup crash reported with that log, never
  `adb wait-for-device` (hangs forever if the emulator dies).
- Golden SDK rule the code enforces in `doctor`/`tuner`: Google APIs 4 KB
  image good; Google Play (no `adb root`, updater churn) and 16 KB page-size
  (QEMU 4096 MB floor) bad.
