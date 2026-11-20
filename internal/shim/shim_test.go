package shim

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeSdk points ANDROID_HOME and HOME at temp dirs holding an SDK whose
// emulator binary contains content, and returns the emulator dir.
func fakeSdk(t *testing.T, content string) string {
	if runtime.GOOS == "windows" {
		t.Skip("the shim is not supported on Windows")
	}
	sdk := t.TempDir()
	t.Setenv("ANDROID_HOME", sdk)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("ANDROID_AVD_HOME", "")
	t.Setenv("ANDROID_SDK_HOME", "")
	t.Setenv("ANDROID_USER_HOME", "")
	dir := filepath.Join(sdk, "emulator")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "emulator"), content)
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func TestInstallUninstallRoundTrip(t *testing.T) {
	dir := fakeSdk(t, "REAL-EMULATOR-v1")
	emu, real := filepath.Join(dir, "emulator"), filepath.Join(dir, "emulator.real")

	if err := InstallShim(1536); err != nil {
		t.Fatal(err)
	}
	if !hasShimHeader(emu) || readFile(t, real) != "REAL-EMULATOR-v1" {
		t.Fatal("install did not move the real binary aside and write the shim")
	}
	// Reinstalling (e.g. new --ram) must not touch the real binary.
	if err := InstallShim(2048); err != nil {
		t.Fatal(err)
	}
	if readFile(t, real) != "REAL-EMULATOR-v1" {
		t.Fatal("reinstall changed emulator.real")
	}

	if err := UninstallShim(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, emu); got != "REAL-EMULATOR-v1" {
		t.Errorf("after uninstall emulator = %q, want the original", got)
	}
	if _, err := os.Stat(real); err == nil {
		t.Error("emulator.real left behind")
	}
}

// After an SDK update overwrote the shim, emulator is the new real binary and
// emulator.real the old one. Uninstall must not swap the old one back in.
func TestUninstallAfterSDKUpdateKeepsNewEmulator(t *testing.T) {
	dir := fakeSdk(t, "REAL-EMULATOR-v1")
	emu := filepath.Join(dir, "emulator")
	if err := InstallShim(1536); err != nil {
		t.Fatal(err)
	}
	writeFile(t, emu, "REAL-EMULATOR-v2") // the SDK update
	if !IsShimOverwritten() {
		t.Fatal("IsShimOverwritten = false after the update")
	}

	if err := UninstallShim(); err == nil {
		t.Error("uninstall succeeded; want a refusal explaining the SDK update")
	}
	if got := readFile(t, emu); got != "REAL-EMULATOR-v2" {
		t.Errorf("emulator = %q, want the updated v2 binary kept", got)
	}
}

func TestReinstallAfterSDKUpdateWrapsNewEmulator(t *testing.T) {
	dir := fakeSdk(t, "REAL-EMULATOR-v1")
	emu, real := filepath.Join(dir, "emulator"), filepath.Join(dir, "emulator.real")
	if err := InstallShim(1536); err != nil {
		t.Fatal(err)
	}
	writeFile(t, emu, "REAL-EMULATOR-v2")

	if err := InstallShim(1536); err != nil {
		t.Fatal(err)
	}
	if !hasShimHeader(emu) || readFile(t, real) != "REAL-EMULATOR-v2" {
		t.Errorf("reinstall should wrap v2; emulator.real = %q", readFile(t, real))
	}
}

func TestIsShimOutdated(t *testing.T) {
	dir := fakeSdk(t, "REAL-EMULATOR-v1")
	if err := InstallShim(1536); err != nil {
		t.Fatal(err)
	}
	if IsShimOutdated() {
		t.Fatal("fresh shim reported outdated")
	}
	// A 1.0.13 shim: has the defaults file and markers, but hard-codes $HOME/.android/avd.
	emu := filepath.Join(dir, "emulator")
	writeFile(t, emu, strings.ReplaceAll(readFile(t, emu), "ANDROID_AVD_HOME", "UNUSED"))
	if IsShimOutdated() {
		t.Error("flagged without ANDROID_AVD_HOME set, where the old shim works")
	}
	t.Setenv("ANDROID_AVD_HOME", t.TempDir())
	if !IsShimOutdated() {
		t.Error("not flagged although it ignores ANDROID_AVD_HOME")
	}
}

// After an avdslim upgrade, an outdated shim is rewritten in place: same
// --ram, same emulator.real, current script.
func TestRefreshOutdatedShim(t *testing.T) {
	dir := fakeSdk(t, "REAL-EMULATOR-v1")
	emu, real := filepath.Join(dir, "emulator"), filepath.Join(dir, "emulator.real")
	if err := InstallShim(2048); err != nil {
		t.Fatal(err)
	}
	// Turn it into a pre-1.0.13 shim (no feature markers).
	writeFile(t, emu, strings.ReplaceAll(readFile(t, emu), "avdslim_flag", "old_flag"))
	if !IsShimOutdated() {
		t.Fatal("setup: shim should be outdated")
	}

	refreshed, err := RefreshIfOutdated()
	if err != nil || !refreshed {
		t.Fatalf("RefreshIfOutdated = %v, %v; want true, nil", refreshed, err)
	}
	if IsShimOutdated() {
		t.Error("still outdated after refresh")
	}
	if !strings.Contains(readFile(t, emu), "RAM=2048") {
		t.Error("--ram 2048 from the old shim was not kept")
	}
	if readFile(t, real) != "REAL-EMULATOR-v1" {
		t.Error("emulator.real changed")
	}

	if refreshed, _ := RefreshIfOutdated(); refreshed {
		t.Error("refreshed a current shim again")
	}
}

