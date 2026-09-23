package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kdbhalala/avdslim/internal/adbtest"
)

// TestMain lets the test binary act as avdslim: run() re-executes it with
// AVDSLIM_RUN_MAIN=1, so each command runs the real main() in its own process.
func TestMain(m *testing.M) {
	if os.Getenv("AVDSLIM_RUN_MAIN") == "1" {
		os.Args = append([]string{"avdslim"}, strings.Fields(os.Getenv("AVDSLIM_ARGS"))...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// serial uses a console port no real emulator is likely to hold, so host
// memory probes (lsof on the port) find nothing.
const serial = "emulator-5990"

// device is a fake guest: a dir holding the fake adb and its state files,
// plus an isolated HOME shared by every run() of the test.
type device struct {
	t         *testing.T
	dir, home string
	env       []string
}

func newDevice(t *testing.T, running bool) *device {
	if runtime.GOOS == "windows" {
		t.Skip("fake adb is a bash script")
	}
	d := &device{t: t, dir: t.TempDir(), home: t.TempDir()}
	if _, err := adbtest.Install(d.dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(d.home, "avd"), 0755); err != nil {
		t.Fatal(err)
	}
	if running {
		d.write("devices", "List of devices attached\n"+serial+"\tdevice\n")
		d.write("avd_"+serial, "Avdslim_Test_Avd\n")
		d.write("packages", "com.google.android.apps.maps\ncom.google.android.youtube\ncom.android.chrome\n")
	}
	return d
}

func (d *device) write(name, content string) {
	if err := os.WriteFile(filepath.Join(d.dir, name), []byte(content), 0644); err != nil {
		d.t.Fatal(err)
	}
}

func (d *device) read(name string) string {
	b, _ := os.ReadFile(filepath.Join(d.dir, name))
	return string(b)
}

// run executes `avdslim args...` with the fake adb first on PATH and HOME
// isolated, returning combined output and whether it exited 0.
func (d *device) run(args ...string) (string, bool) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append([]string{
		"AVDSLIM_RUN_MAIN=1",
		"AVDSLIM_ARGS=" + strings.Join(args, " "),
		"PATH=" + d.dir + string(os.PathListSeparator) + "/usr/bin:/bin:/usr/sbin:/sbin",
		"HOME=" + d.home,
		"ANDROID_AVD_HOME=" + filepath.Join(d.home, "avd"),
	}, d.env...)
	out, err := cmd.CombinedOutput()
	return string(out), err == nil
}

func mustContain(t *testing.T, out string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q:\n%s", w, out)
		}
	}
}

func TestOnOffRoundTrip(t *testing.T) {
	d := newDevice(t, true)

	out, ok := d.run("on")
	if !ok {
		t.Fatalf("on failed:\n%s", out)
	}
	mustContain(t, out, "Slimming complete for "+serial, "Disabled: com.google.android.apps.maps", "Successfully disabled 2 packages")
	if !strings.Contains(d.read("state"), "com.google.android.apps.maps") {
		t.Fatalf("state missing disabled package: %q", d.read("state"))
	}
	if d.read("global_window_animation_scale") != "" {
		t.Error("animations changed though skipped by default")
	}
	if d.read("global_bluetooth_on") != "0" {
		t.Error("bluetooth not turned off")
	}

	out, ok = d.run("list")
	if !ok {
		t.Fatalf("list failed:\n%s", out)
	}
	mustContain(t, out, serial, "SLIMMED")

	out, ok = d.run("off")
	if !ok {
		t.Fatalf("off failed:\n%s", out)
	}
	mustContain(t, out, "Restored 2 packages", "Successfully restored "+serial)
	if d.read("state") != "" {
		t.Error("state file not removed")
	}
	if d.read("global_bluetooth_on") != "" {
		t.Errorf("bluetooth_on = %q, want unset (its original)", d.read("global_bluetooth_on"))
	}
}

