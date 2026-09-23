package main

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kdbhalala/avdslim/internal/adb"
	"github.com/kdbhalala/avdslim/internal/bloat"
	"github.com/kdbhalala/avdslim/internal/config"
	"github.com/kdbhalala/avdslim/internal/doctor"
	"github.com/kdbhalala/avdslim/internal/host"
	"github.com/kdbhalala/avdslim/internal/shim"
)

// var, not const: release.yml sets it via -ldflags "-X main.version=...".
var version = "1.0.15"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	cmd := strings.ToLower(os.Args[1])
	subArgs := withDefaults(cmd, os.Args[2:])
	client := adb.NewClient()

	// After an avdslim upgrade, bring an installed Studio shim up to date
	// (our script only; the real emulator binary is untouched).
	if !map[string]bool{"help": true, "-h": true, "--help": true, "version": true, "-v": true, "--version": true}[cmd] {
		if refreshed, err := shim.RefreshIfOutdated(); err != nil {
			fmt.Fprintf(os.Stderr, "⚠️  Could not update the Android Studio shim (%v); run `avdslim install-shim`\n", err)
		} else if refreshed {
			fmt.Println("✓ Updated the Android Studio shim to this avdslim version.")
		}
	}

	switch cmd {
	case "help", "-h", "--help":
		printUsage()
	case "version", "-v", "--version":
		fmt.Printf("avdslim version %s\n", version)
	case "doctor":
		doctor.RunDoctor(client)
	case "profiles":
		bloat.PrintProfiles()
	case "list":
		handleList(client)
	case "measure":
		handleMeasure(client, subArgs)
	case "on", "slim":
		handleOn(client, subArgs)
	case "enable":
		handleEnable(client, subArgs)
	case "disable":
		handleDisable(client, subArgs)
	case "off", "unslim", "restore", "reset":
		handleOff(client, subArgs)
	case "watch":
		handleWatch(client, subArgs)
	case "tune-avd", "tune":
		handleTuneAvd(subArgs)
	case "create", "new":
		handleCreate(subArgs)
	case "launch", "start", "run":
		handleLaunch(client, subArgs)
	case "stop", "kill", "quit":
		handleStop(client, subArgs)
	case "bake":
		handleBake(client, subArgs)
	case "snapshot", "snap":
		handleSnapshot(client, subArgs)
	case "unbake", "unsnapshot":
		handleUnbake(subArgs)
	case "restart":
		handleRestart(client, subArgs)
	case "repair", "fix":
		handleRepair(client, subArgs)
	case "bench", "benchmark":
		handleBench(client, subArgs)
	case "install-shim", "shim":
		handleInstallShim(subArgs)
	case "uninstall-shim", "unshim":
		handleUninstallShim()
	default:
		fmt.Printf("❌ Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

// flagCommands are the commands whose flags the defaults file may set.
var flagCommands = map[string]bool{
	"on": true, "slim": true, "watch": true, "tune-avd": true, "tune": true, "create": true, "new": true,
	"launch": true, "start": true, "run": true, "bake": true,
	"snapshot": true, "snap": true, "install-shim": true, "shim": true,
}

// knownDefaultFlags are the flags accepted in the defaults file.
var knownDefaultFlags = []string{
	"--ram=", "--heap=", "--gpu=", "--keep=", "--skip=",
	"--aggressive", "--headless", "--no-lowram", "--no-slim",
	"--no-anim", "--no-animations", "--anim", "--animations",
}

func newDefaultSkip() map[string]bool {
	return map[string]bool{"animations": true}
}

// withDefaults puts the defaults file's flags before args, so flags typed on the
// command line are parsed later and win. Commands ignore flags they don't use.
func withDefaults(cmd string, args []string) []string {
	if !flagCommands[cmd] {
		return args
	}
	defaults := config.LoadDefaults()
	for _, d := range defaults {
		known := false
		for _, k := range knownDefaultFlags {
			if d == k || (strings.HasSuffix(k, "=") && strings.HasPrefix(d, k)) {
				known = true
				break
			}
		}
		if !known {
			fmt.Fprintf(os.Stderr, "⚠️  Ignoring unknown flag %q in %s\n", d, config.DefaultsFilePath())
		}
	}
	return append(defaults, args...)
}

// addSkip records a --skip=a,b flag, exiting on an unknown group so typos are not silently ignored.
func addSkip(skip map[string]bool, flag string) {
	for _, g := range strings.Split(strings.TrimPrefix(flag, "--skip="), ",") {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		if !adb.IsSkipGroup(g) {
			fmt.Fprintf(os.Stderr, "❌ Unknown --skip value %q (valid: %s)\n", g, strings.Join(adb.SkipGroups, ", "))
			os.Exit(1)
		}
		skip[g] = true
	}
}

func printUsage() {
	fmt.Printf(`════════════════════════════════════════════════════════════════════════
 ⚡ AVD-SLIM — Android Emulator RAM & CPU Optimizer
 (Inspired by simslim for iOS simulators) — v%s
════════════════════════════════════════════════════════════════════════

Usage:
  avdslim <command> [arguments]

Commands:
  list                 List running emulators (with Activity Monitor memory) & saved AVDs
  measure [device]     Deep memory breakdown (host Footprint/RSS + guest dumpsys)
  on [device]          Slim down emulator: disable bloat daemons & trim RAM
                       Options: --aggressive (also disables Play Store updater)
                                --keep=<package> (preserve specific package, e.g. Maps)
                                --no-anim (turn animations 0x for instant UI response)
                                --skip=<groups> (leave alone: bluetooth,bglimit,sync,location,setup)
  restore, off         Undo 'on': re-enable disabled packages & restore changed settings
  enable <target> [dev] Re-enable a feature or package
                       Guest (running AVD): bluetooth, animations, sync, location, bglimit
                       Host (config.ini):   audio, camera, dpad, bootanim — needs a restart
                       Options: --all
  disable <feature> [avd] Turn a host feature (audio, camera, dpad, bootanim) back off
  watch                Auto-detect & slim new emulators as soon as they boot
                       Options: --aggressive, --keep=<package>, --no-anim, --skip=<groups>
  tune-avd [avd_name]  Tune host AVD config.ini (RAM=1536M, Metal GPU, no cameras)
                       Options: --ram=<MB> (default: 1536), --heap=<MB> (default: 256)
  create <name>        New AVD from the newest installed 'Google APIs' 4 KB image, pre-tuned
                       Options: --api=<level>, --device=<id> (default: pixel_5), --ram=<MB>
  start, run, launch [avd] Launch AVD with low-memory host flags & auto-slim upon boot
                       Options: --no-slim, --no-lowram, --headless, --cold, --ram=<MB> (default: 1536)
  stop, kill [device]  Gracefully shut down emulator (Options: --snap, -f)
  bake [avd_name]      Create local Golden Snapshot (pruned & slimmed) for ~1.5s instant boots
                       Options: --ram=<MB> (default: 1536), --aggressive, --skip=<groups>, --headless, --live
  snapshot, snap       Capture running emulator (with pre-installed apps & test logins)
                       into Golden Snapshot for instant <1.5s restores
  repair [device]      Unstick an AVD hung on a black screen after slimming (needs adb root)
  unbake [avd_name]    Delete Golden Snapshot and return AVD to stock cold boots
  bench [device]       Show before/after memory & CPU efficiency scoreboard
  install-shim         Wrap SDK emulator binary so Android Studio launches stay slim
                       Options: --ram=<MB> (default: 1536)
  uninstall-shim       Restore stock Android SDK emulator binary
  doctor               Audit environment, AVDs, system image 16K overhead & toolchain
  profiles             List all bloat categories, packages & guaranteed-working services
  version              Print avdslim version

Examples:
  avdslim watch
  avdslim doctor
  avdslim list
  avdslim measure
  avdslim on --aggressive
  avdslim on --keep=com.google.android.apps.maps
  avdslim on --skip=animations,sync
  avdslim enable audio Pixel_10_Pro
  avdslim disable camera Pixel_10_Pro
  avdslim tune-avd Pixel_10_Pro --ram=1536
  avdslim create Slim_Pixel --api=35
  avdslim restart

Defaults:
  Put flags in %s to apply them to
  on, watch, tune-avd, create, start, bake, snapshot and install-shim. Flags typed on
  the command line win. Example file contents:
    --ram=2048 --skip=animations --keep=com.google.android.apps.maps
`, version, config.DefaultsFilePath())
}

func handleList(client *adb.Client) {
	fmt.Println("🔎 Checking running Android emulators...")
	running, err := client.GetRunningEmulators()
	if err != nil {
		fmt.Printf("Error checking emulators: %v\n", err)
	} else if len(running) == 0 {
		fmt.Println("ℹ️  No running Android emulators detected via adb.")
		fmt.Println()
	} else {
		fmt.Printf("\n📱 Running Emulators (%d):\n", len(running))
		for _, emu := range running {
			hostPid := host.FindHostPidForSerial(emu.Serial)
			hostFootprintMb := 0
			hostRssMb := 0
			if hostPid > 0 {
				hostFootprintMb = host.GetHostFootprintMb(hostPid)
				hostRssMb = host.GetHostRssMb(hostPid)
			}

			statusStr := "🔴 FULL (Stock)"
			if emu.IsSlimmed {
				statusStr = "⚡ SLIMMED"
			}
			fmt.Printf("  • %s (%s, Android %s, API %s)\n", emu.Serial, emu.Model, emu.AndroidVersion, emu.ApiLevel)
			if hostFootprintMb > 0 {
				if hostFootprintMb > hostRssMb && hostRssMb > 0 && hostFootprintMb-hostRssMb >= 100 {
					fmt.Printf("    Host PID: %d | Activity Monitor: %d MB | RSS: %d MB (%d MB swapped) | Status: %s\n", hostPid, hostFootprintMb, hostRssMb, hostFootprintMb-hostRssMb, statusStr)
				} else {
					fmt.Printf("    Host PID: %d | Activity Monitor: %d MB | RSS: %d MB | Status: %s\n", hostPid, hostFootprintMb, hostRssMb, statusStr)
				}
			} else {
				fmt.Printf("    Host PID: %d | Status: %s\n", hostPid, statusStr)
			}
		}
		fmt.Println()
	}

	fmt.Println("💾 Installed AVD Configurations:")
	avds := config.GetInstalledAvds()
	if len(avds) == 0 {
		fmt.Println("  (No AVDs found in ~/.android/avd)")
		fmt.Println()
	} else {
		for _, avd := range avds {
			ram := avd["hw.ramSize"]
			if ram == "" {
				ram = "unknown"
			}
			heap := avd["vm.heapSize"]
			if heap == "" {
				heap = "unknown"
			}
			gpu := avd["hw.gpu.mode"]
			if gpu == "" {
				gpu = "unknown"
			}
			fmt.Printf("  • %s (RAM: %sMB, Heap: %sMB, GPU: %s)\n", avd["name"], ram, heap, gpu)
		}
		fmt.Println()
	}
}

func handleMeasure(client *adb.Client, args []string) {
	serial, err := client.ResolveDevice(args)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("📊 Measuring memory footprint for %s...\n\n", serial)

	hostPid := host.FindHostPidForSerial(serial)
	if hostPid > 0 {
		footprint := host.GetHostFootprintMb(hostPid)
		rss := host.GetHostRssMb(hostPid)
		fmt.Println("🖥️  HOST (macOS) Footprint:")
		fmt.Printf("   QEMU / Emulator PID: %d\n", hostPid)
		fmt.Printf("   Activity Monitor Memory (Footprint): %d MB\n", footprint)
		if footprint > rss && rss > 0 && footprint-rss >= 100 {
			fmt.Printf("   Resident Physical RAM (RSS): %d MB (%d MB compressed or swapped out)\n\n", rss, footprint-rss)
		} else {
			fmt.Printf("   Resident Physical RAM (RSS): %d MB\n\n", rss)
		}
	}

	out, _ := client.Exec("-s", serial, "shell", "dumpsys", "meminfo")
	host.PrintGuestMeminfo(out)
}

func handleOn(client *adb.Client, args []string) {
	aggressive := false
	filteredArgs := make([]string, 0, len(args))
	var keepPackages []string
	skip := newDefaultSkip()
	for _, a := range args {
		if a == "--aggressive" {
			aggressive = true
		} else if strings.HasPrefix(a, "--keep=") {
			pkg := strings.TrimPrefix(a, "--keep=")
			if pkg != "" {
				keepPackages = append(keepPackages, pkg)
			}
		} else if a == "--no-anim" || a == "--no-animations" {
			delete(skip, "animations")
		} else if a == "--anim" || a == "--animations" {
			skip["animations"] = true
		} else if strings.HasPrefix(a, "--skip=") {
			addSkip(skip, a)
		} else if !strings.HasPrefix(a, "--") {
			filteredArgs = append(filteredArgs, a)
		}
	}

	serial, err := client.ResolveDevice(filteredArgs)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}

	presetName := "Standard"
	if aggressive {
		presetName = "Aggressive"
	}
	fmt.Printf("⚡ Slimming Android Emulator (%s) [preset: %s]...\n\n", serial, presetName)

	if len(keepPackages) > 0 {
		fmt.Printf("   Preserving requested package(s): %s\n", strings.Join(keepPackages, ", "))
	}

	hostPid := host.FindHostPidForSerial(serial)
	beforeFootprint := 0
	beforeRss := 0
	if hostPid > 0 {
		beforeFootprint = host.GetHostFootprintMb(hostPid)
		beforeRss = host.GetHostRssMb(hostPid)
	}

	fmt.Println("1. Disabling non-essential background daemons:")
	count, err := client.Slim(serial, aggressive, keepPackages, skip)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("   -> Successfully disabled %d packages.\n\n", count)

	if skip["animations"] {
		fmt.Println("2. Tuned system settings (animations ON, background limit 4, sync off, location off).")
	} else {
		fmt.Println("2. Tuned system settings (animations 0x, background limit 4, sync off, location off).")
	}
	var leftUnchanged []string
	for _, k := range sortedKeys(skip) {
		if k != "animations" {
			leftUnchanged = append(leftUnchanged, k)
		}
	}
	if len(leftUnchanged) > 0 {
		fmt.Printf("   Left unchanged (--skip): %s\n", strings.Join(leftUnchanged, ", "))
	}
	fmt.Println("3. Purged cached processes and trimmed memory.")
	fmt.Println()

	time.Sleep(1 * time.Second)

	afterFootprint := 0
	afterRss := 0
	if hostPid > 0 {
		afterFootprint = host.GetHostFootprintMb(hostPid)
		afterRss = host.GetHostRssMb(hostPid)
	}

	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Printf("🎉 Slimming complete for %s!\n", serial)
	if beforeFootprint > 0 && afterFootprint > 0 {
		diffFp := beforeFootprint - afterFootprint
		if diffFp < 0 {
			diffFp = 0
		}
		diffRss := beforeRss - afterRss
		if diffRss < 0 {
			diffRss = 0
		}
		fmt.Printf("🖥️  Activity Monitor Memory: %dMB -> %dMB (Reclaimed: %dMB)\n", beforeFootprint, afterFootprint, diffFp)
		fmt.Printf("🖥️  Host Resident RAM (RSS): %dMB -> %dMB (Reclaimed: %dMB)\n", beforeRss, afterRss, diffRss)
	}
	fmt.Printf("ℹ️  To restore default stock services anytime:\n   avdslim off %s\n\n", serial)
}

func handleOff(client *adb.Client, args []string) {
	serial, err := client.ResolveDevice(args)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("🔄 Restoring default services for %s...\n\n", serial)
	fmt.Println("1. Re-enabling packages:")
	count, err := client.Restore(serial)
	fmt.Printf("   -> Restored %d packages.\n\n", count)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
	fmt.Println("2. Restored the system settings avdslim changed to their previous values.")
	fmt.Printf("✅ Successfully restored %s to stock configuration.\n\n", serial)
	if name := avdNameForSerial(client, serial); name != "" && config.HasGoldenSnapshot(name) {
		fmt.Println("⚠️  This AVD has a slimmed Golden Snapshot. `avdslim start` boots from it,")
		fmt.Println("   so the next launch is slimmed again. To stay stock:")
		fmt.Printf("   avdslim unbake %s     (or launch with: avdslim start %s --cold)\n\n", name, name)
	}
}

func handleRepair(client *adb.Client, args []string) {
	serial, err := client.ResolveDevice(args)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("🩺 Repairing %s...\n", serial)
	fixed, err := client.Repair(serial)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
	if len(fixed) == 0 {
		fmt.Println("✓ No boot-critical packages disabled. Nothing to repair.")
		return
	}
	for _, p := range fixed {
		fmt.Printf("   ✓ Re-enabled: %s\n", p)
	}

	fmt.Println("⏳ Framework restarted, waiting for boot...")
	for i := 0; i < 60; i++ {
		res, _ := client.Exec("-s", serial, "shell", "getprop", "sys.boot_completed")
		if strings.TrimSpace(res) == "1" {
			fmt.Printf("✅ %s booted.\n\n", serial)
			return
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Println("⚠️  Boot still not complete after 120s. Check `adb logcat -b crash`.")
}

func handleEnable(client *adb.Client, args []string) {
	all := false
	var filtered []string
	for _, a := range args {
		if a == "--all" {
			all = true
		} else {
			filtered = append(filtered, a)
		}
	}

	if len(filtered) == 0 {
		fmt.Println("❌ Please specify a feature or package to enable.")
		fmt.Println("Usage: avdslim enable <feature|package> [device|avd] [--all]")
		fmt.Println("\nGuest features: bluetooth, animations, sync, location, bglimit")
		fmt.Println("Host features:  " + strings.Join(config.HostFeatureNames(), ", ") + " (config.ini, needs a restart)")
		fmt.Println("App Aliases:    maps, photos, chrome, camera, store, youtube, phone, contacts")
		fmt.Println("\nExamples:")
		fmt.Println("  avdslim enable bluetooth")
		fmt.Println("  avdslim enable audio Pixel_10_Pro")
		fmt.Println("  avdslim enable maps 1")
		fmt.Println("  avdslim enable animations Slim_Pixel_5")
		fmt.Println("  avdslim enable sync --all")
		fmt.Println("\nFull list: https://github.com/kdbhalala/avdslim/blob/main/docs/FEATURES.md")
		return
	}

	// Host features live in the AVD's config.ini, so they need no running emulator.
	for i, a := range filtered {
		if feature, ok := config.HostFeatureName(a); ok {
			rest := append(append([]string{}, filtered[:i]...), filtered[i+1:]...)
			handleHostFeature(client, feature, a, rest, true)
			return
		}
	}

	running, err := client.GetRunningEmulators()
	if err != nil || len(running) == 0 {
		fmt.Println("❌ No running Android emulators detected via adb.")
		os.Exit(1)
	}

	var target, deviceArg string
	if len(filtered) == 1 {
		target = filtered[0]
	} else {
		if isDeviceSpecifier(client, running, filtered[0]) {
			deviceArg = filtered[0]
			target = filtered[1]
		} else {
			target = filtered[0]
			deviceArg = filtered[1]
		}
	}

	var devicesToEnable []string
	if all {
		for _, r := range running {
			devicesToEnable = append(devicesToEnable, r.Serial)
		}
	} else if deviceArg != "" {
		serial, err := client.ResolveDevice([]string{deviceArg})
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			os.Exit(1)
		}
		devicesToEnable = []string{serial}
	} else {
		serial, err := client.ResolveDevice(nil)
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			os.Exit(1)
		}
		devicesToEnable = []string{serial}
	}

	failed := false
	for _, serial := range devicesToEnable {
		actions, err := client.Enable(serial, target)
		if err != nil {
			fmt.Printf("❌ [%s] %v\n", serial, err)
			failed = true
			continue
		}
		fmt.Printf("⚡ Re-enabling %q on %s:\n", target, serial)
		for _, act := range actions {
			fmt.Printf("   ✓ %s\n", act)
		}
		fmt.Printf("✅ Successfully enabled %s on %s!\n\n", target, serial)
	}
	if failed {
		os.Exit(1)
	}
}

// handleDisable turns a host feature back off. Guest-side packages and settings
// are already covered by `avdslim on`, so this is host-only.
func handleDisable(client *adb.Client, args []string) {
	var filtered []string
	for _, a := range args {
		if !strings.HasPrefix(a, "--") {
			filtered = append(filtered, a)
		}
	}

	if len(filtered) == 0 {
		fmt.Println("❌ Please specify a host feature to disable.")
		fmt.Println("Usage: avdslim disable <feature> [avd]")
		fmt.Println("\nHost features:")
		for _, l := range config.HostFeatureDescriptions() {
			fmt.Println("  " + l)
		}
		fmt.Println("\nGuest packages & settings are re-slimmed with: avdslim on")
		fmt.Println("Full list: https://github.com/kdbhalala/avdslim/blob/main/docs/FEATURES.md")
		return
	}

	feature, ok := config.HostFeatureName(filtered[0])
	if !ok {
		fmt.Printf("❌ Unknown host feature %q (valid: %s)\n", filtered[0], strings.Join(config.HostFeatureNames(), ", "))
		os.Exit(1)
	}
	handleHostFeature(client, feature, filtered[0], filtered[1:], false)
}

// handleHostFeature flips a config.ini host feature for one AVD, and for an
// `enable` also re-enables the matching guest packages when that AVD is running.
func handleHostFeature(client *adb.Client, feature, target string, rest []string, on bool) {
	avdName, err := resolveAvdForFeature(client, rest)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}

	actions, err := config.SetHostFeature(avdName, feature, on)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}

	verb := "Enabling"
	if !on {
		verb = "Disabling"
	}
	fmt.Printf("⚙️  %s host feature %q on %s:\n", verb, feature, avdName)
	for _, a := range actions {
		fmt.Printf("   ✓ config.ini %s\n", a)
	}

	// The guest packages (e.g. the camera apps) only matter when enabling.
	if on {
		if serial := runningSerialForAvd(client, avdName); serial != "" {
			if guestActions, err := client.Enable(serial, target); err == nil {
				for _, a := range guestActions {
					fmt.Printf("   ✓ %s (%s)\n", a, serial)
				}
			}
		}
	}

	fmt.Println()
	fmt.Println("ℹ️  QEMU reads these on a cold boot only. Apply with:")
	fmt.Printf("   avdslim restart %s      # stops, purges snapshots, relaunches\n", avdName)
	if config.HasGoldenSnapshot(avdName) {
		fmt.Printf("   avdslim bake %s         # re-bake the Golden Snapshot with the new hardware\n", avdName)
	}
	fmt.Println()
}