func TestRefreshLeavesNoShimAlone(t *testing.T) {
	dir := fakeSdk(t, "REAL-EMULATOR-v1")
	if refreshed, err := RefreshIfOutdated(); refreshed || err != nil {
		t.Fatalf("RefreshIfOutdated with no shim = %v, %v", refreshed, err)
	}
	if got := readFile(t, filepath.Join(dir, "emulator")); got != "REAL-EMULATOR-v1" {
		t.Errorf("emulator changed to %q", got)
	}
}

func TestUninstallWithoutShim(t *testing.T) {
	dir := fakeSdk(t, "REAL-EMULATOR-v1")
	if err := UninstallShim(); err == nil {
		t.Error("uninstall with no shim installed succeeded")
	}
	if got := readFile(t, filepath.Join(dir, "emulator")); got != "REAL-EMULATOR-v1" {
		t.Errorf("emulator changed to %q", got)
	}
}

// runShim installs the shim over a fake emulator that prints its arguments,
// runs it with args, and returns what the "real" emulator received.
func runShim(t *testing.T, env []string, args ...string) string {
	t.Helper()
	dir := fakeSdk(t, "#!/bin/bash\necho \"$*\"\n")
	if err := InstallShim(1536); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(filepath.Join(dir, "emulator"), args...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("shim failed: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestShimInjectsLowMemoryFlags(t *testing.T) {
	got := runShim(t, nil, "-avd", "Pixel")
	for _, want := range []string{"-memory 1536", "-lowram", "-no-audio", "-camera-back none", "-avd Pixel"} {
		if !strings.Contains(got, want) {
			t.Errorf("args %q missing %q", got, want)
		}
	}
	if got := runShim(t, nil, "-avd", "Pixel", "--no-slim"); strings.Contains(got, "-lowram") {
		t.Errorf("--no-slim still injected flags: %q", got)
	}
}

// Snapshot and feature markers must be found under ANDROID_AVD_HOME when set,
// as they are everywhere else in avdslim.
func TestShimHonorsAndroidAvdHome(t *testing.T) {
	avdHome := t.TempDir()
	avd := filepath.Join(avdHome, "Pixel.avd")
	if err := os.MkdirAll(filepath.Join(avd, "snapshots", "avdslim_clean"), 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(avd, "config.ini"), "avdslim.audio=yes\n")

	got := runShim(t, []string{"ANDROID_AVD_HOME=" + avdHome}, "-avd", "Pixel")
	if !strings.Contains(got, "-snapshot avdslim_clean") {
		t.Errorf("Golden Snapshot under ANDROID_AVD_HOME not used: %q", got)
	}
	if strings.Contains(got, "-no-audio") {
		t.Errorf("avdslim.audio=yes under ANDROID_AVD_HOME ignored: %q", got)
	}
}

// An AVD whose .ini name differs from its folder (#7) and an AVD home under
// ANDROID_SDK_HOME (#6) must get the same snapshot and feature markers as the
// Go side (config.ResolveAvd) finds for them.
func TestShimResolvesAvdFolderLikeConfig(t *testing.T) {
	cases := []struct {
		name string
		env  func(t *testing.T, home string) []string
		ini  func(folder string) string
		dir  func(home string) string
	}{
		{
			name: "absolute path= with a mismatched folder name",
			env:  func(t *testing.T, home string) []string { return []string{"HOME=" + home} },
			ini:  func(folder string) string { return "path=" + folder + "\n" },
			dir:  func(home string) string { return filepath.Join(home, "Pixel_Real.avd") },
		},
		{
			name: "path.rel= under ANDROID_SDK_HOME",
			env: func(t *testing.T, home string) []string {
				return []string{"HOME=" + home, "ANDROID_SDK_HOME=" + filepath.Join(home, "sdkhome")}
			},
			ini: func(folder string) string { return "path.rel=avd/Pixel_Real.avd\n" },
			dir: func(home string) string { return filepath.Join(home, "sdkhome", "avd", "Pixel_Real.avd") },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			avdHome := filepath.Join(home, ".android", "avd")
			if strings.Contains(tc.name, "path.rel") {
				avdHome = filepath.Join(home, "sdkhome", "avd")
			}
			if err := os.MkdirAll(avdHome, 0755); err != nil {
				t.Fatal(err)
			}
			folder := tc.dir(home)
			if err := os.MkdirAll(filepath.Join(folder, "snapshots", "avdslim_clean"), 0755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(folder, "config.ini"), "avdslim.audio=yes\n")
			writeFile(t, filepath.Join(avdHome, "Studio_Pixel.ini"), tc.ini(folder))

			got := runShim(t, tc.env(t, home), "-avd", "Studio_Pixel")
			if !strings.Contains(got, "-snapshot avdslim_clean") {
				t.Errorf("Golden Snapshot in the ini's folder not used: %q", got)
			}
			if strings.Contains(got, "-no-audio") {
				t.Errorf("avdslim.audio=yes in the ini's folder ignored: %q", got)
			}
		})
	}
}