// `start` boots the Golden Snapshot with -no-snapshot-save, so it would bring
// back the slimmed state after `off`; off must say so.
func TestOffWarnsAboutGoldenSnapshot(t *testing.T) {
	d := newDevice(t, true)
	d.run("on")
	out, _ := d.run("off")
	if strings.Contains(out, "Golden Snapshot") {
		t.Errorf("warned without a snapshot:\n%s", out)
	}

	d.run("on")
	if err := os.MkdirAll(filepath.Join(d.home, "avd", "Avdslim_Test_Avd.avd", "snapshots", "avdslim_clean"), 0755); err != nil {
		t.Fatal(err)
	}
	out, ok := d.run("off")
	if !ok {
		t.Fatalf("off failed:\n%s", out)
	}
	mustContain(t, out, "Golden Snapshot", "avdslim unbake Avdslim_Test_Avd")
}

// unbake must delete the snapshot where every other command looks for it:
// under ANDROID_AVD_HOME when set, not ~/.android/avd.
func TestUnbakeHonorsAndroidAvdHome(t *testing.T) {
	d := newDevice(t, false)
	avd := filepath.Join(d.home, "avd", "Avdslim_Test_Pixel.avd")
	snap := filepath.Join(avd, "snapshots", "avdslim_clean")
	if err := os.MkdirAll(snap, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(avd, "config.ini"), []byte("hw.ramSize=1536\n"), 0644); err != nil {
		t.Fatal(err)
	}

	out, _ := d.run("unbake", "Avdslim_Test_Pixel")
	mustContain(t, out, "Removed Golden Snapshot")
	if _, err := os.Stat(snap); err == nil {
		t.Error("snapshot still there")
	}
}

func TestOnFlags(t *testing.T) {
	d := newDevice(t, true)
	out, ok := d.run("on", "--no-anim", "--skip=bluetooth,sync", "--keep=com.google.android.youtube")
	if !ok {
		t.Fatalf("on failed:\n%s", out)
	}
	mustContain(t, out, "Left unchanged (--skip): bluetooth, sync", "Successfully disabled 1 packages")
	if d.read("global_window_animation_scale") != "0" {
		t.Error("--no-anim did not zero animations")
	}
	if d.read("global_bluetooth_on") != "" || d.read("global_auto_sync") != "" {
		t.Error("skipped groups were changed")
	}
	if strings.Contains(d.read("calls"), "disable-user --user 0 com.google.android.youtube") {
		t.Error("--keep package was disabled")
	}
}

func TestOnRejectsUnknownSkip(t *testing.T) {
	d := newDevice(t, true)
	out, ok := d.run("on", "--skip=bogus")
	if ok {
		t.Fatalf("expected non-zero exit:\n%s", out)
	}
	mustContain(t, out, `Unknown --skip value "bogus"`)
	if strings.Contains(d.read("calls"), "disable-user") {
		t.Error("packages disabled despite bad flag")
	}
}

func TestOnFailsWithoutStateRecord(t *testing.T) {
	d := newDevice(t, true)
	d.write("readonly", "")
	out, ok := d.run("on")
	if ok {
		t.Fatalf("on succeeded without a state record:\n%s", out)
	}
	mustContain(t, out, "nothing was changed")
	if strings.Contains(d.read("calls"), "disable-user") {
		t.Error("packages disabled without a state record")
	}
}

func TestOffPartialFailureIsRetryable(t *testing.T) {
	d := newDevice(t, true)
	if out, ok := d.run("on"); !ok {
		t.Fatalf("on failed:\n%s", out)
	}
	d.write("enable_fails", "com.google.android.youtube\n")
	out, ok := d.run("off")
	if ok {
		t.Fatalf("off reported success with a package still disabled:\n%s", out)
	}
	mustContain(t, out, "still disabled: com.google.android.youtube")

	os.Remove(filepath.Join(d.dir, "enable_fails"))
	if out, ok := d.run("off"); !ok {
		t.Fatalf("retry failed:\n%s", out)
	}
	if d.read("state") != "" {
		t.Error("state file kept after a full restore")
	}
}