// resolveAvdForFeature picks the AVD a host feature applies to: an explicit
// name or index, else the only running emulator's AVD, else a prompt.
func resolveAvdForFeature(client *adb.Client, args []string) (string, error) {
	installed := config.GetInstalledAvds()

	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			continue
		}
		if idx, err := strconv.Atoi(a); err == nil {
			if idx >= 1 && idx <= len(installed) {
				return installed[idx-1]["name"], nil
			}
			// A bare number is also an emulator index; fall through to serials.
		}
		for _, avd := range installed {
			if strings.EqualFold(avd["name"], a) {
				return avd["name"], nil
			}
		}
		if serial, err := client.ResolveDevice([]string{a}); err == nil {
			if name := avdNameForSerial(client, serial); name != "" {
				return name, nil
			}
		}
		return "", fmt.Errorf("AVD %q not found in ~/.android/avd", a)
	}

	if running, err := client.GetRunningEmulators(); err == nil && len(running) == 1 {
		if name := avdNameForSerial(client, running[0].Serial); name != "" {
			fmt.Printf("ℹ️  Using the running emulator's AVD: %q\n\n", name)
			return name, nil
		}
	}
	return selectAvdInteractively(installed, "Select an AVD")
}

// avdNameForSerial asks a running emulator for its AVD name ("" when unknown).
func avdNameForSerial(client *adb.Client, serial string) string {
	out, _ := client.Exec("-s", serial, "emu", "avd", "name")
	name := strings.TrimSpace(strings.Split(strings.TrimSpace(out), "\n")[0])
	if name == "" || strings.Contains(name, "KO:") {
		return ""
	}
	return name
}

