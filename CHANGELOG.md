# Changelog

## v1.0.15 — 2026-09-23

### Added
- **The Android Studio shim updates itself.** After you upgrade avdslim, the next avdslim command rewrites an outdated shim script in place, keeping its `--ram`; the real emulator binary is never touched. No more manual `avdslim install-shim` after upgrades.

### Fixed
- **A stopped emulator could borrow a running neighbour's PID.** With two emulators up, `list`/`measure` could show the wrong one's memory and `stop`/`restart`/`bake` could refuse to continue. PIDs are now matched on the exact `-port`.
- **Crash-looping guests are detected.** An AVD left by avdslim ≤ 1.0.8 in the Android 16 Bluetooth boot loop reports `boot_completed=1` while its package manager is gone. `doctor` now flags it and `on` points at `avdslim repair`.

## v1.0.14 — 2026-09-23

### Added
- **`avdslim create <name>`**: creates a new AVD from the newest installed "Google APIs" 4 KB image for the host CPU (Play Store and 16 KB page-size images are never picked), via `avdmanager`, then tunes it like `tune-avd`. Options: `--api=<level>`, `--device=<id>` (default `pixel_5`), `--ram`/`--heap`/`--gpu`. Refuses to overwrite an existing AVD. It never installs images or accepts licenses; with no suitable image it prints the `sdkmanager` command to run. Finds `avdmanager` on PATH or in the SDK's `cmdline-tools/latest` or versioned (`cmdline-tools/23.0`) directory.
- **End-to-end CLI tests without an emulator**: `cmd/avdslim/main_test.go` runs the real `main()` against a fake `adb` (`internal/adbtest`), a fake `emulator` and a fake `avdmanager`, covering `on`/`off`/`list`/`enable`/`doctor`/`start`/`bake`/`unbake`/`create`, plus the first tests for the Android Studio shim.
- **`install.sh` verifies the release checksum** and refuses a tarball that does not match `checksums.txt`.

### Fixed
- **`off` could miss packages.** A re-slim (`on --aggressive` then `on`, or `on --keep X`) overwrote the record of what earlier runs disabled, so `off` never re-enabled those packages. The record is now cumulative, and `--keep` re-enables a package an earlier slim disabled.
- **`on` changed the guest without a record.** The state file is now written and read back *before* any change; if that fails, nothing is changed and `on` exits 1. A failed `pm list packages` is an error instead of a silent no-op.
- **`off` deleted its record even when re-enabling failed.** Packages that could not be re-enabled stay recorded, `off` names them and exits 1, and can be re-run.
- **`off` is undone by the Golden Snapshot.** `start` boots the slimmed snapshot, so `off` now says so and points at `unbake` / `start --cold`.
- **`uninstall-shim` downgraded an updated emulator.** After an SDK update replaced the shim, it swapped the stale `emulator.real` back in. It now refuses and explains; the normal path renames in one step.
- **The shim ignored `ANDROID_AVD_HOME`** for the Golden Snapshot and `enable audio|camera`; so did `restart`, `bake` and `unbake`. `doctor` flags older shims for users who set it.
- **A failed `bake` deleted the existing Golden Snapshot.** The old one is kept until the new one is saved and restored on any failure. `bake` now aborts when slimming or the save fails instead of reporting success, and stops only the emulator whose AVD name matches exactly (baking `Pixel` no longer kills `Pixel_10_Pro`).
- **`start`/`bake` hung forever when the emulator died at startup.** Emulator output now goes to a temp log; a startup crash prints its last lines and exits 1. Unknown AVD names are rejected up front.
- **`restart` purged and relaunched while the old emulator was still running.** It now stops instead.
- **`enable`/`disable` of host features changed `config.ini` without a backup** when the backup could not be written. They now refuse, like `tune-avd`.
- **About 30 error paths exited 0**, so scripts and CI saw success. Every failure now exits 1. `snapshot` with several emulators running asks which one instead of using the first.
- **Headline numbers**: the README banner compared two different metrics (82%). Like-for-like in Activity Monitor it is 8.5 GB → 2.5 GB (71%).

### Changed
- **Windows builds**: releases now include `avdslim_<ver>_windows_amd64.zip` and `_windows_arm64.zip` (`avdslim.exe`). `install-shim` remains unavailable on Windows.
- `release.yml` runs gofmt, vet and the tests (with `-race`) before publishing; the Action runs the installer from its own ref instead of `main`.