func TestNoEmulator(t *testing.T) {
	d := newDevice(t, false)
	// Scripts and CI rely on a non-zero exit when nothing was done.
	for _, cmd := range []string{"on", "off", "measure", "snapshot"} {
		out, ok := d.run(cmd)
		if ok {
			t.Errorf("%s exited 0 with no emulator", cmd)
		}
		mustContain(t, strings.ToLower(out), "no running android emulator")
	}
	out, ok := d.run("enable", "maps")
	if ok {
		t.Error("enable exited 0 with no emulator")
	}
	mustContain(t, out, "No running Android emulators")
}

func TestEnableUpdatesState(t *testing.T) {
	d := newDevice(t, true)
	if out, ok := d.run("on"); !ok {
		t.Fatalf("on failed:\n%s", out)
	}
	out, ok := d.run("enable", "maps")
	if !ok {
		t.Fatalf("enable failed:\n%s", out)
	}
	state := d.read("state")
	if strings.Contains(state, "com.google.android.apps.maps") {
		t.Errorf("maps still recorded as disabled: %s", state)
	}
	if !strings.Contains(state, "com.google.android.youtube") {
		t.Errorf("other disabled packages lost: %s", state)
	}
}

func TestDoctor(t *testing.T) {
	d := newDevice(t, false)
	out, ok := d.run("doctor")
	if !ok {
		t.Fatalf("doctor failed:\n%s", out)
	}
	mustContain(t, out, "ADB: "+filepath.Join(d.dir, "adb"), "No active emulators running")

	d = newDevice(t, true)
	d.run("on")
	out, _ = d.run("doctor")
	mustContain(t, out, serial, "Status: ⚡ Slimmed")
}

// fakeAvdmanager handles `create avd -n NAME -k PKG -d DEV` like the real one:
// it writes NAME.avd/config.ini under ANDROID_AVD_HOME with stock values.
const fakeAvdmanager = `#!/bin/bash
echo "$*" >> "$(dirname "$0")/avdmanager_calls"
cat > /dev/null # consume the hardware-profile answer
while [ $# -gt 0 ]; do
  case "$1" in -n) n="$2" ;; -k) k="$2" ;; -d) dev="$2" ;; esac
  shift
done
[ "$dev" = "bogus" ] && { echo "Error: No device found matching --device bogus" >&2; exit 1; }
mkdir -p "$ANDROID_AVD_HOME/$n.avd"
printf 'hw.ramSize=4096\nhw.gpu.mode=auto\nimage.sysdir.1=%s\n' "$k" > "$ANDROID_AVD_HOME/$n.avd/config.ini"
printf 'path=%s\n' "$ANDROID_AVD_HOME/$n.avd" > "$ANDROID_AVD_HOME/$n.ini"
`

// withSdk gives the device an SDK holding the given system images (paths
// under system-images/, "ABI" replaced by the host ABI) and a fake avdmanager.
func (d *device) withSdk(images ...string) {
	sdk := d.t.TempDir()
	abi := "x86_64"
	if runtime.GOARCH == "arm64" {
		abi = "arm64-v8a"
	}
	for _, img := range images {
		if err := os.MkdirAll(filepath.Join(sdk, "system-images", strings.ReplaceAll(img, "ABI", abi)), 0755); err != nil {
			d.t.Fatal(err)
		}
	}
	d.write("avdmanager", fakeAvdmanager)
	if err := os.Chmod(filepath.Join(d.dir, "avdmanager"), 0755); err != nil {
		d.t.Fatal(err)
	}
	d.env = append(d.env, "ANDROID_HOME="+sdk)
}