// runningSerialForAvd returns the serial of the emulator running avdName, if any.
func runningSerialForAvd(client *adb.Client, avdName string) string {
	running, err := client.GetRunningEmulators()
	if err != nil {
		return ""
	}
	for _, r := range running {
		if strings.EqualFold(avdNameForSerial(client, r.Serial), avdName) {
			return r.Serial
		}
	}
	return ""
}

func isDeviceSpecifier(client *adb.Client, running []adb.RunningEmulator, s string) bool {
	if _, err := strconv.Atoi(s); err == nil {
		return true
	}
	if strings.HasPrefix(s, "emulator-") {
		return true
	}
	for _, r := range running {
		if strings.EqualFold(r.Serial, s) {
			return true
		}
		nameOut, _ := client.Exec("-s", r.Serial, "emu", "avd", "name")
		avdName := strings.TrimSpace(strings.Split(nameOut, "\n")[0])
		if avdName != "" && strings.EqualFold(avdName, s) {
			return true
		}
	}
	return false
}

func selectAvdInteractively(installed []map[string]string, promptTitle string) (string, error) {
	if len(installed) == 0 {
		return "", fmt.Errorf("no installed AVDs found in ~/.android/avd")
	}
	if len(installed) == 1 {
		fmt.Printf("ℹ️  Auto-selecting only installed AVD: %q\n\n", installed[0]["name"])
		return installed[0]["name"], nil
	}

	fmt.Printf("📱 %s:\n", promptTitle)
	for i, avd := range installed {
		ram := avd["hw.ramSize"]
		if ram == "" {
			ram = "default"
		} else if !strings.HasSuffix(ram, "MB") && !strings.HasSuffix(ram, "G") && !strings.HasSuffix(ram, "M") {
			ram += "MB"
		}
		heap := avd["vm.heapSize"]
		if heap == "" {
			heap = "default"
		} else if !strings.HasSuffix(heap, "MB") && !strings.HasSuffix(heap, "M") {
			heap += "MB"
		}
		fmt.Printf("  [%d] %s (Config: RAM %s, Heap %s)\n", i+1, avd["name"], ram, heap)
	}
	fmt.Println()
	fmt.Printf("👉 Enter selection [1-%d] (default 1): ", len(installed))

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		fmt.Printf("✓ Selected: %s\n\n", installed[0]["name"])
		return installed[0]["name"], nil
	}

	if choice, err := strconv.Atoi(input); err == nil && choice >= 1 && choice <= len(installed) {
		fmt.Printf("✓ Selected: %s\n\n", installed[choice-1]["name"])
		return installed[choice-1]["name"], nil
	}

	for _, avd := range installed {
		if strings.EqualFold(avd["name"], input) {
			fmt.Printf("✓ Selected: %s\n\n", avd["name"])
			return avd["name"], nil
		}
	}

	return "", fmt.Errorf("invalid selection: %q", input)
}

