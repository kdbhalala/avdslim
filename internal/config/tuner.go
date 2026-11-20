package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// GetInstalledAvds lists the AVDs that have a config.ini, named as the emulator
// names them (see listAvds).
func GetInstalledAvds() []map[string]string {
	var list []map[string]string
	for _, a := range listAvds(GetAvdBaseDir()) {
		kv, err := readConfigIni(filepath.Join(a.dir, "config.ini"))
		if err != nil {
			continue
		}
		kv["name"] = a.name
		list = append(list, kv)
	}
	return list
}

func TuneAvd(targetAvd string, ramMb, heapMb int, gpuMode string) error {
	avdHome := GetAvdBaseDir()
	installed := GetInstalledAvds()

	if len(installed) == 0 {
		return fmt.Errorf("no AVD configurations found in %s", avdHome)
	}

	var avdName string
	if targetAvd != "" {
		name, _, ok := ResolveAvd(targetAvd)
		if !ok {
			return fmt.Errorf("AVD %q not found in %s", targetAvd, avdHome)
		}
		avdName = name
	} else if len(installed) == 1 {
		avdName = installed[0]["name"]
	} else {
		fmt.Println("Found multiple AVDs. Please specify one:")
		for _, a := range installed {
			fmt.Printf("  • %s\n", a["name"])
		}
		return fmt.Errorf("please specify AVD name")
	}

	fileToTune, err := AvdConfigPath(avdName)
	if err != nil {
		return err
	}
	fmt.Printf("⚙️  Tuning config.ini for %q (RAM: %dMB, Heap: %dMB)...\n", avdName, ramMb, heapMb)

	data, err := os.ReadFile(fileToTune)
	if err != nil {
		return err
	}

	// Abort before touching config.ini or snapshots if the backup cannot be
	// made: tuning is only reversible while the .bak exists.
	backupFile := fileToTune + ".bak"
	if _, err := os.Stat(backupFile); err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("cannot check backup %s: %w", backupFile, err)
		}
		if err := os.WriteFile(backupFile, data, 0644); err != nil {
			return fmt.Errorf("refusing to tune %q without a backup: %w", avdName, err)
		}
		fmt.Printf("   ✓ Created backup: %s\n", backupFile)
	}

	lines := strings.Split(string(data), "\n")
	kv := make(map[string]string)
	for _, l := range lines {
		idx := strings.Index(l, "=")
		if idx > 0 {
			k := strings.TrimSpace(l[:idx])
			v := strings.TrimSpace(l[idx+1:])
			kv[k] = v
		}
	}

	if gpuMode == "" {
		gpuMode = GetRecommendedGpuMode()
	}

	kv["hw.ramSize"] = strconv.Itoa(ramMb)
	kv["vm.heapSize"] = strconv.Itoa(heapMb)
	// Audio, cameras and the D-Pad go off here unless `avdslim enable <feature>`
	// marked them on; ApplySlimHardware honors those markers.
	ApplySlimHardware(kv)
	kv["hw.gpu.mode"] = gpuMode
	kv["hw.gpu.enabled"] = "yes"
	kv["hw.keyboard"] = "yes"
	kv["hw.cpu.ncore"] = "2"
	kv["fastboot.forceColdBoot"] = "yes"
	kv["fastboot.forceFastBoot"] = "no"

	var sb strings.Builder
	for k, v := range kv {
		sb.WriteString(fmt.Sprintf("%s=%s\n", k, v))
	}

	if err := os.WriteFile(fileToTune, []byte(sb.String()), 0644); err != nil {
		return err
	}

	// Purge stale runtime ini and snapshots to prevent restoring previous 4GB/lavapipe states
	avdDir := filepath.Dir(fileToTune)
	_ = os.Remove(filepath.Join(avdDir, "hardware-qemu.ini"))
	_ = os.Remove(filepath.Join(avdDir, "hardware-qemu.ini.lock"))
	_ = os.RemoveAll(filepath.Join(avdDir, "snapshots"))

	fmt.Printf("✅ Successfully tuned AVD %q!\n", avdName)
	fmt.Printf("   • Host RAM allocated: %d MB (prevents host memory pressure)\n", ramMb)
	fmt.Printf("   • VM Heap: %d MB\n", heapMb)
	fmt.Printf("   • Hardware Audio: %s | Cameras: %s (re-enable with `avdslim enable audio|camera`)\n",
		onOff(kv["hw.audioOutput"] == "yes"), onOff(kv["hw.camera.back"] != "none"))
	fmt.Printf("   • GPU Mode: %s (%s)\n", gpuMode, GetGpuBackendDescription(gpuMode))
	fmt.Println("   • Runtime cache & snapshots: purged (prevents restoring stale 4GB/lavapipe states)")

	is16K := strings.Contains(kv["tag.id"], "page_size_16kb") || strings.Contains(kv["image.sysdir.1"], "ps16k") || strings.Contains(kv["image.sysdir.1"], "16kb")
	isPlayStore := strings.ToLower(kv["PlayStore.enabled"]) == "true" || strings.ToLower(kv["PlayStore.enabled"]) == "yes" || strings.Contains(kv["tag.id"], "playstore")
	if is16K {
		fmt.Println("   🚨 Note: This AVD uses a 16 KB page-size image. QEMU enforces a 4096 MB RAM floor.")
		fmt.Println("      💡 Recommendation: For daily dev at 1536 MB RAM, use standard 4 KB 'Google APIs'.")
	} else if isPlayStore {
		fmt.Println("   ⚠️  Note: This AVD uses 'Google Play' (locks guest root & runs background updaters).")
		fmt.Println("      💡 Recommendation: For lowest RAM usage, use 'Google APIs' instead.")
	}

	fmt.Println()
	fmt.Printf("ℹ️  Note: If this emulator is currently running, restart it to apply changes:\n   avdslim restart %s\n\n", avdName)
	return nil
}

func onOff(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}

// AvdDir is the folder of avdName (an ini name or folder alias). An unknown
// name maps to <AVD home>/<name>.avd, as before.
func AvdDir(avdName string) string {
	if _, dir, ok := ResolveAvd(avdName); ok {
		return dir
	}
	return filepath.Join(GetAvdBaseDir(), avdName+".avd")
}

// GoldenSnapshotDir is where `avdslim bake` saves avdName's snapshot.
func GoldenSnapshotDir(avdName string) string {
	return filepath.Join(AvdDir(avdName), "snapshots", "avdslim_clean")
}

func HasGoldenSnapshot(avdName string) bool {
	info, err := os.Stat(GoldenSnapshotDir(avdName))
	return err == nil && info.IsDir()
}
