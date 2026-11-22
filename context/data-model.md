# Data model — avdslim

State lives in host or guest files and live emulator flags.

## Entities

- **AVD config** — `~/.android/avd/<name>.avd/config.ini` (or
  `$ANDROID_AVD_HOME`). Flat `key=value`, parsed into `map[string]string`
  (`config.GetInstalledAvds`). Tuned keys: `hw.ramSize` (default 1536),
  `vm.heapSize` (256), `hw.gpu.mode`/`hw.gpu.enabled`, `hw.cpu.ncore=2`,
  `hw.keyboard=yes`, camera/audio off, `fastboot.forceColdBoot=yes`. One-time backup
  `config.ini.bak` (never overwritten once present). Owner: `internal/config/tuner.go`.
- **Golden snapshot** — directory `<avd>.avd/snapshots/avdslim_clean/`.
  Existence = "baked" (`config.HasGoldenSnapshot` = dir check only; no
  validity check — corrupt/partial dirs read as present). Created by `bake`
  (cold boot → slim → `emu avd snapshot save`) or `snapshot`/`stop --snap`
  (live state incl. installed apps/logins). Deleted by `unbake` (`RemoveAll`).
  Consumed by `start`/`launch` and the shim as
  `-snapshot avdslim_clean -no-snapshot-save`.
- **Slim state** — `/data/local/tmp/avdslim_state.json` on the device.
  Version 1 records the preset, timestamp and package names in `disabled_packages`.
  `package_states` maps those names to user 0's original override (0 for app
  default, 1 for explicitly enabled, or `null` for a migrated legacy entry).
  An interrupted package disable leaves `applying: true` for recovery with `off`.
  `settings` maps each recorded `namespace/name` to its original value
  (`null` means unset). Re-slimming preserves these originals.
  Unversioned records restore only listed packages. A pre-1.0.6 record with
  listed packages and no settings retains its historical stock-settings
  fallback on migration (`legacy_settings`). A missing journal makes `off`
  a no-op. An invalid journal returns an error and stays available for inspection.
  Factory reset wipes the journal. `IsSlimmed` is derived from its existence.
- **Bloat lists** — compile-time constants in `internal/bloat/packages.go`:
  `StandardBloatCategories map[string][]string` (~50 pkgs across 9 categories),
  `AggressiveBloatPackages` (5 pkgs: Play Store updater, Chrome, setup wizards).
  Not user-editable except per-invocation `--keep=<pkg>`.
- **User defaults** — `os.UserConfigDir()/avdslim/defaults` (macOS
  `~/Library/Application Support/avdslim/defaults`, Linux
  `~/.config/avdslim/defaults`): whitespace-separated flags, `#` comments.
  Prepended to args of `on`/`watch`/`tune-avd`/`start`/`bake`/`snapshot`/
  `install-shim` (CLI flags win). The shim reads `--ram` from it at launch.
  Owner: `internal/config/defaults.go`.
- **Shim marker** — SDK `emulator/` dir holds either stock binary or
  (`emulator.real` binary + `emulator` bash script containing
  `# avdslim emulator shim`). `IsShimInstalled` checks both;
  `IsShimOverwritten` = `emulator.real` present but `emulator` isn't the
  script (an SDK emulator update replaced it). Unix only: `InstallShim`
  refuses on Windows.

## Ownership boundaries

- `config` owns host files (SDK/AVD paths, `config.ini`, snapshot dirs).
- `adb.Client` owns guest state (`pm`, `settings`, state JSON, `emu` console).
- `host` owns nothing persistent — pure live process probing.
- `main.go` handlers orchestrate but persist nothing themselves.