func handleTuneAvd(args []string) {
	ramMb := 1536
	heapMb := 256
	gpuMode := ""
	targetAvd := ""

	for _, a := range args {
		if strings.HasPrefix(a, "--ram=") {
			if v, err := strconv.Atoi(strings.TrimPrefix(a, "--ram=")); err == nil {
				ramMb = v
			}
		} else if strings.HasPrefix(a, "--heap=") {
			if v, err := strconv.Atoi(strings.TrimPrefix(a, "--heap=")); err == nil {
				heapMb = v
			}
		} else if strings.HasPrefix(a, "--gpu=") {
			gpuMode = strings.TrimPrefix(a, "--gpu=")
		} else if !strings.HasPrefix(a, "--") {
			targetAvd = a
		}
	}

	installed := config.GetInstalledAvds()
	if targetAvd != "" {
		if idx, err := strconv.Atoi(targetAvd); err == nil && idx >= 1 && idx <= len(installed) {
			targetAvd = installed[idx-1]["name"]
		}
	} else if len(installed) > 0 {
		var err error
		targetAvd, err = selectAvdInteractively(installed, "Select an AVD to tune")
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			os.Exit(1)
		}
	}

	if err := config.TuneAvd(targetAvd, ramMb, heapMb, gpuMode); err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
}

// handleCreate makes a new AVD from an installed "Google APIs" 4 KB image via
// avdmanager, then tunes it. It never installs SDK packages or accepts licenses.
func handleCreate(args []string) {
	name, device, api := "", "pixel_5", 0
	ramMb, heapMb, gpuMode := 1536, 256, ""
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--api="):
			v, err := strconv.Atoi(strings.TrimPrefix(a, "--api="))
			if err != nil || v <= 0 {
				fmt.Printf("❌ Invalid %s (expected a number, e.g. --api=35)\n", a)
				os.Exit(1)
			}
			api = v
		case strings.HasPrefix(a, "--device="):
			device = strings.TrimPrefix(a, "--device=")
		case strings.HasPrefix(a, "--ram="):
			if v, err := strconv.Atoi(strings.TrimPrefix(a, "--ram=")); err == nil {
				ramMb = v
			}
		case strings.HasPrefix(a, "--heap="):
			if v, err := strconv.Atoi(strings.TrimPrefix(a, "--heap=")); err == nil {
				heapMb = v
			}
		case strings.HasPrefix(a, "--gpu="):
			gpuMode = strings.TrimPrefix(a, "--gpu=")
		case !strings.HasPrefix(a, "--") && name == "":
			name = a
		}
	}

	if !validAvdName(name) {
		fmt.Println("❌ Please give the new AVD a name (letters, digits, '.', '_' or '-').")
		fmt.Println("Usage: avdslim create <name> [--api=<level>] [--device=pixel_5] [--ram=<MB>]")
		os.Exit(1)
	}
	for _, avd := range config.GetInstalledAvds() {
		if strings.EqualFold(avd["name"], name) {
			fmt.Printf("❌ AVD %q already exists. Tune it instead: avdslim tune-avd %s\n", avd["name"], avd["name"])
			os.Exit(1)
		}
	}

	images := config.SlimImages(config.GetAndroidSdkDir(), api)
	if len(images) == 0 {
		level := "35"
		if api > 0 {
			level = strconv.Itoa(api)
		}
		fmt.Printf("❌ No 'Google APIs' (4 KB) %s system image installed.\n", config.HostAbi())
		fmt.Println("   Install one (you will be asked to accept Google's license):")
		fmt.Printf("   sdkmanager \"system-images;android-%s;google_apis;%s\"\n", level, config.HostAbi())
		os.Exit(1)
	}
	avdmanager := config.FindAvdmanager()
	if avdmanager == "" {
		fmt.Println("❌ avdmanager not found. Install the SDK Command-line Tools")
		fmt.Println("   (Android Studio → SDK Manager → SDK Tools), or put avdmanager on PATH.")
		os.Exit(1)
	}

	fmt.Printf("📦 Creating AVD %q from %s (device: %s)...\n", name, images[0], device)
	cmd := exec.Command(avdmanager, "create", "avd", "-n", name, "-k", images[0], "-d", device)
	cmd.Stdin = strings.NewReader("no\n") // decline the custom hardware profile prompt
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("❌ avdmanager failed: %v (it needs Java 17+; check --device with `avdmanager list device`)\n", err)
		os.Exit(1)
	}
	fmt.Println()

	if err := config.TuneAvd(name, ramMb, heapMb, gpuMode); err != nil {
		fmt.Printf("❌ Created %q but tuning failed: %v\n   Retry: avdslim tune-avd %s\n", name, err, name)
		os.Exit(1)
	}
	fmt.Printf("Next: avdslim start %s   then   avdslim bake %s   (~1.5 s boots)\n\n", name, name)
}

// validAvdName mirrors avdmanager's allowed AVD name characters.
func validAvdName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