func TestCreate(t *testing.T) {
	d := newDevice(t, false)
	d.withSdk("android-34/google_apis/ABI", "android-35/google_apis/ABI", "android-36/google_apis_ps16k/ABI")

	out, ok := d.run("create", "Slim_Test")
	if !ok {
		t.Fatalf("create failed:\n%s", out)
	}
	mustContain(t, out, "android-35;google_apis", "Successfully tuned AVD \"Slim_Test\"", "avdslim bake Slim_Test")
	calls := d.read("avdmanager_calls")
	mustContain(t, calls, "create avd -n Slim_Test", ";android-35;google_apis;", "-d pixel_5")

	cfg, _ := os.ReadFile(filepath.Join(d.home, "avd", "Slim_Test.avd", "config.ini"))
	mustContain(t, string(cfg), "hw.ramSize=1536")
	if _, err := os.Stat(filepath.Join(d.home, "avd", "Slim_Test.avd", "config.ini.bak")); err != nil {
		t.Error("no config.ini.bak backup of avdmanager's stock config")
	}

	// A second create must not overwrite the existing AVD.
	out, ok = d.run("create", "slim_test")
	if ok {
		t.Fatalf("create over existing AVD succeeded:\n%s", out)
	}
	mustContain(t, out, "already exists")
	if n := strings.Count(d.read("avdmanager_calls"), "\n"); n != 1 {
		t.Errorf("avdmanager called %d times, want 1", n)
	}
}

func TestCreateOptions(t *testing.T) {
	d := newDevice(t, false)
	d.withSdk("android-34/google_apis/ABI", "android-35/google_apis/ABI")

	out, ok := d.run("create", "Old", "--api=34", "--device=pixel_6", "--ram=2048")
	if !ok {
		t.Fatalf("create failed:\n%s", out)
	}
	mustContain(t, d.read("avdmanager_calls"), ";android-34;", "-d pixel_6")
	cfg, _ := os.ReadFile(filepath.Join(d.home, "avd", "Old.avd", "config.ini"))
	mustContain(t, string(cfg), "hw.ramSize=2048")
}

func TestCreateFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		images []string
		args   []string
		want   string
	}{
		{"no name", []string{"android-35/google_apis/ABI"}, nil, "Please give the new AVD a name"},
		{"bad name", []string{"android-35/google_apis/ABI"}, []string{"my/avd"}, "Please give the new AVD a name"},
		{"bad api", []string{"android-35/google_apis/ABI"}, []string{"X", "--api=abc"}, "Invalid --api=abc"},
		{"only 16k and playstore", []string{"android-36/google_apis_ps16k/ABI", "android-36/google_apis_playstore/ABI"}, []string{"X"}, `sdkmanager "system-images;android-35;google_apis;`},
		{"api not installed", []string{"android-35/google_apis/ABI"}, []string{"X", "--api=33"}, `sdkmanager "system-images;android-33;google_apis;`},
		{"avdmanager fails", []string{"android-35/google_apis/ABI"}, []string{"X", "--device=bogus"}, "avdmanager failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDevice(t, false)
			d.withSdk(tc.images...)
			out, ok := d.run(append([]string{"create"}, tc.args...)...)
			if ok {
				t.Fatalf("expected non-zero exit:\n%s", out)
			}
			mustContain(t, out, tc.want)
			if entries, _ := os.ReadDir(filepath.Join(d.home, "avd")); len(entries) > 0 && tc.name != "avdmanager fails" {
				t.Errorf("AVD dir not empty after failure: %v", entries)
			}
		})
	}
}

// fakeEmulator "boots" by registering its -port with the fake adb, or, with
// emulator_crash present, dies the way a broken system image does.
const fakeEmulator = `#!/bin/bash
D="$(dirname "$0")"
echo "$*" >> "$D/emulator_calls"
if [ -f "$D/emulator_crash" ]; then
  echo "PANIC: Missing emulator engine program for 'arm64' CPU."
  exit 1
fi
while [ $# -gt 0 ]; do
  [ "$1" = "-port" ] && port="$2"
  [ "$1" = "-avd" ] && avd="$2"
  shift
done
echo "$avd" > "$D/avd_emulator-$port"
printf 'List of devices attached\nemulator-%s\tdevice\n' "$port" > "$D/devices"
echo 1 > "$D/prop_sys.boot_completed"
sleep 5
`

