package adb

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/kdbhalala/avdslim/internal/bloat"
	"github.com/kdbhalala/avdslim/internal/config"
)

const StateFilePath = "/data/local/tmp/avdslim_state.json"

type RunningEmulator struct {
	Serial         string
	Model          string
	AndroidVersion string
	ApiLevel       string
	HostPid        int
	HostRssMb      int
	IsSlimmed      bool
}

type SlimState struct {
	Timestamp        string   `json:"timestamp"`
	DisabledPackages []string `json:"disabled_packages"`
	Preset           string   `json:"preset"`
	// Settings maps "namespace/name" to its value before avdslim first changed it
	// (a nil value means it was unset). A nil map means a <= 1.0.5 state.
	Settings map[string]*string `json:"settings"`
}

// tweak is one guest setting Slim changes; Group is the name --skip accepts.
type tweak struct {
	Group, Namespace, Name, Value string
}

var tweaks = []tweak{
	{"animations", "global", "window_animation_scale", "0"},
	{"animations", "global", "transition_animation_scale", "0"},
	{"animations", "global", "animator_duration_scale", "0"},
	{"bglimit", "global", "background_process_limit", "4"},
	{"bglimit", "global", "activity_manager_constants", "max_cached_processes=4"},
	{"sync", "global", "auto_sync", "0"},
	{"location", "secure", "location_mode", "0"},
	{"setup", "secure", "user_setup_complete", "1"},
	{"setup", "global", "device_provisioned", "1"},
	{"bluetooth", "global", "bluetooth_on", "0"},
}

// SkipGroups lists the values --skip accepts.
var SkipGroups = []string{"animations", "bglimit", "sync", "location", "setup", "bluetooth"}

// IsSkipGroup reports whether g is a valid --skip value.
func IsSkipGroup(g string) bool {
	for _, s := range SkipGroups {
		if s == g {
			return true
		}
	}
	return false
}

func (c *Client) readState(serial string) (SlimState, bool) {
	var state SlimState
	raw, err := c.Exec("-s", serial, "shell", "cat", StateFilePath)
	if err != nil || !strings.Contains(raw, "disabled_packages") {
		return state, false
	}
	return state, json.Unmarshal([]byte(raw), &state) == nil
}

// writeState saves state on the guest and reads it back, since a failed
// shell redirect does not always make adb exit non-zero.
func (c *Client) writeState(serial string, state SlimState) error {
	stateJson, err := json.Marshal(state)
	if err != nil {
		return err
	}
	// adb joins the args into one remote shell line, so quote for that shell:
	// a recorded setting value may contain a single quote.
	quoted := "'" + strings.ReplaceAll(string(stateJson), "'", `'\''`) + "'"
	out, err := c.Exec("-s", serial, "shell", "echo", quoted, ">", StateFilePath)
	if err != nil {
		return fmt.Errorf("cannot write %s: %s", StateFilePath, strings.TrimSpace(out))
	}
	got, ok := c.readState(serial)
	if !ok || strings.Join(got.DisabledPackages, ",") != strings.Join(state.DisabledPackages, ",") {
		return fmt.Errorf("cannot write %s: read-back does not match", StateFilePath)
	}
	return nil
}

func (c *Client) getSetting(serial, namespace, name string) *string {
	out, err := c.Exec("-s", serial, "shell", "settings", "get", namespace, name)
	v := strings.TrimSpace(out)
	if err != nil || v == "null" || v == "" {
		return nil
	}
	return &v
}

type Client struct {
	adbPath string
}

func NewClient() *Client {
	return &Client{
		adbPath: findAdb(),
	}
}