## v1.0.13 — 2026-09-21

### Added
- **`avdslim enable <host-feature>` / `avdslim disable <host-feature>`**: audio, cameras, the D-Pad and the boot animation can be turned back on per AVD. `enable` writes an `avdslim.<feature>=yes` marker plus the matching `hw.*` keys into `config.ini`; `start`, `bake`, `tune-avd` and the Android Studio shim all read that marker, so the choice survives a re-tune and needs no shim reinstall per AVD. Host targets need no running emulator and default to the only running emulator's AVD; enabling `camera` also re-enables the guest camera apps when that AVD is up. Changes apply on the next cold boot (`avdslim restart`, and re-bake the Golden Snapshot if there is one).
- **`docs/FEATURES.md`**: complete reference of every `enable`/`disable` target — host features, guest features and settings, all app aliases and the package fallbacks. Tests fail if it drifts from the code.

### Changed
- **`tune-avd` no longer re-mutes enabled hardware**: it applied `hw.audioInput/Output=no` and `hw.camera.*=none` unconditionally, undoing a manual config.ini edit on every run. It now honors the `avdslim.*` markers and reports the resulting audio/camera state.
- **`IsShimOutdated` flags pre-marker shims** (avdslim ≤ 1.0.12), so `doctor` tells you to run `avdslim install-shim` before Android Studio launches can honor `enable audio` / `enable camera`.

## v1.0.12 — 2026-09-19

### Added
- **Host System Memory & Swap Pressure Audit**: Added Section 2 to `avdslim doctor` checking host physical RAM and swap pressure. On Apple Silicon, uses `os.Getpagesize()` to dynamically account for 16 KB pages instead of assuming 4 KB (which undercounts free RAM 4x), and warns if macOS is heavily compressing or swapping anonymous memory.
- **Compressed & Swapped Memory Indicator**: When physical RSS reads significantly lower than Activity Monitor memory footprint due to macOS page compression or swap, `doctor`, `measure`, and `status` now display the compressed/swapped delta directly (`(%d MB compressed/swapped out)`).

### Fixed
- **`footprint` Exact Bytes & Unit-Aware Parsing**: Hardened `GetHostFootprintMb` to request exact bytes via `footprint --format bytes` and parse `B`, `KB`, `MB`, and `GB` (including decimal representations like `1.8 GB`). Prevents formatting parse errors from silently falling back to `ps RSS` (which omits compressed/swapped pages).

## v1.0.11 — 2026-09-18

### Fixed
- **`tune-avd` could rewrite `config.ini` with no backup**: the `config.ini.bak` write ignored its error but still reported success, so a failed backup (permissions, full disk) left the tune irreversible once `config.ini` was rewritten and `snapshots/` purged. It now aborts before touching either.

## v1.0.10 — 2026-09-17

### Fixed
- **`start` / `bake` targeted the wrong emulator when another was already running**: the boot wait and slimming ran against whichever device adb picked, so `avdslim start 2` with AVD 1 up re-slimmed AVD 1 and left AVD 2 on a black screen. Both now pass `-port` to the emulator and target that serial.

## v1.0.9 — 2026-09-17

### Added
- **`avdslim repair` Command**: Unsticks an AVD hung on a black screen by re-enabling boot-critical packages directly in `package-restrictions.xml` (via `adb root`) and restarting the framework. Keeps user data.

### Fixed
- **AVD stuck on black screen after stop/start**: slimming no longer disables `com.google.android.bluetooth`. On Android 16+ images `system_server` crash-loops at boot (`FATAL: Conflicting system configuration detected`) when the Bluetooth APK is disabled, so every cold boot after the first slim hung. Bluetooth is still switched off at runtime via `bluetooth_on=0`.

## v1.0.8 — 2026-09-17