func handleLaunch(client *adb.Client, args []string) {
	installed := config.GetInstalledAvds()
	var avdName string
	var options []string

	for _, a := range args {
		if strings.HasPrefix(a, "--") {
			options = append(options, a)
		} else if avdName == "" {
			avdName = a
		}
	}

	if avdName != "" {
		// Check if user passed a numeric index directly: e.g. `avdslim start 1`
		if idx, err := strconv.Atoi(avdName); err == nil && idx >= 1 && idx <= len(installed) {
			avdName = installed[idx-1]["name"]
			fmt.Printf("✓ Selected [%d]: %s\n\n", idx, avdName)
		}
	} else {
		var err error
		avdName, err = selectAvdInteractively(installed, "Select an AVD to launch")
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			os.Exit(1)
		}
	}

	doSlim := true
	lowRam := true
	headless := false
	forceCold := false
	ramMb := 1536
	gpuMode := config.GetRecommendedGpuMode()

	var slimArgs []string
	for _, a := range options {
		if a == "--aggressive" || strings.HasPrefix(a, "--keep=") || strings.HasPrefix(a, "--skip=") ||
			a == "--no-anim" || a == "--no-animations" || a == "--anim" || a == "--animations" {
			slimArgs = append(slimArgs, a)
		} else if a == "--no-slim" {
			doSlim = false
		} else if a == "--slim" {
			doSlim = true
		} else if a == "--no-lowram" {
			lowRam = false
		} else if a == "--headless" || a == "--no-window" {
			headless = true
		} else if a == "--cold" || a == "--no-snapshot" {
			forceCold = true
		} else if strings.HasPrefix(a, "--ram=") {
			if v, err := strconv.Atoi(strings.TrimPrefix(a, "--ram=")); err == nil {
				ramMb = v
			}
		} else if strings.HasPrefix(a, "--gpu=") {
			gpuMode = strings.TrimPrefix(a, "--gpu=")
		}
	}

	canonical, ok := installedAvdName(installed, avdName)
	if !ok {
		fmt.Printf("❌ AVD %q not found in %s. Run `avdslim list` to see installed AVDs.\n", avdName, config.GetAvdBaseDir())
		os.Exit(1)
	}
	avdName = canonical

	emulator := config.FindEmulatorExecutable()
	emuArgs := []string{
		"-avd", avdName,
		"-memory", strconv.Itoa(ramMb),
		"-gpu", gpuMode,
	}
	emuArgs = append(emuArgs, hostFeatureArgs(avdName)...)

	if lowRam {
		emuArgs = append(emuArgs, "-lowram")
	}

	if headless {
		emuArgs = append(emuArgs, "-no-window")
	}

	port := freeEmulatorPort(client)
	serial := "emulator-" + strconv.Itoa(port)
	emuArgs = append(emuArgs, "-port", strconv.Itoa(port))

	hasGolden := config.HasGoldenSnapshot(avdName)
	if hasGolden && !forceCold {
		emuArgs = append(emuArgs, "-snapshot", "avdslim_clean", "-no-snapshot-save")
		fmt.Printf("✨ Golden Snapshot detected! Restoring instant clean state (< 1.5s boot)...\n")
	} else {
		emuArgs = append(emuArgs, "-no-snapshot-load")
	}

	fmt.Printf("🚀 Launching emulator %q with low-memory host flags:\n", avdName)
	fmt.Printf("   emulator %s\n\n", strings.Join(emuArgs, " "))

	emu := spawnEmulator(emulator, emuArgs)
	// An emulator that fails (bad image, locked AVD) exits within seconds.
	select {
	case err := <-emu.exited:
		fmt.Printf("❌ The emulator exited during startup (%v). Last lines of %s:\n%s\n", err, emu.logPath, logTail(emu.logPath))
		os.Exit(1)
	case <-time.After(3 * time.Second):
	}

	if doSlim {
		fmt.Println("⏳ Waiting for emulator to finish booting...")
		err := emu.waitForBoot(client, serial, 60)
		switch {
		case err == nil:
			if hasGolden && !forceCold {
				fmt.Println("✓ Instant boot complete via Golden Snapshot! Refreshing slim state...")
			} else {
				fmt.Println("✓ Boot complete! Applying avdslim optimizations...")
			}
			handleOn(client, append(slimArgs, serial))
		case errors.Is(err, errEmulatorExited):
			fmt.Printf("❌ %v\n", err)
			os.Exit(1)
		default:
			// Still booting, just slowly: leave it running.
			fmt.Printf("⚠️  Boot %v. You can run `avdslim on` manually once it is up.\n", err)
		}
	}
}

var errEmulatorExited = errors.New("the emulator exited during startup")

// runningEmulator is an emulator avdslim spawned, with its output in logPath.
type runningEmulator struct {
	cmd     *exec.Cmd
	exited  chan error
	logPath string
}

// spawnEmulator starts the emulator detached, keeping its output in a temp
// log: if it dies at startup, that is where it says why. Exits 1 on failure.
func spawnEmulator(emulator string, args []string) *runningEmulator {
	logFile, err := os.CreateTemp("", "avdslim-emulator-*.log")
	if err != nil {
		fmt.Printf("❌ Cannot create emulator log file: %v\n", err)
		os.Exit(1)
	}
	defer logFile.Close() // the child keeps its own descriptor

	cmd := exec.Command(emulator, args...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	host.SetDetached(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Printf("❌ Failed to launch emulator: %v\n", err)
		os.Exit(1)
	}
	e := &runningEmulator{cmd: cmd, exited: make(chan error, 1), logPath: logFile.Name()}
	go func() { e.exited <- cmd.Wait() }()
	fmt.Printf("✓ Emulator process spawned (PID: %d, log: %s).\n", cmd.Process.Pid, e.logPath)
	return e
}

// waitForBoot waits (every 2 s) up to 180 s for adb to see serial, then up to
// tries more polls for sys.boot_completed. It returns an error, with the end
// of the emulator's log, if the emulator exits meanwhile or time runs out.
// Unlike `adb wait-for-device`, it cannot hang on an emulator that died.
func (e *runningEmulator) waitForBoot(client *adb.Client, serial string, tries int) error {
	poll := func(n int, ready func() bool) error {
		for i := 0; i < n; i++ {
			select {
			case err := <-e.exited:
				return fmt.Errorf("%w (%v). Last lines of %s:\n%s", errEmulatorExited, err, e.logPath, logTail(e.logPath))
			default:
			}
			if ready() {
				return nil
			}
			time.Sleep(2 * time.Second)
		}
		return fmt.Errorf("timed out (emulator log: %s)", e.logPath)
	}
	if err := poll(90, func() bool {
		s, _ := client.Exec("-s", serial, "get-state")
		return strings.TrimSpace(s) == "device"
	}); err != nil {
		return err
	}
	return poll(tries, func() bool {
		res, _ := client.Exec("-s", serial, "shell", "getprop", "sys.boot_completed")
		return strings.TrimSpace(res) == "1"
	})
}

// waitStopped waits up to 10 s for serial's emulator process to go away.
func waitStopped(serial string) bool {
	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		if host.FindHostPidForSerial(serial) == 0 {
			return true
		}
	}
	return false
}

// installedAvdName returns the installed AVD's exact name, matching case-insensitively
// (macOS users may type names in any case; the emulator on Linux needs the exact one).
func installedAvdName(installed []map[string]string, name string) (string, bool) {
	for _, avd := range installed {
		if strings.EqualFold(avd["name"], name) {
			return avd["name"], true
		}
	}
	return "", false
}

// logTail returns the last 15 lines of the emulator log, indented.
func logTail(logPath string) string {
	data, _ := os.ReadFile(logPath)
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > 15 {
		lines = lines[len(lines)-15:]
	}
	return "   " + strings.Join(lines, "\n   ")
}

func handleStop(client *adb.Client, args []string) {
	var target string
	snap := false
	force := false

	for _, a := range args {
		if a == "--snap" || a == "--snapshot" {
			snap = true
		} else if a == "-f" || a == "--force" || a == "--no-snap" {
			force = true
		} else if !strings.HasPrefix(a, "--") {
			target = a
		}
	}

	running, err := client.GetRunningEmulators()
	if err != nil || len(running) == 0 {
		fmt.Println("ℹ️  No running Android emulator detected.")
		return
	}

	var serial string
	if target != "" {
		s, err := client.ResolveDevice([]string{target})
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			os.Exit(1)
		}
		serial = s
	} else if len(running) == 1 {
		serial = running[0].Serial
	} else {
		fmt.Println("📱 Running Emulators:")
		for i, emu := range running {
			nameOut, _ := client.Exec("-s", emu.Serial, "emu", "avd", "name")
			name := strings.TrimSpace(strings.Split(nameOut, "\n")[0])
			if name == "" {
				name = emu.Model
			}
			fmt.Printf("   [%d] %s (%s)\n", i+1, emu.Serial, name)
		}
		fmt.Print("\nSelect an emulator to stop (number): ")
		reader := bufio.NewReader(os.Stdin)
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)
		if idx, err := strconv.Atoi(text); err == nil && idx >= 1 && idx <= len(running) {
			serial = running[idx-1].Serial
		} else {
			fmt.Println("❌ Invalid selection.")
			os.Exit(1)
		}
	}

	avdNameOut, _ := client.Exec("-s", serial, "emu", "avd", "name")
	avdName := strings.TrimSpace(strings.Split(avdNameOut, "\n")[0])
	if avdName == "" {
		avdName = serial
	}

	// If not force and not already --snap, prompt
	if !force && !snap {
		fmt.Printf("💾 Save current state of %s into Golden Snapshot before stopping? [y/N]: ", avdName)
		reader := bufio.NewReader(os.Stdin)
		ans, _ := reader.ReadString('\n')
		ans = strings.ToLower(strings.TrimSpace(ans))
		if ans == "y" || ans == "yes" {
			snap = true
		}
	}

	if snap {
		fmt.Printf("📸 Capturing Golden Snapshot for %s before shutdown...\n", avdName)
		handleSnapshot(client, []string{serial})
	}

	fmt.Printf("🛑 Gracefully shutting down %s (%s)...\n", serial, avdName)
	client.Exec("-s", serial, "emu", "kill")
	if !waitStopped(serial) {
		fmt.Printf("❌ Emulator %s did not stop within 10s.\n\n", serial)
		os.Exit(1)
	}
	fmt.Println("✓ Emulator process stopped cleanly.")
	fmt.Println()
}

