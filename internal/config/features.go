package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// hostFeature is a host-side capability avdslim turns off while slimming.
// The avdslim.* marker key in config.ini is the single source of truth for
// launch flags (`start`, `bake` and the Studio shim all read it); the hw.* keys
// are what QEMU itself reads on a cold boot.
type hostFeature struct {
	marker string            // avdslim.* key: "yes" once the user re-enabled it
	hwOn   map[string]string // config.ini values when enabled
	hwOff  map[string]string // config.ini values while slimmed
	desc   string
}

var hostFeatures = map[string]hostFeature{
	"audio": {
		marker: "avdslim.audio",
		hwOn:   map[string]string{"hw.audioInput": "yes", "hw.audioOutput": "yes"},
		hwOff:  map[string]string{"hw.audioInput": "no", "hw.audioOutput": "no"},
		desc:   "host audio in/out (drops -no-audio from launches)",
	},
	"camera": {
		marker: "avdslim.camera",
		hwOn:   map[string]string{"hw.camera.back": "emulated", "hw.camera.front": "emulated"},
		hwOff:  map[string]string{"hw.camera.back": "none", "hw.camera.front": "none"},
		desc:   "emulated cameras (drops -camera-back/-camera-front none)",
	},
	"dpad": {
		marker: "avdslim.dpad",
		hwOn:   map[string]string{"hw.dPad": "yes"},
		hwOff:  map[string]string{"hw.dPad": "no"},
		desc:   "hardware D-Pad keys",
	},
	"bootanim": {
		marker: "avdslim.bootanim",
		desc:   "boot animation (drops -no-boot-anim from launches)",
	},
}

// hostFeatureAliases maps what a user types to a hostFeatures key.
var hostFeatureAliases = map[string]string{
	"audio":         "audio",
	"sound":         "audio",
	"mic":           "audio",
	"microphone":    "audio",
	"speaker":       "audio",
	"camera":        "camera",
	"cam":           "camera",
	"webcam":        "camera",
	"dpad":          "dpad",
	"d-pad":         "dpad",
	"bootanim":      "bootanim",
	"boot-anim":     "bootanim",
	"bootanimation": "bootanim",
}

// HostFeatureName resolves a user-typed target to a host feature key.
func HostFeatureName(target string) (string, bool) {
	name, ok := hostFeatureAliases[strings.ToLower(strings.TrimSpace(target))]
	return name, ok
}

// HostFeatureNames lists the host features `enable`/`disable` accept, sorted.
func HostFeatureNames() []string {
	names := make([]string, 0, len(hostFeatures))
	for n := range hostFeatures {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// HostFeatureDescriptions returns "name — description" lines, sorted by name.
func HostFeatureDescriptions() []string {
	var lines []string
	for _, n := range HostFeatureNames() {
		lines = append(lines, fmt.Sprintf("%-9s %s", n, hostFeatures[n].desc))
	}
	return lines
}

// AvdConfigPath returns the config.ini path of an installed AVD (case-insensitive).
func AvdConfigPath(avdName string) (string, error) {
	if strings.TrimSpace(avdName) == "" {
		return "", fmt.Errorf("AVD name required")
	}
	_, dir, ok := ResolveAvd(avdName)
	if !ok {
		return "", fmt.Errorf("AVD %q not found in %s", avdName, GetAvdBaseDir())
	}
	p := filepath.Join(dir, "config.ini")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("AVD %q has no config.ini", avdName)
	}
	return p, nil
}

// readConfigIni parses key=value lines; comments and ordering are not preserved.
func readConfigIni(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	kv := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		if idx := strings.Index(line, "="); idx > 0 {
			kv[strings.TrimSpace(line[:idx])] = strings.TrimSpace(line[idx+1:])
		}
	}
	return kv, nil
}

// writeConfigIni rewrites path from kv, in sorted key order.
func writeConfigIni(path string, kv map[string]string) error {
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s=%s\n", k, kv[k])
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// HostFeatureOn reports whether the user re-enabled feature on avdName.
// Anything unreadable or unmarked counts as slimmed (off), so launches keep
// injecting the low-memory flags.
func HostFeatureOn(avdName, feature string) bool {
	f, ok := hostFeatures[feature]
	if !ok {
		return false
	}
	path, err := AvdConfigPath(avdName)
	if err != nil {
		return false
	}
	kv, err := readConfigIni(path)
	if err != nil {
		return false
	}
	return strings.EqualFold(kv[f.marker], "yes")
}

// SetHostFeature flips feature on or off in avdName's config.ini and returns
// the applied "key=value" lines. The emulator must be cold-booted for the
// hw.* changes to take effect.
func SetHostFeature(avdName, feature string, on bool) ([]string, error) {
	f, ok := hostFeatures[feature]
	if !ok {
		return nil, fmt.Errorf("unknown host feature %q (valid: %s)", feature, strings.Join(HostFeatureNames(), ", "))
	}
	path, err := AvdConfigPath(avdName)
	if err != nil {
		return nil, err
	}
	kv, err := readConfigIni(path)
	if err != nil {
		return nil, err
	}

	// Keep the one-time backup TuneAvd relies on, so config.ini stays reversible.
	if _, err := os.Stat(path + ".bak"); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("cannot check backup %s.bak: %w", path, err)
		}
		data, err := os.ReadFile(path)
		if err == nil {
			err = os.WriteFile(path+".bak", data, 0644)
		}
		if err != nil {
			return nil, fmt.Errorf("refusing to change %q without a backup: %w", avdName, err)
		}
	}

	hw := f.hwOff
	marker := "no"
	if on {
		hw = f.hwOn
		marker = "yes"
	}

	applied := []string{f.marker + "=" + marker}
	kv[f.marker] = marker
	for _, k := range sortedKeys(hw) {
		kv[k] = hw[k]
		applied = append(applied, k+"="+hw[k])
	}

	if err := writeConfigIni(path, kv); err != nil {
		return nil, err
	}
	return applied, nil
}

// ApplySlimHardware writes the slimmed hw.* values into kv, leaving alone any
// feature the user re-enabled with `avdslim enable`.
func ApplySlimHardware(kv map[string]string) {
	for _, f := range hostFeatures {
		hw := f.hwOff
		if strings.EqualFold(kv[f.marker], "yes") {
			hw = f.hwOn
		}
		for k, v := range hw {
			kv[k] = v
		}
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