func (c *Client) Exec(args ...string) (string, error) {
	cmd := exec.Command(c.adbPath, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (c *Client) GetRunningEmulators() ([]RunningEmulator, error) {
	out, err := c.Exec("devices")
	if err != nil {
		return nil, err
	}

	var list []RunningEmulator
	scanner := bufio.NewScanner(strings.NewReader(out))
	isFirst := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if isFirst {
			isFirst = false
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 && parts[1] == "device" && strings.HasPrefix(parts[0], "emulator-") {
			serial := parts[0]
			model, _ := c.Exec("-s", serial, "shell", "getprop", "ro.product.model")
			ver, _ := c.Exec("-s", serial, "shell", "getprop", "ro.build.version.release")
			sdk, _ := c.Exec("-s", serial, "shell", "getprop", "ro.build.version.sdk")

			stateExists, _ := c.Exec("-s", serial, "shell", "ls", StateFilePath)
			isSlimmed := strings.Contains(stateExists, "avdslim_state.json")

			list = append(list, RunningEmulator{
				Serial:         serial,
				Model:          strings.TrimSpace(model),
				AndroidVersion: strings.TrimSpace(ver),
				ApiLevel:       strings.TrimSpace(sdk),
				IsSlimmed:      isSlimmed,
			})
		}
	}
	return list, nil
}

func (c *Client) ResolveDevice(args []string) (string, error) {
	var target string
	for _, a := range args {
		if !strings.HasPrefix(a, "--") && !strings.HasPrefix(a, "-") {
			target = a
			break
		}
	}

	running, err := c.GetRunningEmulators()
	if err != nil || len(running) == 0 {
		return "", fmt.Errorf("no running Android emulator found via adb")
	}

	if target != "" {
		// 1. Check numeric 1-based index (e.g. "1", "2")
		if idx, err := strconv.Atoi(target); err == nil {
			if idx >= 1 && idx <= len(running) {
				return running[idx-1].Serial, nil
			}
			return "", fmt.Errorf("invalid emulator index %d (only %d running emulator(s))", idx, len(running))
		}

		// 2. Check exact serial match (e.g. "emulator-5554")
		for _, r := range running {
			if strings.EqualFold(r.Serial, target) {
				return r.Serial, nil
			}
		}

		// 3. Check AVD name match (e.g. "Slim_Pixel_5")
		for _, r := range running {
			nameOut, _ := c.Exec("-s", r.Serial, "emu", "avd", "name")
			avdName := strings.TrimSpace(strings.Split(nameOut, "\n")[0])
			if strings.EqualFold(avdName, target) {
				return r.Serial, nil
			}
		}

		return "", fmt.Errorf("emulator %q not found among running emulators", target)
	}

	if len(running) == 1 {
		return running[0].Serial, nil
	}

	fmt.Println("⚠️  Multiple emulators running. Please specify one:")
	for i, r := range running {
		nameOut, _ := c.Exec("-s", r.Serial, "emu", "avd", "name")
		avdName := strings.TrimSpace(strings.Split(nameOut, "\n")[0])
		if avdName == "" {
			avdName = r.Model
		}
		fmt.Printf("  [%d] %s (%s)\n", i+1, r.Serial, avdName)
	}
	return "", fmt.Errorf("device serial or index required")
}

// Slim disables bloat packages and applies tweaks, except groups in skip.
func (c *Client) Slim(serial string, aggressive bool, keepPackages []string, skip map[string]bool) (int, error) {
	targetPackages := make([]string, 0, 70)
	for cat, pkgs := range bloat.StandardBloatCategories {
		if strings.HasPrefix(cat, "Bluetooth") && skip["bluetooth"] {
			continue
		}
		targetPackages = append(targetPackages, pkgs...)
	}
	if aggressive {
		targetPackages = append(targetPackages, bloat.AggressiveBloatPackages...)
	}

	keepMap := make(map[string]bool)
	for _, k := range keepPackages {
		keepMap[k] = true
	}

	installedRaw, err := c.Exec("-s", serial, "shell", "pm", "list", "packages")
	installedMap := make(map[string]bool)
	for _, l := range strings.Split(installedRaw, "\n") {
		if clean, ok := strings.CutPrefix(strings.TrimSpace(l), "package:"); ok && clean != "" {
			installedMap[clean] = true
		}
	}
	if err != nil || len(installedMap) == 0 {
		if strings.Contains(installedRaw, "Can't find service: package") {
			return 0, fmt.Errorf("the guest's package manager is not running: its framework is crash-looping " +
				"(the Android 16+ bug from avdslim <= 1.0.8). Fix it with `avdslim repair`")
		}
		return 0, fmt.Errorf("cannot list guest packages (is the emulator fully booted?): %s", strings.TrimSpace(installedRaw))
	}

	toDisable := make([]string, 0, len(targetPackages))
	for _, pkg := range targetPackages {
		if installedMap[pkg] && !keepMap[pkg] {
			toDisable = append(toDisable, pkg)
		}
	}

	preset := "Standard"
	if aggressive {
		preset = "Aggressive"
	}
	// Record each setting's original value before changing it. Keep values from an
	// earlier slim, or a re-slim would record the slimmed values as "original".
	// A <= 1.0.5 state has no originals and its values are already slimmed, so record none.
	prev, hasPrev := c.readState(serial)
	var originals map[string]*string
	if !hasPrev || prev.Settings != nil {
		originals = make(map[string]*string)
		for k, v := range prev.Settings {
			originals[k] = v
		}
		for _, t := range tweaks {
			key := t.Namespace + "/" + t.Name
			if _, ok := originals[key]; !ok && !skip[t.Group] {
				originals[key] = c.getSetting(serial, t.Namespace, t.Name)
			}
		}
	}

	// Write the record before touching the guest, listing everything this run
	// may disable plus what earlier runs did; `pm enable` on a package that
	// stayed enabled is harmless. No record, no changes: `off` needs it.
	state := SlimState{
		Timestamp:        time.Now().Format(time.RFC3339),
		DisabledPackages: union(prev.DisabledPackages, toDisable),
		Preset:           preset,
		Settings:         originals,
	}
	if err := c.writeState(serial, state); err != nil {
		return 0, fmt.Errorf("%w; nothing was changed", err)
	}

	// --keep on a re-slim: bring back packages an earlier slim disabled.
	recorded := prev.DisabledPackages
	for _, pkg := range prev.DisabledPackages {
		if !keepMap[pkg] {
			continue
		}
		if res, _ := c.Exec("-s", serial, "shell", "pm", "enable", pkg); strings.Contains(res, "new state") {
			recorded = remove(recorded, pkg)
			fmt.Printf("   ✓ Re-enabled (--keep): %s\n", pkg)
		}
	}

	disabledList := make([]string, 0, len(toDisable))
	for _, pkg := range toDisable {
		res, _ := c.Exec("-s", serial, "shell", "pm", "disable-user", "--user", "0", pkg)
		if strings.Contains(res, "disabled-user") || strings.Contains(res, "new state") {
			disabledList = append(disabledList, pkg)
			fmt.Printf("   ✓ Disabled: %s\n", pkg)
		}
	}

	state.DisabledPackages = union(recorded, disabledList)
	if err := c.writeState(serial, state); err != nil {
		// The first record is a superset, so `off` still undoes everything.
		fmt.Printf("   ⚠️  Could not update the state record (%v); `avdslim off` still restores everything\n", err)
	}

	for _, t := range tweaks {
		if !skip[t.Group] {
			c.Exec("-s", serial, "shell", "settings", "put", t.Namespace, t.Name, t.Value)
		}
	}
	if !skip["bluetooth"] {
		c.Exec("-s", serial, "shell", "cmd", "bluetooth_manager", "disable")
	}

	// Trim memory
	c.Exec("-s", serial, "shell", "am", "kill-all")
	c.Exec("-s", serial, "shell", "am", "trim-memory", "--all", "COMPLETE")
	c.Exec("-s", serial, "shell", "su", "0", "sync")
	c.Exec("-s", serial, "shell", "su", "0", "echo 3 > /proc/sys/vm/drop_caches")

	return len(disabledList), nil
}

func (c *Client) Restore(serial string) (int, error) {
	var packagesToEnable []string
	state, hasState := c.readState(serial)
	if hasState {
		packagesToEnable = state.DisabledPackages
	}

	if len(packagesToEnable) == 0 {
		for _, pkgs := range bloat.StandardBloatCategories {
			packagesToEnable = append(packagesToEnable, pkgs...)
		}
		packagesToEnable = append(packagesToEnable, bloat.AggressiveBloatPackages...)
	}

	restoredCount := 0
	var failed []string
	for _, pkg := range packagesToEnable {
		res, _ := c.Exec("-s", serial, "shell", "pm", "enable", pkg)
		if strings.Contains(res, "new state") {
			restoredCount++
			fmt.Printf("   ✓ Enabled: %s\n", pkg)
		} else if hasState {
			failed = append(failed, pkg)
			fmt.Printf("   ✗ Could not enable %s: %s\n", pkg, strings.TrimSpace(res))
		}
	}

	if state.Settings != nil {
		// Exact inverse: put back what Slim recorded; skipped groups were never touched.
		for _, t := range tweaks {
			orig, recorded := state.Settings[t.Namespace+"/"+t.Name]
			switch {
			case !recorded:
			case orig == nil:
				c.Exec("-s", serial, "shell", "settings", "delete", t.Namespace, t.Name)
			default:
				c.Exec("-s", serial, "shell", "settings", "put", t.Namespace, t.Name, *orig)
			}
		}
		if orig, recorded := state.Settings["global/bluetooth_on"]; recorded && (orig == nil || *orig != "0") {
			c.Exec("-s", serial, "shell", "cmd", "bluetooth_manager", "enable")
		}
	} else {
		// ponytail: state from avdslim <= 1.0.5 has no originals, so fall back to stock values.
		c.Exec("-s", serial, "shell", "settings", "put", "global", "window_animation_scale", "1")
		c.Exec("-s", serial, "shell", "settings", "put", "global", "transition_animation_scale", "1")
		c.Exec("-s", serial, "shell", "settings", "put", "global", "animator_duration_scale", "1")
		c.Exec("-s", serial, "shell", "settings", "delete", "global", "background_process_limit")
		c.Exec("-s", serial, "shell", "settings", "put", "global", "auto_sync", "1")
		c.Exec("-s", serial, "shell", "settings", "put", "secure", "location_mode", "3")
		c.Exec("-s", serial, "shell", "settings", "put", "global", "bluetooth_on", "1")
		c.Exec("-s", serial, "shell", "cmd", "bluetooth_manager", "enable")
	}

	if len(failed) > 0 {
		// Keep only what is still disabled so `avdslim off` can be retried.
		// Settings stay recorded; putting them back again is harmless.
		state.DisabledPackages = failed
		if err := c.writeState(serial, state); err != nil {
			return restoredCount, fmt.Errorf("%d package(s) still disabled (%s) and the state record could not be updated: %v", len(failed), strings.Join(failed, ", "), err)
		}
		return restoredCount, fmt.Errorf("%d package(s) still disabled: %s (run `avdslim off` again)", len(failed), strings.Join(failed, ", "))
	}
	c.Exec("-s", serial, "shell", "rm", "-f", StateFilePath)

	return restoredCount, nil
}

// union returns a followed by the items of b not already in a.
func union(a, b []string) []string {
	out := append([]string(nil), a...)
	for _, s := range b {
		if !contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

func remove(list []string, s string) []string {
	var out []string
	for _, x := range list {
		if x != s {
			out = append(out, x)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// repairScript builds the guest shell script that re-enables pkgs by editing
// package-restrictions.xml directly, since `pm` is unavailable while
// system_server crash-loops. The file carries fs-verity, so it is replaced by
// rename rather than written in place.
func repairScript(pkgs []string) string {
	var check, fix strings.Builder
	for _, p := range pkgs {
		name := `name="` + strings.ReplaceAll(p, ".", `\.`) + `"`
		fmt.Fprintf(&check, `grep -q '%s[^>]*enabled="[23]"' $t && echo FIXED:%s && n=1; `, name, p)
		fmt.Fprintf(&fix, ` -e 's|\(%s[^>]*\) enabled="[23]" enabledCaller="[^"]*"|\1|'`, name)
	}
	return `set -e; d=/data/system/users/0; f=$d/package-restrictions.xml; t=/data/local/tmp/avdslim_pr.xml; n=0; ` +
		`abx2xml $f $t; ` + check.String() +
		`if [ $n = 1 ]; then stop; sed -i` + fix.String() + ` $t; ` +
		`xml2abx $t $d/avdslim_pr.new; chown system:system $d/avdslim_pr.new; chmod 660 $d/avdslim_pr.new; ` +
		`restorecon $d/avdslim_pr.new; mv -f $d/avdslim_pr.new $f; rm -f $f.reservecopy; start; fi; rm -f $t`
}

// Repair re-enables boot-critical packages on a guest stuck in a
// system_server crash loop and restarts the framework. Needs `adb root`
// (google_apis images; Play Store images refuse it).
func (c *Client) Repair(serial string) ([]string, error) {
	out, _ := c.Exec("-s", serial, "root")
	if strings.Contains(out, "cannot run as root") {
		return nil, fmt.Errorf("adb root not permitted on this image (Play Store build); cold boot with -wipe-data instead")
	}
	c.Exec("-s", serial, "wait-for-device")

	out, err := c.Exec("-s", serial, "shell", repairScript(bloat.BootCritical))
	if err != nil {
		return nil, fmt.Errorf("repair script failed: %s", strings.TrimSpace(out))
	}
	var fixed []string
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(strings.TrimSpace(line), "FIXED:"); ok {
			fixed = append(fixed, p)
		}
	}
	// Old crashes would make CrashLooping report the loop again while the framework restarts.
	c.Exec("-s", serial, "logcat", "-b", "crash", "-c")
	return fixed, nil
}

// CrashLooping reports the Android 16+ system_server crash loop caused by a
// disabled Bluetooth APK (see bloat.BootCritical). Repair fixes it. While it
// loops, sys.boot_completed never becomes 1.
func (c *Client) CrashLooping(serial string) bool {
	out, _ := c.Exec("-s", serial, "logcat", "-d", "-b", "crash")
	return strings.Contains(out, "FEATURE_BLUETOOTH")
}

var featureAliases = map[string][]string{
	"bluetooth":    {"com.google.android.bluetooth", "com.android.bluetoothmidiservice"},
	"bt":           {"com.google.android.bluetooth", "com.android.bluetoothmidiservice"},
	"maps":         {"com.google.android.apps.maps"},
	"photos":       {"com.google.android.apps.photos"},
	"chrome":       {"com.android.chrome"},
	"camera":       {"com.android.camera2", "com.android.cameraextensions", "com.android.DeviceAsWebcam"},
	"store":        {"com.android.vending"},
	"playstore":    {"com.android.vending"},
	"vending":      {"com.android.vending"},
	"youtube":      {"com.google.android.youtube", "com.google.android.apps.youtube.music"},
	"music":        {"com.google.android.music", "com.android.music"},
	"sms":          {"com.google.android.apps.messaging", "com.android.mms"},
	"messaging":    {"com.google.android.apps.messaging", "com.android.mms"},
	"dialer":       {"com.google.android.dialer", "com.android.dialer"},
	"phone":        {"com.google.android.dialer", "com.android.dialer"},
	"contacts":     {"com.google.android.contacts", "com.android.contacts"},
	"docs":         {"com.google.android.apps.docs"},
	"drive":        {"com.google.android.apps.docs"},
	"printing":     {"com.android.printspooler", "com.android.bips", "com.google.android.printservice.recommendation"},
	"print":        {"com.android.printspooler", "com.android.bips", "com.google.android.printservice.recommendation"},
	"search":       {"com.google.android.googlequicksearchbox"},
	"assistant":    {"com.google.android.googlequicksearchbox"},
	"wellbeing":    {"com.google.android.apps.wellbeing"},
	"clock":        {"com.android.deskclock", "com.google.android.deskclock"},
	"calculator":   {"com.android.calculator2", "com.google.android.calculator"},
	"calendar":     {"com.android.calendar", "com.google.android.calendar"},
	"voiceaccess":  {"com.google.android.apps.accessibility.voiceaccess"},
	"markup":       {"com.google.android.markup"},
	"safety":       {"com.google.android.apps.safetyhub"},
	"multidisplay": {"com.android.emulator.multidisplay"},
}

// Enable re-enables a specific feature, setting group, or package on the device,
// and updates the on-device state JSON.
func (c *Client) Enable(serial, target string) ([]string, error) {
	norm := strings.ToLower(strings.TrimSpace(target))
	var actions []string
	var packagesToEnable []string
	var settingsToRemove []string

	state, hasState := c.readState(serial)

	switch norm {
	case "bluetooth", "bt":
		packagesToEnable = featureAliases["bluetooth"]
		c.Exec("-s", serial, "shell", "settings", "put", "global", "bluetooth_on", "1")
		c.Exec("-s", serial, "shell", "cmd", "bluetooth_manager", "enable")
		actions = append(actions, "Bluetooth enabled (bluetooth_manager ON, settings put global bluetooth_on 1)")
		settingsToRemove = append(settingsToRemove, "global/bluetooth_on")

	case "animations", "anim":
		c.Exec("-s", serial, "shell", "settings", "put", "global", "window_animation_scale", "1")
		c.Exec("-s", serial, "shell", "settings", "put", "global", "transition_animation_scale", "1")
		c.Exec("-s", serial, "shell", "settings", "put", "global", "animator_duration_scale", "1")
		actions = append(actions, "Animations restored to 1.0x (stock fluid)")
		settingsToRemove = append(settingsToRemove, "global/window_animation_scale", "global/transition_animation_scale", "global/animator_duration_scale")

	case "sync":
		c.Exec("-s", serial, "shell", "settings", "put", "global", "auto_sync", "1")
		actions = append(actions, "Auto-sync enabled (settings put global auto_sync 1)")
		settingsToRemove = append(settingsToRemove, "global/auto_sync")

	case "location", "gps":
		c.Exec("-s", serial, "shell", "settings", "put", "secure", "location_mode", "3")
		actions = append(actions, "Location mode restored (settings put secure location_mode 3)")
		settingsToRemove = append(settingsToRemove, "secure/location_mode")

	case "bglimit":
		c.Exec("-s", serial, "shell", "settings", "delete", "global", "background_process_limit")
		c.Exec("-s", serial, "shell", "settings", "delete", "global", "activity_manager_constants")
		actions = append(actions, "Background process limits cleared")
		settingsToRemove = append(settingsToRemove, "global/background_process_limit", "global/activity_manager_constants")

	default:
		if pkgs, ok := featureAliases[norm]; ok {
			packagesToEnable = pkgs
		} else if strings.Contains(target, ".") {
			packagesToEnable = []string{target}
		} else {
			var matched []string
			for _, pkgs := range bloat.StandardBloatCategories {
				for _, p := range pkgs {
					parts := strings.Split(p, ".")
					if strings.EqualFold(parts[len(parts)-1], norm) {
						matched = append(matched, p)
					}
				}
			}
			for _, p := range bloat.AggressiveBloatPackages {
				parts := strings.Split(p, ".")
				if strings.EqualFold(parts[len(parts)-1], norm) {
					matched = append(matched, p)
				}
			}
			if len(matched) > 0 {
				packagesToEnable = matched
			} else {
				return nil, fmt.Errorf("unknown feature or package %q (specify 'bluetooth', 'animations', 'sync', 'location', or an app name/package)", target)
			}
		}
	}

	enabledSet := make(map[string]bool)
	for _, pkg := range packagesToEnable {
		res, _ := c.Exec("-s", serial, "shell", "pm", "enable", pkg)
		if strings.Contains(res, "enabled") || strings.Contains(res, "new state") || res == "" {
			actions = append(actions, fmt.Sprintf("Enabled package: %s", pkg))
			enabledSet[pkg] = true
		}
	}

	if hasState {
		var updatedDisabled []string
		for _, p := range state.DisabledPackages {
			if !enabledSet[p] {
				updatedDisabled = append(updatedDisabled, p)
			}
		}
		state.DisabledPackages = updatedDisabled

		if state.Settings != nil {
			for _, s := range settingsToRemove {
				delete(state.Settings, s)
			}
		}

		if len(state.DisabledPackages) == 0 && (state.Settings == nil || len(state.Settings) == 0) {
			c.Exec("-s", serial, "shell", "rm", "-f", StateFilePath)
		} else if err := c.writeState(serial, state); err != nil {
			return actions, fmt.Errorf("enabled, but the state record was not updated (`avdslim off` may re-apply it): %w", err)
		}
	}

	return actions, nil
}

func findAdb() string {
	return config.FindAdbExecutable()
}