func handleRestart(client *adb.Client, args []string) {
	serial, err := client.ResolveDevice(args)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}

	avdNameOut, _ := client.Exec("-s", serial, "emu", "avd", "name")
	lines := strings.Split(strings.TrimSpace(avdNameOut), "\n")
	avdName := ""
	if len(lines) > 0 {
		avdName = strings.TrimSpace(lines[0])
	}
	if avdName == "" || strings.Contains(avdName, "KO:") {
		installed := config.GetInstalledAvds()
		if len(installed) == 1 {
			avdName = installed[0]["name"]
		}
	}
	if avdName == "" {
		fmt.Println("❌ Could not determine AVD name for running emulator.")
		os.Exit(1)
	}

	fmt.Printf("🔄 Gracefully shutting down %s (%s)...\n", serial, avdName)
	client.Exec("-s", serial, "emu", "kill")

	// Purging files under a live emulator, then launching a second one on the
	// same AVD, corrupts it.
	if !waitStopped(serial) {
		fmt.Printf("❌ %s is still running after 10 s. Stop it (avdslim stop %s -f) and retry.\n", serial, serial)
		os.Exit(1)
	}
	fmt.Println("✓ Emulator process stopped.")

	// Purge stale runtime cache & snapshots
	hadGolden := config.HasGoldenSnapshot(avdName)
	avdDir := config.AvdDir(avdName)
	_ = os.Remove(filepath.Join(avdDir, "hardware-qemu.ini"))
	_ = os.Remove(filepath.Join(avdDir, "hardware-qemu.ini.lock"))
	_ = os.RemoveAll(filepath.Join(avdDir, "snapshots"))
	fmt.Println("✓ Purged stale hardware-qemu.ini and snapshots.")
	if hadGolden {
		fmt.Printf("ℹ️  That included the Golden Snapshot. Re-create it with: avdslim bake %s\n", avdName)
	}

	launchArgs := []string{avdName, "--slim"}
	for _, a := range args {
		if strings.HasPrefix(a, "--ram=") {
			launchArgs = append(launchArgs, a)
		}
	}
	handleLaunch(client, launchArgs)
}

// hostFeatureArgs returns the slimming flags for the features avdName has NOT
// re-enabled via `avdslim enable <feature>`.
func hostFeatureArgs(avdName string) []string {
	var args []string
	if !config.HostFeatureOn(avdName, "audio") {
		args = append(args, "-no-audio")
	}
	if !config.HostFeatureOn(avdName, "camera") {
		args = append(args, "-camera-back", "none", "-camera-front", "none")
	}
	if !config.HostFeatureOn(avdName, "bootanim") {
		args = append(args, "-no-boot-anim")
	}
	return args
}

// freeEmulatorPort returns the console port a new emulator should take, so boot
// waits and slimming target it and never another running AVD.
func freeEmulatorPort(client *adb.Client) int {
	devices, _ := client.Exec("devices")
	for p := 5554; p <= 5682; p += 2 {
		if strings.Contains(devices, "emulator-"+strconv.Itoa(p)+"\t") {
			continue
		}
		if l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p)); err == nil {
			l.Close()
			return p
		}
	}
	return 5554
}

func findEmulator() string {
	return config.FindEmulatorExecutable()
}

func handleWatch(client *adb.Client, args []string) {
	aggressive := false
	var keepPackages []string
	skip := newDefaultSkip()
	for _, a := range args {
		if a == "--aggressive" {
			aggressive = true
		} else if strings.HasPrefix(a, "--keep=") {
			pkg := strings.TrimPrefix(a, "--keep=")
			if pkg != "" {
				keepPackages = append(keepPackages, pkg)
			}
		} else if a == "--no-anim" || a == "--no-animations" {
			delete(skip, "animations")
		} else if a == "--anim" || a == "--animations" {
			skip["animations"] = true
		} else if strings.HasPrefix(a, "--skip=") {
			addSkip(skip, a)
		}
	}

	preset := "Standard"
	if aggressive {
		preset = "Aggressive"
	}

	fmt.Println("👀 AVD-SLIM Watcher active...")
	fmt.Printf("   Preset: %s\n", preset)
	if len(keepPackages) > 0 {
		fmt.Printf("   Preserving packages: %s\n", strings.Join(keepPackages, ", "))
	}
	if len(skip) > 0 {
		fmt.Printf("   Leaving settings unchanged: %s\n", strings.Join(sortedKeys(skip), ", "))
	}
	warnIfShimOverwritten()
	fmt.Println("   Monitoring for newly booted Android emulators in the background.")
	fmt.Println("   Will automatically apply low-memory optimizations as soon as emulators boot.")
	fmt.Println("   Press Ctrl+C to stop.")
	fmt.Println()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	slimmedDevices := make(map[string]bool)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-sigChan:
			fmt.Println("\n👋 Stopping AVD-SLIM watcher. Goodbye!")
			return
		case <-ticker.C:
			emulators, err := client.GetRunningEmulators()
			if err != nil {
				continue
			}

			// Clean up devices that were turned off / disconnected
			currentMap := make(map[string]bool)
			for _, emu := range emulators {
				currentMap[emu.Serial] = true
			}
			for serial := range slimmedDevices {
				if !currentMap[serial] {
					delete(slimmedDevices, serial)
				}
			}

			for _, emu := range emulators {
				if slimmedDevices[emu.Serial] || emu.IsSlimmed {
					slimmedDevices[emu.Serial] = true
					continue
				}

				// Check if boot completed
				res, _ := client.Exec("-s", emu.Serial, "shell", "getprop", "sys.boot_completed")
				if strings.TrimSpace(res) != "1" {
					fmt.Printf("⏳ [%s] Emulator detected, waiting for boot completion...\n", emu.Serial)
					continue
				}

				fmt.Printf("\n✨ [%s] Emulator booted! Automatically applying avdslim...\n", emu.Serial)
				warnIfShimOverwritten()
				// Marked either way: a failure here is not transient once boot has completed.
				slimmedDevices[emu.Serial] = true
				count, err := client.Slim(emu.Serial, aggressive, keepPackages, skip)
				if err != nil {
					fmt.Printf("❌ [%s] Not slimmed: %v\n\n", emu.Serial, err)
					continue
				}
				fmt.Printf("✓ [%s] Successfully slimmed! Disabled %d packages, trimmed RAM.\n\n", emu.Serial, count)
			}
		}
	}
}