func (d *device) withEmulator(avd string) {
	d.write("emulator", fakeEmulator)
	if err := os.Chmod(filepath.Join(d.dir, "emulator"), 0755); err != nil {
		d.t.Fatal(err)
	}
	dir := filepath.Join(d.home, "avd", avd+".avd")
	if err := os.MkdirAll(dir, 0755); err != nil {
		d.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.ini"), []byte("hw.ramSize=1536\n"), 0644); err != nil {
		d.t.Fatal(err)
	}
	d.write("packages", "com.google.android.apps.maps\n")
}

func TestStartLaunchesAndSlims(t *testing.T) {
	d := newDevice(t, false)
	d.withEmulator("Avdslim_Test_Pixel")

	out, ok := d.run("start", "avdslim_test_pixel") // any case, like on macOS
	if !ok {
		t.Fatalf("start failed:\n%s", out)
	}
	mustContain(t, out, "Boot complete", "Slimming complete")
	mustContain(t, d.read("emulator_calls"), "-avd Avdslim_Test_Pixel ", "-lowram", "-memory 1536", "-no-snapshot-load")
}

func TestStartReportsEmulatorCrash(t *testing.T) {
	d := newDevice(t, false)
	d.withEmulator("Avdslim_Test_Pixel")
	d.write("emulator_crash", "")

	out, ok := d.run("start", "Avdslim_Test_Pixel")
	if ok {
		t.Fatalf("start succeeded although the emulator crashed:\n%s", out)
	}
	mustContain(t, out, "exited during startup", "PANIC: Missing emulator engine")
}

func TestStartRejectsUnknownAvd(t *testing.T) {
	d := newDevice(t, false)
	d.withEmulator("Avdslim_Test_Pixel")
	out, ok := d.run("start", "Nope")
	if ok {
		t.Fatalf("start succeeded for an unknown AVD:\n%s", out)
	}
	mustContain(t, out, `AVD "Nope" not found`)
	if d.read("emulator_calls") != "" {
		t.Error("emulator launched for an unknown AVD")
	}
}

// oldSnapshot gives Avdslim_Test_Pixel a previous Golden Snapshot and returns its marker path.
func (d *device) oldSnapshot() string {
	snap := filepath.Join(d.home, "avd", "Avdslim_Test_Pixel.avd", "snapshots", "avdslim_clean")
	if err := os.MkdirAll(snap, 0755); err != nil {
		d.t.Fatal(err)
	}
	marker := filepath.Join(snap, "marker")
	if err := os.WriteFile(marker, []byte("old-snapshot\n"), 0644); err != nil {
		d.t.Fatal(err)
	}
	return marker
}

func TestBakeReplacesSnapshot(t *testing.T) {
	d := newDevice(t, false)
	d.withEmulator("Avdslim_Test_Pixel")
	marker := d.oldSnapshot()

	out, ok := d.run("bake", "Avdslim_Test_Pixel")
	if !ok {
		t.Fatalf("bake failed:\n%s", out)
	}
	mustContain(t, out, "Golden Snapshot Baked Successfully")
	if b, _ := os.ReadFile(marker); string(b) != "new-snapshot\n" {
		t.Errorf("snapshot marker = %q, want the new snapshot", b)
	}
	if _, err := os.Stat(filepath.Dir(marker) + ".avdslim-old"); err == nil {
		t.Error("previous snapshot copy left behind")
	}
}

// A failed bake must not cost the user the snapshot they already had.
func TestBakeFailureKeepsPreviousSnapshot(t *testing.T) {
	for _, tc := range []struct{ name, failSwitch, want string }{
		{"slim fails", "readonly", "Slimming failed"},
		{"save fails", "snapshot_fails", "Snapshot save failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDevice(t, false)
			d.withEmulator("Avdslim_Test_Pixel")
			marker := d.oldSnapshot()
			d.write(tc.failSwitch, "")

			out, ok := d.run("bake", "Avdslim_Test_Pixel")
			if ok {
				t.Fatalf("bake succeeded:\n%s", out)
			}
			mustContain(t, out, tc.want, "previous Golden Snapshot is unchanged")
			if b, _ := os.ReadFile(marker); string(b) != "old-snapshot\n" {
				t.Errorf("previous snapshot lost: marker = %q", b)
			}
		})
	}
}

