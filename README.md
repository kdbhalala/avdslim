# ⚡ AVD-SLIM™

> **Android Emulator RAM & CPU Optimizer**  
> *Inspired by [MobAI-App/simslim](https://github.com/MobAI-App/simslim) for iOS simulators.*

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/kdbhalala/avdslim)](https://goreportcard.com/report/github.com/kdbhalala/avdslim)
[![Release](https://img.shields.io/github/v/release/kdbhalala/avdslim)](https://github.com/kdbhalala/avdslim/releases)
[![GitHub Marketplace](https://img.shields.io/badge/Marketplace-AVD--SLIM-blue?logo=github-actions&logoColor=white)](https://github.com/marketplace/actions/avd-slim-android-emulator-ram-ci-optimizer)

`avdslim` is a lightweight, zero-dependency CLI tool that cuts Android Virtual Device (AVD) host memory from **~8.5 GB to ~2.5 GB** in Activity Monitor (**~1.5 GB** of it actually resident) and cuts idle CPU overhead to near-zero on Apple Silicon & Linux.

```
┌────────────────────────────────────────────────────────────────────────┐
│  Activity Monitor (phys_footprint), Pixel 10 Pro:                      │
│  Before:  qemu-system-aarch64  ██████████████████████████  8,518 MB    │
│  After:   qemu-system-aarch64  ████████                    2,498 MB    │
│                                                                        │
│  ⚡ Reclaimed: ~6.0 GB host RAM (71% reduction)                        │
└────────────────────────────────────────────────────────────────────────┘
```

## ⚡ Quick Start

```bash
brew tap kdbhalala/avdslim https://github.com/kdbhalala/avdslim.git && brew install avdslim
avdslim doctor      # read-only audit: what is costing RAM on this machine
avdslim tune-avd    # 1536 MB + host GPU in the AVD's config.ini (backup kept)
avdslim start       # launch with -lowram; slims the guest once it boots
avdslim off         # undo the guest changes any time
```

Launching from Android Studio's ▶ button instead? See the optional
[`install-shim`](#1-android-studio-1-click-integration-install-shim) below.

---

## 🛡️ Fidelity & Safety Guarantee

The #1 fear with debloating tools is silent breakage. `avdslim` is designed to be **safe by default**:

| Subsystem / Service | Status | Guarantee |
| :--- | :---: | :--- |
| **Firebase Cloud Messaging (FCM)** | ✅ **100% Active** | `GcmService` allowlisted; push notifications work out of the box |
| **Firebase Auth & Google Sign-In** | ✅ **100% Active** | Core `com.google.android.gms` APIs are protected and never disabled |
| **Android System WebView** | ✅ **100% Active** | Chromium engine, JavaScript, and in-app browsers untouched |
| **Flutter / React Native / Native** | ✅ **100% Active** | Hot reload, DevTools, debugging, and JNI/NDK runtimes work 100% |
| **Localhost & Network Sockets** | ✅ **100% Active** | TCP/UDP, Metro bundler (`:8081`), and adb reverse unaffected |
| **Full Revert (`off`)** | ✅ **One Command** | `avdslim off` re-enables every package avdslim disabled and puts each changed setting back to its previous value. If a package can't be re-enabled, it says which and `off` can be re-run. After `avdslim bake`, also run `avdslim unbake` (the snapshot is slimmed). |

---

## 💡 Golden SDK Recommendation: Which System Image to Choose?

When creating Virtual Devices in **Android Studio Device Manager**, your choice of system image makes an enormous difference in RAM consumption:

| System Image Type | Status | Why? |
| :--- | :---: | :--- |
| **Google APIs** *(Standard 4 KB)* | ✅ **ALWAYS USE (Best)** | **100% Firebase Auth, FCM Push & Maps active** with zero Play Store background updaters. Allows `adb root` so `avdslim` can compact kernel memory. **Runs ultra-smooth at 1536 MB RAM**. |
| **Google Play** | ❌ **AVOID** | Runs heavy Play Store self-updaters and background Play Protect scanning loops. Production build locks out `adb root` (cannot flush kernel caches). Consumes ~40% more RAM. |
| **16 KB Page Size** *(`ps16k`)* | ❌ **AVOID** | Hardcodes a **4,096 MB minimum RAM ceiling in QEMU** (ignoring low-memory flags). Uses 4x larger page buffers. Only use if specifically debugging 16K native C/C++ alignment. |

---

## 🎯 The Problem

When developing Android apps on macOS or Linux, developers often discover `qemu-system-aarch64` consuming **5 GB to 8+ GB of RAM** in Activity Monitor.

### Why Does the Emulator Consume 8 GB?
1. **The Lavapipe Trap**: Android Studio frequently defaults `hw.gpu.mode = auto`, which falls back to Mesa CPU software rasterization (`lavapipe`). This allocates **~4 GB of software rendering buffers** directly in host RAM on top of the guest OS RAM.
2. **16 KB Page Size Images**: On modern ARM64 images (`google_apis_ps16k`), QEMU hardcodes a minimum RAM threshold (`minRam = 4096MB`), silently overriding lower RAM settings.
3. **Android Bloatware**: Over 35 non-essential daemons (Google Assistant, System Intelligence, Maps, Photos, YouTube, telemetry) wake CPU cores and pollute memory.
4. **Stale Snapshots**: Android Studio re-loads cached snapshots (`hardware-qemu.ini`) that preserve heavy 4 GB states across reboots.

---

## 💡 The Solution

Just like `simslim` silences iOS simulators via `launchctl`, `avdslim`:
1. **Passes `-lowram` to QEMU**: Removes the internal 4 GB lower bound and boots the Android kernel in low-RAM mode (`hw.ramSize = 1024M` or `1536M`).
2. **Enforces Cross-Platform GPU Acceleration**: Forces `-gpu host` to render natively via host GPU drivers, completely bypassing CPU software rasterizers:
   - **macOS (Apple Silicon / Intel)**: Native Apple Metal hardware acceleration.
   - **Linux / Ubuntu (Desktop)**: Native DRI / OpenGL / Vulkan via Mesa / NVIDIA drivers (`/dev/dri`).
   - **Linux / Ubuntu (Headless CI / Docker)**: Auto-detects headless environments (no `$DISPLAY`) and uses Google SwiftShader (`-gpu swiftshader_indirect`) to avoid display server crashes while bounding memory.
   - **Windows 10 / 11**: Direct3D 11 via ANGLE or native Desktop OpenGL / Vulkan.
3. **Disables 45+ Bloat Daemons**: Silences non-essential Google background services (Bluetooth, Privacy Sandbox, telemetry, wearable/camera stubs) via `pm disable-user --user 0`.
4. **Fluid or Zero-Latency Animations**: Fluid 1.0x animations by default; toggleable to 0x instant transitions via `--no-anim`.
5. **Limits Background Churn**: Caps `background_process_limit = 4` and `max_cached_processes = 4` (protecting OAuth and biometrics) and disables auto-sync.
6. **Drops Caches**: Flushes Linux page caches and compacts memory heaps.

---

## 📊 Memory Footprint Breakdown

| Stage | Activity Monitor (`phys_footprint`) | Active Dirty RAM (`footprint`) | Reclaimed |
| :--- | :--- | :--- | :--- |
| **Default Stock Emulator** (Pixel 10 Pro) | **8,518 MB (8.5 GB)** | ~6,500 MB | Baseline |
| **With `avdslim launch` (`-lowram`, Metal GPU)** | **2,498 MB (2.5 GB)** | **1,560 MB (1.5 GB)** | **~6.0 GB saved (71%)** |
| **Standard 1080p Profile** (Pixel 5) | **2,325 MB (2.3 GB)** | **1,306 MB (1.3 GB)** | **~6.2 GB saved (73%)** |

> **Note on Activity Monitor vs Dirty RAM**:  
> macOS Activity Monitor reports `phys_footprint` from Apple's Mach kernel ledger. This includes ~530 MB of compressed pages from the initial boot spike and Metal GPU display pipeline buffers. The actual dirty physical memory held in RAM is **~1.5 GB** (verified with `footprint -p <pid>`).

---

## 🚀 Installation

### Option 1: One-Line Installer (Fastest)
```bash
curl -fsSL https://raw.githubusercontent.com/kdbhalala/avdslim/main/install.sh | bash
```

### Option 2: Homebrew (macOS / Linux directly from this repo)
```bash
brew tap kdbhalala/avdslim https://github.com/kdbhalala/avdslim.git
brew install avdslim
```

> **Official builds only** come from the channels listed in [TRADEMARKS.md](TRADEMARKS.md#official-channels). To verify a downloaded release:
> ```bash
> shasum -a 256 -c checksums.txt --ignore-missing
> gh attestation verify avdslim_<version>_<os>_<arch>.tar.gz --repo kdbhalala/avdslim
> ```

### Option 3: Go Install
```bash
go install github.com/kdbhalala/avdslim/cmd/avdslim@latest
```

### Option 4: Pre-built Binaries
Download pre-compiled binaries from [GitHub Releases](https://github.com/kdbhalala/avdslim/releases):
* macOS Apple Silicon: `avdslim_*_darwin_arm64.tar.gz`
* macOS Intel: `avdslim_*_darwin_amd64.tar.gz`
* Linux: `avdslim_*_linux_amd64.tar.gz` / `avdslim_*_linux_arm64.tar.gz`
* Windows: `avdslim_*_windows_amd64.zip` / `avdslim_*_windows_arm64.zip` (unzip and put `avdslim.exe` on your `PATH`; `install-shim` is not available on Windows)

### Option 5: Build from Source (100% Stdlib, Zero Dependencies)
```bash
git clone https://github.com/kdbhalala/avdslim.git
cd avdslim
make install
```

---

## 🛠️ Usage & Workflows

### 1. Android Studio 1-Click Integration (`install-shim`)
Prefer clicking the green **"Play"** button in Android Studio? Wrap the SDK emulator binary once:
```bash
avdslim install-shim
```
* **Zero workflow changes**: Android Studio launches automatically stay slimmed (1536 MB, `-lowram`, Metal GPU).
* **Safe & reversible anytime**: `avdslim uninstall-shim` restores the original SDK binary instantly.
* **After an emulator update in Android Studio**, the SDK Manager replaces the shim and launches go back to stock. `avdslim doctor` and `avdslim watch` warn about it; run `avdslim install-shim` again. An update can also invalidate the golden snapshot, so re-run `avdslim bake` if boots are slow again.
* **Not supported on Windows**: Android Studio runs `emulator.exe` directly, so a script wrapper can't replace it. Use `avdslim start` and `avdslim tune-avd` instead, or one of the options below.

### VS Code / Flutter (any OS, no shim)
* **Easiest**: run `avdslim start <avd>` first, then press Run. Flutter uses the emulator that is already running.
* **Pick it from VS Code's device list**: add a custom emulator to your VS Code `settings.json` (Dart-Code extension) that launches through avdslim:
  ```json
  "dart.flutterCustomEmulators": [
    { "id": "avdslim-medium", "name": "Medium_Phone (avdslim)", "executable": "avdslim", "args": ["start", "Medium_Phone"] }
  ]
  ```
* `avdslim tune-avd` alone also helps IDE launches (RAM size and GPU live in the AVD's `config.ini`), but it turns off quick boot, so without `avdslim start` or the shim every launch is a cold boot and skips the Golden Snapshot.

> **Windows status: community-tested.** The Windows builds are not tested on real hardware by the maintainer. `list`/`measure` don't show host memory on Windows yet. Reports in [issue #1](https://github.com/kdbhalala/avdslim/issues/1) are welcome.

---

### 2. Live Efficiency Benchmark (`bench`)
Print a live before/after scoreboard comparing stock flagship consumption against your running slimmed AVD:
```bash
avdslim bench
```

---

### 3. Golden Snapshot: ~1.5-Second Instant Boot (`bake`)
Cold booting Android emulators typically takes 35–60 seconds. `avdslim bake` cold boots your emulator once, applies all bloat pruning and memory optimizations, and saves an immutable `avdslim_clean` snapshot:
```bash
avdslim bake
# Or specify AVD name or index:
avdslim bake Pixel_10_Pro
# Headless baking (for CI or background):
avdslim bake 1 --headless
```
* **~1.5s instant restore**: Subsequent launches (`avdslim start` or Android Studio via shim) restore from the clean snapshot in < 2 seconds.
* **Ephemeral safety (`-no-snapshot-save`)**: Dev sessions never pollute the snapshot. Every reboot starts 100% clean and slimmed.

---

### 4. Snapshot Live Configured State (`snapshot`)
Want your test apps, debug build, local database, or test account logins preserved in the instant restore snapshot?
1. Launch your emulator: `avdslim start`
2. Install your apps, log into test accounts, and configure your test environment.
3. Lock this exact state into your Golden Snapshot:
```bash
avdslim snapshot
# Or alias:
avdslim bake --live
```
Now, every future launch restores your pre-installed apps and credentials instantly in **< 1.5s**!

---

### 5. Remove / Reset Golden Snapshot (`unbake`)
If an Android SDK image updates or you want to return to stock cold boots:
```bash
avdslim unbake
# Or specify AVD name or index:
avdslim unbake Pixel_10_Pro
```

---

### 6. Zero-Friction Watch Mode (`watch`)
Don't want to change your workflow? Run `avdslim watch` in the background. Whenever you launch an emulator from Android Studio or VS Code, `avdslim` detects it and automatically silences bloat as soon as it boots:
```bash
avdslim watch
```
*(Options: `--aggressive`, `--keep=<package>`, `--skip=<groups>`)*.

---

### 7. Instant Undo / Restore (`restore`, `off`)
Need to verify a bug with 100% stock Google services? One command re-enables the packages avdslim disabled and puts every setting it changed back to the value it had before (e.g. your 0.5x animations stay 0.5x):
```bash
avdslim restore
# Or use alias:
avdslim off
```

---

### 8. Selectively Re-enable Features (`enable`)
Want to turn on just **one feature or app** on a running, slimmed AVD without doing a full restore? Use `avdslim enable`:
```bash
# Enable by feature:
avdslim enable bluetooth
avdslim enable animations
avdslim enable sync
avdslim enable location

# Enable by app alias or package:
avdslim enable maps
avdslim enable chrome
avdslim enable com.google.android.apps.photos

# Target specific emulator (when multiple are running):
avdslim enable bluetooth 1               # by index (1, 2)
avdslim enable maps Slim_Pixel_5         # by AVD name
avdslim enable bluetooth --all           # across all running emulators
```

**Host features** (audio, cameras, D-Pad, boot animation) live in the AVD's
`config.ini`, not on the device, so they need no running emulator — and a cold
boot to take effect:
```bash
avdslim enable audio                     # hw.audioInput/Output=yes, drops -no-audio
avdslim enable camera Pixel_10_Pro       # emulated cameras + the guest camera apps
avdslim enable dpad
avdslim enable bootanim                  # drops -no-boot-anim

avdslim disable audio                    # back to the slimmed default
```
`enable` writes an `avdslim.<feature>=yes` marker that `start`, `bake`,
`tune-avd` and the Android Studio shim all honor, so the setting survives
re-tuning. Apply it with `avdslim restart <avd>`; if the AVD has a Golden
Snapshot, re-bake it (`avdslim bake <avd>`) since the snapshot carries the old
hardware config. After upgrading avdslim, run `avdslim install-shim` again so
Studio launches pick up the markers.

📖 **Full list of every feature, alias and package you can enable or disable:
[docs/FEATURES.md](docs/FEATURES.md).**

---

### 9. Slim an Active Emulator (`on`)
Immediately silences background bloat and trims memory on a running emulator:
```bash
# Standard preset (safe for all apps):
avdslim on

# Aggressive preset (also disables Play Store self-updater):
avdslim on --aggressive

# Keep a specific app (e.g. Google Maps):
avdslim on --keep=com.google.android.apps.maps

# Disable animations for instant, zero-latency transitions (otherwise ON by default):
avdslim on --no-anim

# Leave some settings alone:
avdslim on --skip=sync,location
```
`--no-anim` (or `--skip`) also works with `watch`, `start`, `bake` and `snapshot`. Groups:

| Group | Settings left unchanged |
| :--- | :--- |
| `animations` | window, transition and animator scales (ON by default; disable with `--no-anim`) |
| `bluetooth` | `bluetooth_on` & Bluetooth packages (otherwise disabled to save ~25 MB RAM) |
| `bglimit` | `max_cached_processes` & `background_process_limit` (otherwise 4) |
| `sync` | `auto_sync` (otherwise off) |
| `location` | `location_mode` (otherwise off) |
| `setup` | `user_setup_complete`, `device_provisioned` (otherwise marked done) |

#### Your own defaults
Put flags you always want in a defaults file. They apply to `on`, `watch`, `tune-avd`, `create`, `start`, `bake`, `snapshot` and `install-shim`; flags typed on the command line win. Lines starting with `#` are comments.

* macOS: `~/Library/Application Support/avdslim/defaults`
* Linux: `~/.config/avdslim/defaults`

```
# e.g.
--ram=2048 --skip=animations --keep=com.google.android.apps.maps
```
The Android Studio shim reads `--ram` from this file at every launch, so changing it needs no reinstall. `avdslim doctor` shows the file it found.

---

### Repair a Stuck AVD (`repair`)

AVD hangs on a black screen after a stop/start? avdslim ≤ 1.0.8 disabled
`com.google.android.bluetooth`, which Android 16+ needs to boot. With the
stuck emulator still running:
```bash
avdslim repair            # or: avdslim repair emulator-5554
```
Re-enables boot-critical packages and restarts the framework. Needs `adb root`
(Google APIs images). On Play Store images, cold boot with `-wipe-data` instead.

### 9. Deep Memory Breakdown (`measure`)
Inspect host macOS memory (`phys_footprint`, resident RSS) alongside the guest Android `dumpsys meminfo`:
```bash
avdslim measure
# Or specify serial:
avdslim measure emulator-5554
```

---

### 10. Tune Host AVD Configuration (`tune-avd`)
Configures an AVD's `config.ini` for optimal memory consumption and purges stale snapshots:
```bash
avdslim tune-avd Pixel_10_Pro --ram=1536 --heap=256
```
* Sets `hw.ramSize = 1536`
* Sets `hw.gpu.mode = host` (Apple Silicon Metal hardware acceleration)
* Disables camera and audio emulation threads
* Purges stale `hardware-qemu.ini` and snapshots

### Create a Slim AVD (`create`)
Makes a new AVD that passes `doctor` from the start, with no Android Studio clicking:
```bash
avdslim create Slim_Pixel              # newest installed Google APIs image
avdslim create Slim_Pixel --api=35 --device=pixel_6 --ram=2048
```
* Picks the newest installed **Google APIs 4 KB** image for your CPU (`arm64-v8a` or
  `x86_64`); Play Store and 16 KB page-size images are never used
* Runs `avdmanager create avd`, then applies `tune-avd` (backup kept as `config.ini.bak`)
* Refuses to overwrite an existing AVD
* Never downloads images or accepts licenses for you. If no suitable image is
  installed it prints the `sdkmanager` command to run.
* Needs the SDK Command-line Tools (`avdmanager`) and Java 17+

---

### 11. Restart Emulator with Clean Cache (`restart`)
Gracefully shuts down the emulator, purges stale runtime snapshots, and relaunches with low-memory host flags:
```bash
avdslim restart emulator-5554 --ram=1536
```

---

### 12. Start / Launch Emulator (`start`, `run`, `launch`)
Starts an AVD with low-memory host flags and auto-slims upon boot. If a Golden Snapshot exists, it boots in **<1.5s** automatically:
```bash
# Interactive numbered menu (press 1, 2, or hit Enter for default)
avdslim start

# Select directly by index number
avdslim start 1

# Multi-window / split-screen testing (disables -lowram kernel flag)
avdslim start 1 --no-lowram

# Headless mode (for CI runners or automated testing)
avdslim start 1 --headless

# Force a cold boot without loading snapshot
avdslim start 1 --cold

# Custom RAM allocation
avdslim run Pixel_10_Pro --ram=1536

# Skip auto-slimming if you need stock services untouched
avdslim start 1 --no-slim
```

---

### 13. Graceful Stop with Snapshot Prompt (`stop`, `kill`)
Gracefully shuts down the emulator. Optionally updates the Golden Snapshot with your session's state:
```bash
# Interactive (prompts if you want to save current state):
avdslim stop

# Automatically trim bloat, snapshot state, and exit:
avdslim stop --snap

# Immediate force exit without snapshotting:
avdslim stop -f
```

---

### 14. Environment Doctor (`doctor`)
Audits your Android toolchain, active AVDs, Golden Snapshots, 16K page size overhead, and warns about software GPU fallback:
```bash
avdslim doctor
```

---

### 15. View Bloat Profiles (`profiles`)
Inspects the list of disabled packages categorized by function (Assistant, Telephony, Consumer Bloat, etc.) and guaranteed core services:
```bash
avdslim profiles
```

---

## ☁️ GitHub Actions CI Integration
Official GitHub Marketplace Action: **[AVD-SLIM — Android Emulator RAM & CI Optimizer](https://github.com/marketplace/actions/avd-slim-android-emulator-ram-ci-optimizer)**

Slash CI runner memory and run parallel emulator shards on free GitHub Actions runners:

```yaml
- name: AVD-SLIM — Android Emulator RAM & CI Optimizer
  uses: kdbhalala/avdslim@v1
  with:
    ram: '1536'
    install-shim: 'true'
```

---

## 📜 License

MIT License. See [LICENSE](LICENSE) for details.

The AVD-SLIM™ and avdslim™ names are not covered by MIT. Official channels and naming rules for forks are in [TRADEMARKS.md](TRADEMARKS.md).