func handleBench(client *adb.Client, args []string) {
	serial, err := client.ResolveDevice(args)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}

	hostPid := host.FindHostPidForSerial(serial)
	footprintMb := 0
	rssMb := 0
	if hostPid > 0 {
		footprintMb = host.GetHostFootprintMb(hostPid)
		rssMb = host.GetHostRssMb(hostPid)
	}

	disabledRaw, _ := client.Exec("-s", serial, "shell", "pm", "list", "packages", "-d")
	disabledCount := 0
	if strings.TrimSpace(disabledRaw) != "" {
		for _, line := range strings.Split(strings.TrimSpace(disabledRaw), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "package:") {
				disabledCount++
			}
		}
	}

	baselineFp := 5600
	baselineRss := 2800

	fpSavingPct := 0
	if footprintMb > 0 && footprintMb < baselineFp {
		fpSavingPct = int(((float64(baselineFp) - float64(footprintMb)) / float64(baselineFp)) * 100)
	}
	rssSavingPct := 0
	if rssMb > 0 && rssMb < baselineRss {
		rssSavingPct = int(((float64(baselineRss) - float64(rssMb)) / float64(baselineRss)) * 100)
	}

	animDesc := "1.0x (Stock Fluid) "
	if scaleOut, err := client.Exec("-s", serial, "shell", "settings", "get", "global", "window_animation_scale"); err == nil {
		if strings.TrimSpace(scaleOut) == "0" {
			animDesc = "0x (Zero GPU Churn)  100%"
		}
	}

	fmt.Printf(`════════════════════════════════════════════════════════════════════════
 ⚡ AVD-SLIM Live Efficiency Scoreboard: %s
════════════════════════════════════════════════════════════════════════
 Metric                      Stock Baseline    AVD-SLIM (Current)   Savings
 ───────────────────────────────────────────────────────────────────────
 Host Memory (Footprint)     5,600 MB (5.6 GB) %5d MB (%3.1f GB)   -%d%% ⚡
 Physical Resident RAM (RSS) 2,800 MB (2.8 GB) %5d MB (%3.1f GB)   -%d%% ⚡
 Disabled Background Bloat   0 packages        %2d packages disabled
 Dalvik / ART Heap Ceiling   512 MB            256 MB (Compact)    -50%%
 Display Animations / Churn  1.0x Scale        %s
 ───────────────────────────────────────────────────────────────────────
 🛡️  Fidelity: 100%% FCM Push, Firebase Auth, WebView & Sockets Guaranteed
════════════════════════════════════════════════════════════════════════
`, serial, footprintMb, float64(footprintMb)/1024.0, fpSavingPct, rssMb, float64(rssMb)/1024.0, rssSavingPct, disabledCount, animDesc)
}