// A guest whose framework crash-loops (the Android 16 bluetooth bug) still
// reports boot_completed; doctor and on must name it and point at repair.
func TestCrashLoopingGuestPointsAtRepair(t *testing.T) {
	d := newDevice(t, true)
	d.write("pm_broken", "")

	out, _ := d.run("doctor")
	mustContain(t, out, "Package manager not running", "avdslim repair")

	out, ok := d.run("on")
	if ok {
		t.Fatalf("on succeeded on a crash-looping guest:\n%s", out)
	}
	mustContain(t, out, "avdslim repair")
}

// Upgrading avdslim updates an old shim on the next run of any command.
func TestAnyCommandRefreshesOutdatedShim(t *testing.T) {
	d := newDevice(t, false)
	sdk := t.TempDir()
	emuDir := filepath.Join(sdk, "emulator")
	if err := os.MkdirAll(emuDir, 0755); err != nil {
		t.Fatal(err)
	}
	// A pre-1.0.13 shim: header and RAM=, but no feature markers.
	old := "#!/bin/bash\n# avdslim emulator shim — old\nDEFAULTS_FILE=x\nRAM=2048\nexec \"$DIR/emulator.real\" \"$@\"\n"
	if err := os.WriteFile(filepath.Join(emuDir, "emulator"), []byte(old), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(emuDir, "emulator.real"), []byte("REAL"), 0755); err != nil {
		t.Fatal(err)
	}
	d.env = append(d.env, "ANDROID_HOME="+sdk)

	out, _ := d.run("list")
	mustContain(t, out, "Updated the Android Studio shim")
	shimNow, _ := os.ReadFile(filepath.Join(emuDir, "emulator"))
	mustContain(t, string(shimNow), "avdslim_flag", "RAM=2048")

	out, _ = d.run("list")
	if strings.Contains(out, "Updated the Android Studio shim") {
		t.Error("refreshed an up-to-date shim again")
	}
	if out, _ := d.run("version"); strings.Contains(out, "shim") {
		t.Errorf("version touched the shim:\n%s", out)
	}
}

func TestRepair(t *testing.T) {
	d := newDevice(t, true)
	out, ok := d.run("repair")
	if !ok {
		t.Fatalf("repair failed:\n%s", out)
	}
	mustContain(t, out, "Nothing to repair")

	d.write("root_denied", "")
	out, ok = d.run("repair")
	if ok {
		t.Fatalf("repair succeeded without adb root:\n%s", out)
	}
	mustContain(t, out, "adb root not permitted")
}

func TestRestartPurgesAndRelaunches(t *testing.T) {
	d := newDevice(t, true)
	d.withEmulator("Avdslim_Test_Avd")
	avd := filepath.Join(d.home, "avd", "Avdslim_Test_Avd.avd")
	if err := os.MkdirAll(filepath.Join(avd, "snapshots", "avdslim_clean"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(avd, "hardware-qemu.ini"), []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	out, ok := d.run("restart")
	if !ok {
		t.Fatalf("restart failed:\n%s", out)
	}
	mustContain(t, out, "Purged stale", "avdslim bake Avdslim_Test_Avd", "Slimming complete")
	for _, purged := range []string{"snapshots", "hardware-qemu.ini"} {
		if _, err := os.Stat(filepath.Join(avd, purged)); err == nil {
			t.Errorf("%s not purged", purged)
		}
	}
	if !strings.Contains(d.read("emulator_calls"), "-avd Avdslim_Test_Avd") {
		t.Error("emulator not relaunched")
	}
}

func TestUnknownCommand(t *testing.T) {
	d := newDevice(t, false)
	out, ok := d.run("frobnicate")
	if ok {
		t.Fatal("expected non-zero exit")
	}
	mustContain(t, out, "Unknown command: frobnicate")
}