### Added
- **`avdslim enable` Command**: Selectively re-enable specific features (`bluetooth`, `animations`, `sync`, `location`, `bglimit`) or app packages (`maps`, `photos`, `chrome`, `camera`, `store`, etc.) on running AVDs without performing a full restore. Supports index, AVD name, serial, and `--all` multi-device targeting.
- **Bluetooth Debloating & `--skip bluetooth`**: Added Bluetooth subsystem (`com.google.android.bluetooth`, `com.android.bluetoothmidiservice`, `bluetooth_on`) debloating, saving ~25 MB RAM. Added `--skip bluetooth` option for developers testing BLE peripherals.
- **Privacy Sandbox Debloating**: Silenced `com.google.android.adservices.api` and `com.google.mainline.adservices` background attribution daemons.
- **Continuous Integration (CI)**: Added GitHub Actions CI workflow to run formatting checks, `go vet`, tests, and binary compilation on push and pull requests.

### Changed
- **Animation Default Inverted**: Animations are now ON by default (Fluid 1.0x) for natural UI navigation. Added `--no-anim` / `--no-animations` flag to explicitly disable animations (0x) for instant UI response.
- **Background Process Capping**: Configured `max_cached_processes=4` alongside `background_process_limit=4` to eliminate hidden cached app RAM bloat on Android 14–37.
- **CPU Thread Tuning**: Set `hw.cpu.ncore=2` in `tune-avd` to curb host thread stack allocation and parallel compilation thread churn.
- **PhotoPicker Protected**: Excluded `com.google.android.photopicker` by default to ensure modern `ActivityResultContracts.PickVisualMedia` dialogs remain functional.

## v1.0.7 — 2026-09-17

### Added
- **OS-Specific GPU Backends**: Automatically selects `-gpu host` on macOS (native Apple Metal acceleration), `-gpu auto` on Linux and Windows (native DRI/Vulkan and Direct3D 11 via ANGLE), and `-gpu swiftshader_indirect` on headless Linux CI runners without display servers.
- **Flexible Device Target Resolution**: Commands (`stop`, `snapshot`, `on`, `off`, `bench`) now accept numeric 1-based index numbers (`1`, `2`), AVD names (`Slim_Pixel_5`), or ADB serials (`emulator-5554`).
- **Physical Keyboard Forwarding**: Automatically sets `hw.keyboard = yes` during `tune-avd` so typing on host keyboards works out-of-the-box inside the emulator.

### Changed
- **Default RAM Raised to 1536 MB**: Raised default guest RAM from 1024 MB to 1536 MB across CLI flags, Android Studio shim, AVD tuner, bake snapshots, and GitHub Actions workflows. Balances lightweight host memory (~1.7 GB) with guest OS `lmkd` headroom, preventing Gboard and dev application kills on Android API 34–37 images.

### Fixed
- Fixed `avdslim stop` to correctly target emulators by index, name, or serial with responsive termination polling.
- Fixed `github-repo-stats` workflow downsampling ZeroDivisionError on new or sparse data branches.

## v1.0.6 — 2026-09-17

### Upgrading
- **Run `avdslim install-shim` again after upgrading** if you use the Android
  Studio shim. Shims installed by 1.0.5 or earlier don't read the new defaults
  file. `avdslim doctor` flags an outdated shim.
- `go install github.com/kdbhalala/avdslim/cmd/avdslim@latest` works again
  (the module path now matches the repository).
- Emulators slimmed by 1.0.5 or earlier have no saved setting values, so
  `avdslim off` still resets them to stock (animations 1x, location on).
  Run `avdslim off` then `avdslim on` once to start saving your own values.

### Added
- `--skip=animations,bglimit,sync,location,setup` on `on`, `watch`, `start`,
  `bake` and `snapshot` to leave those settings unchanged.
- Per-user defaults file (`~/Library/Application Support/avdslim/defaults` on
  macOS, `~/.config/avdslim/defaults` on Linux). The shim reads `--ram` from
  it at every launch.
- `doctor` and `watch` warn when an Android Studio emulator update has
  replaced the shim.
- Release tarballs carry build provenance attestations
  (`gh attestation verify`).
- `TRADEMARKS.md`: naming rules for forks and the list of official channels.

### Changed
- `avdslim off` restores each setting to the value it had before
  `avdslim on`, instead of fixed stock values.
- `start` passes `--aggressive`, `--keep` and `--skip` through to slimming.
- `install-shim` refuses on Windows with an explanation (the script wrapper
  never worked there); `uninstall-shim` still removes an old one.

### Fixed
- `avdslim version` now reports the release tag.