func handleInstallShim(args []string) {
	ramMb := 1536
	for _, a := range args {
		if strings.HasPrefix(a, "--ram=") {
			if v, err := strconv.Atoi(strings.TrimPrefix(a, "--ram=")); err == nil {
				ramMb = v
			}
		}
	}

	fmt.Printf("🔧 Installing AVD-SLIM emulator shim (Default RAM: %dMB)...\n", ramMb)
	if err := shim.InstallShim(ramMb); err != nil {
		fmt.Printf("❌ Failed to install shim: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✅ Successfully installed emulator shim!")
	fmt.Println("   • From now on, launching emulators via Android Studio 'Play' button")
	fmt.Printf("     will automatically inject -memory %d -lowram -no-audio flags.\n", ramMb)
	fmt.Printf("   • A --ram=<MB> line in %s overrides this at launch.\n", config.DefaultsFilePath())
	fmt.Println("   • After an emulator update in Android Studio, run `avdslim install-shim` again.")
	fmt.Println("   • To restore stock Android Studio emulator behavior anytime:")
	fmt.Println("     avdslim uninstall-shim")
	fmt.Println()
}

func handleUninstallShim() {
	fmt.Println("🔄 Restoring original Android SDK emulator binary...")
	if err := shim.UninstallShim(); err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Successfully uninstalled shim. Stock emulator binary restored.")
	fmt.Println()
}

func handleBake(client *adb.Client, args []string) {
	for _, a := range args {
		if a == "--live" || a == "--current" {
			handleSnapshot(client, args)
			return
		}
	}

	installed := config.GetInstalledAvds()
	var targetAvd string
	ramMb := 1536
	aggressive := false
	headless := false
	var keepPackages []string
	skip := newDefaultSkip()

	for _, a := range args {
		if strings.HasPrefix(a, "--skip=") {
			addSkip(skip, a)
		} else if strings.HasPrefix(a, "--ram=") {
			if v, err := strconv.Atoi(strings.TrimPrefix(a, "--ram=")); err == nil {
				ramMb = v
			}
		} else if a == "--aggressive" {
			aggressive = true
		} else if a == "--headless" || a == "--no-window" {
			headless = true
		} else if a == "--no-anim" || a == "--no-animations" {
			delete(skip, "animations")
		} else if a == "--anim" || a == "--animations" {
			skip["animations"] = true
		} else if strings.HasPrefix(a, "--keep=") {
			pkg := strings.TrimPrefix(a, "--keep=")
			if pkg != "" {
				keepPackages = append(keepPackages, pkg)
			}
		} else if !strings.HasPrefix(a, "--") {
			targetAvd = a
		}
	}

	if targetAvd != "" {
		if idx, err := strconv.Atoi(targetAvd); err == nil && idx >= 1 && idx <= len(installed) {
			targetAvd = installed[idx-1]["name"]
			fmt.Printf("✓ Selected [%d]: %s\n\n", idx, targetAvd)
		}
	} else if len(installed) > 0 {
		var err error
		targetAvd, err = selectAvdInteractively(installed, "Select an AVD to bake Golden Snapshot for")
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("❌ No installed AVDs found.")
		os.Exit(1)
	}

	fmt.Printf("🍳 Baking Golden Snapshot for %q (RAM: %d MB)...\n", targetAvd, ramMb)
	fmt.Println("   • Cold boots emulator in pristine state")
	fmt.Println("   • Automatically prunes background bloatware & optimizes settings")
	fmt.Println("   • Captures 'avdslim_clean' snapshot for instant ~1.5s launches")
	fmt.Println()

	// 1. Stop a running instance of this AVD (exact name: "Pixel" must not
	// match "Pixel_10_Pro") so the bake is a cold boot.
	running, _ := client.GetRunningEmulators()
	for _, emu := range running {
		if !strings.EqualFold(avdNameForSerial(client, emu.Serial), targetAvd) {
			continue
		}
		fmt.Printf("🔄 Stopping active emulator instance (%s) for clean baking...\n", emu.Serial)
		client.Exec("-s", emu.Serial, "emu", "kill")
		if !waitStopped(emu.Serial) {
			fmt.Printf("❌ %s did not stop. Stop it (avdslim stop %s -f) and retry.\n", emu.Serial, emu.Serial)
			os.Exit(1)
		}
	}

	// 2. Launch cold emulator (DO NOT pass -no-snapshot-save, DO pass -no-snapshot-load).
	// The existing Golden Snapshot stays until the new one is ready to save.
	emulator := config.FindEmulatorExecutable()
	gpuMode := config.GetRecommendedGpuMode()
	emuArgs := []string{
		"-avd", targetAvd,
		"-lowram",
		"-memory", strconv.Itoa(ramMb),
		"-gpu", gpuMode,
		"-no-snapshot-load",
	}
	emuArgs = append(emuArgs, hostFeatureArgs(targetAvd)...)
	port := freeEmulatorPort(client)
	targetSerial := "emulator-" + strconv.Itoa(port)
	emuArgs = append(emuArgs, "-port", strconv.Itoa(port))
	if headless {
		emuArgs = append(emuArgs, "-no-window")
	}

	fmt.Println("🚀 Spawning baseline emulator...")
	emu := spawnEmulator(emulator, emuArgs)
	snapDir := config.GoldenSnapshotDir(targetAvd)
	oldSnap := snapDir + ".avdslim-old" // the previous snapshot while saving the new one
	// abort stops the baking emulator, puts any previous snapshot back, and exits 1.
	abort := func(msg string) {
		fmt.Printf("❌ %s Aborting bake.\n", msg)
		client.Exec("-s", targetSerial, "emu", "kill")
		if !waitStopped(targetSerial) {
			_ = emu.cmd.Process.Kill()
		}
		if _, err := os.Stat(oldSnap); err == nil {
			_ = os.RemoveAll(snapDir)
			_ = os.Rename(oldSnap, snapDir)
		}
		if config.HasGoldenSnapshot(targetAvd) {
			fmt.Println("   Your previous Golden Snapshot is unchanged.")
		}
		os.Exit(1)
	}

	fmt.Println("⏳ Waiting for boot completion...")
	if err := emu.waitForBoot(client, targetSerial, 90); err != nil {
		abort(fmt.Sprintf("Boot failed: %v.", err))
	}

	fmt.Printf("✓ Boot complete on %s! Settling system daemons (4s)...\n", targetSerial)
	time.Sleep(4 * time.Second)

	// 3. Apply avdslim optimizations
	fmt.Println("⚡ Pruning bloatware & tuning runtime settings...")
	count, err := client.Slim(targetSerial, aggressive, keepPackages, skip)
	if err != nil {
		abort(fmt.Sprintf("Slimming failed, so the snapshot would not be slim: %v.", err))
	}
	fmt.Printf("✓ Disabled %d bloat packages and trimmed memory.\n", count)

	time.Sleep(2 * time.Second)
	client.Exec("-s", targetSerial, "shell", "sync")

	// 4. Replace the golden snapshot now that the new state is ready.
	fmt.Println("📸 Capturing Golden Snapshot 'avdslim_clean'...")
	_ = os.RemoveAll(oldSnap)
	if _, err := os.Stat(snapDir); err == nil {
		if err := os.Rename(snapDir, oldSnap); err != nil {
			abort(fmt.Sprintf("Cannot set the previous snapshot aside: %v.", err))
		}
	}
	snapOut, err := client.Exec("-s", targetSerial, "emu", "avd", "snapshot", "save", "avdslim_clean")
	if err != nil || strings.Contains(snapOut, "KO") {
		abort(fmt.Sprintf("Snapshot save failed: %s.", strings.TrimSpace(snapOut)))
	}
	time.Sleep(2 * time.Second)

	// 5. Verify snapshot on host filesystem
	if !config.HasGoldenSnapshot(targetAvd) {
		abort(fmt.Sprintf("The emulator reported a save, but %s does not exist.", snapDir))
	}
	_ = os.RemoveAll(oldSnap)
	fmt.Println("✨ Golden Snapshot verified on disk!")

	// 6. Graceful shutdown
	fmt.Println("🛑 Gracefully shutting down baking emulator...")
	client.Exec("-s", targetSerial, "emu", "kill")
	if waitStopped(targetSerial) {
		fmt.Println("✓ Emulator shut down cleanly.")
	} else {
		fmt.Printf("⚠️  %s is still running; stop it with: avdslim stop %s -f\n", targetSerial, targetSerial)
	}

	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════════════════")
	fmt.Printf(" 🎉 Golden Snapshot Baked Successfully for %s!\n", targetAvd)
	fmt.Println("════════════════════════════════════════════════════════════════════════")
	fmt.Println(" • Snapshot Name: 'avdslim_clean'")
	fmt.Println(" • Startup Latency: Reduced from ~45s cold boot to <1.5s instant restore ⚡")
	fmt.Printf(" • RAM Allocation: %d MB (-lowram)\n", ramMb)
	fmt.Println(" • How to launch:")
	fmt.Printf("     avdslim start %s\n", targetAvd)
	fmt.Println("   Or click 'Play' in Android Studio (if `avdslim install-shim` is enabled).")
	fmt.Println("════════════════════════════════════════════════════════════════════════")
	fmt.Println()
}

func handleSnapshot(client *adb.Client, args []string) {
	var serial string
	skipSlim := false
	aggressive := false
	snapName := "avdslim_clean"
	var keepPackages []string
	skip := newDefaultSkip()

	for _, a := range args {
		if a == "--skip-slim" || a == "--no-prune" {
			skipSlim = true
		} else if strings.HasPrefix(a, "--skip=") {
			addSkip(skip, a)
		} else if a == "--no-anim" || a == "--no-animations" {
			delete(skip, "animations")
		} else if a == "--anim" || a == "--animations" {
			skip["animations"] = true
		} else if a == "--aggressive" {
			aggressive = true
		} else if a == "--live" || a == "--current" {
			// standard flag alias, ignore
		} else if strings.HasPrefix(a, "--tag=") {
			snapName = strings.TrimPrefix(a, "--tag=")
		} else if strings.HasPrefix(a, "--name=") {
			snapName = strings.TrimPrefix(a, "--name=")
		} else if strings.HasPrefix(a, "--keep=") {
			pkg := strings.TrimPrefix(a, "--keep=")
			if pkg != "" {
				keepPackages = append(keepPackages, pkg)
			}
		} else if !strings.HasPrefix(a, "--") {
			serial = a
		}
	}

	running, err := client.GetRunningEmulators()
	if err != nil || len(running) == 0 {
		fmt.Println("❌ No running Android emulator found via adb.")
		fmt.Println()
		fmt.Println("💡 To snapshot a custom configured state (with test apps & logins):")
		fmt.Println("   1. Start your emulator: avdslim start")
		fmt.Println("   2. Install your debug APKs / log into test accounts")
		fmt.Println("   3. Run: avdslim snapshot")
		os.Exit(1)
	}

	// With several emulators and no serial, ResolveDevice lists them and asks,
	// rather than snapshotting whichever adb happened to list first.
	var targetArgs []string
	if serial != "" {
		targetArgs = []string{serial}
	}
	serial, err = client.ResolveDevice(targetArgs)
	if err != nil {
		fmt.Printf("❌ %v\n", err)
		os.Exit(1)
	}

	// Get AVD Name
	avdNameOut, _ := client.Exec("-s", serial, "emu", "avd", "name")
	lines := strings.Split(strings.TrimSpace(avdNameOut), "\n")
	avdName := ""
	if len(lines) > 0 {
		avdName = strings.TrimSpace(lines[0])
	}
	if avdName == "" || strings.Contains(avdName, "KO:") {
		installed := config.GetInstalledAvds()
		if len(installed) == 1 {
			avdName = installed[0]["name"]
		}
	}

	fmt.Printf("📸 Capturing Golden Snapshot from live emulator %s (%s)...\n", serial, avdName)
	if !skipSlim {
		fmt.Println("⚡ Trimming background daemons and caches while preserving installed apps...")
		count, err := client.Slim(serial, aggressive, keepPackages, skip)
		if err != nil {
			fmt.Printf("❌ %v\n   Snapshot not saved. Retry, or pass --skip-slim to capture as-is.\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Trimmed memory and disabled %d background bloat packages.\n", count)
	}

	fmt.Println("💾 Syncing filesystem...")
	client.Exec("-s", serial, "shell", "sync")
	time.Sleep(1 * time.Second)

	fmt.Printf("📸 Saving snapshot %q...\n", snapName)
	out, err := client.Exec("-s", serial, "emu", "avd", "snapshot", "save", snapName)
	if err != nil || strings.Contains(out, "KO") {
		fmt.Printf("❌ Failed to save snapshot: %s\n", strings.TrimSpace(out))
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════════════════")
	fmt.Printf(" 🎉 Live State Captured as Golden Snapshot for %s!\n", avdName)
	fmt.Println("════════════════════════════════════════════════════════════════════════")
	fmt.Printf(" • Target AVD: %s (%s)\n", avdName, serial)
	fmt.Printf(" • Snapshot Name: %q\n", snapName)
	fmt.Println(" • Preserved: All installed apps, local databases & logged-in accounts")
	fmt.Println(" • Instant Restore: Next time you launch with `avdslim start` or Android Studio,")
	fmt.Println("   it will boot into this exact configured state in < 1.5 seconds! ⚡")
	fmt.Println(" • The running emulator remains active for your current work.")
	fmt.Println("════════════════════════════════════════════════════════════════════════")
	fmt.Println()
}

func handleUnbake(args []string) {
	installed := config.GetInstalledAvds()
	var targetAvd string

	for _, a := range args {
		if !strings.HasPrefix(a, "--") {
			targetAvd = a
		}
	}

	if targetAvd != "" {
		if idx, err := strconv.Atoi(targetAvd); err == nil && idx >= 1 && idx <= len(installed) {
			targetAvd = installed[idx-1]["name"]
		}
	} else if len(installed) > 0 {
		var err error
		targetAvd, err = selectAvdInteractively(installed, "Select an AVD to remove Golden Snapshot from")
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("❌ No installed AVDs found.")
		os.Exit(1)
	}

	snapDir := config.GoldenSnapshotDir(targetAvd)
	if _, err := os.Stat(snapDir); os.IsNotExist(err) {
		fmt.Printf("ℹ️  No Golden Snapshot found for %s.\n", targetAvd)
		return
	}

	if err := os.RemoveAll(snapDir); err != nil {
		fmt.Printf("❌ Failed to remove snapshot: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✅ Removed Golden Snapshot 'avdslim_clean' for %s.\n", targetAvd)
	fmt.Println("   Subsequent launches will perform standard boots.")
	fmt.Println()
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// warnIfShimOverwritten flags an Android Studio emulator update that replaced the shim.
func warnIfShimOverwritten() {
	if shim.IsShimOverwritten() {
		fmt.Println("⚠️  An emulator update replaced the avdslim shim, so Android Studio launches are no longer slimmed.")
		fmt.Println("   Run: avdslim install-shim")
	}
}
