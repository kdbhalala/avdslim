package adb

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func skipAllSettings() map[string]bool {
	skip := make(map[string]bool)
	for _, group := range SkipGroups {
		skip[group] = true
	}
	return skip
}

func TestRestorePreservesPackageOverrides(t *testing.T) {
	c, dir := newFake(t)
	const maps, youtube = "com.google.android.apps.maps", "com.google.android.youtube"
	originals := map[string]string{
		maps: "0", youtube: "1", "com.google.android.gm": "2",
		"com.android.gallery3d": "3", "com.android.music": "4", "com.google.android.videos": "0",
	}
	var packages []string
	for pkg, state := range originals {
		packages = append(packages, pkg)
		write(t, dir, "enabled_"+pkg, state)
	}
	write(t, dir, "packages", strings.Join(packages, "\n")+"\n")
	write(t, dir, "manifest_disabled", "com.google.android.videos\n")
	for i := 0; i < 2; i++ {
		if _, err := c.Slim("e", false, nil, skipAllSettings()); err != nil {
			t.Fatal(err)
		}
		var marker struct {
			Version int            `json:"version"`
			States  map[string]int `json:"package_states"`
		}
		if err := json.Unmarshal([]byte(read(t, dir, "state")), &marker); err != nil {
			t.Fatal(err)
		}
		if marker.Version != 1 || !reflect.DeepEqual(marker.States, map[string]int{maps: 0, youtube: 1}) {
			t.Fatalf("on #%d recorded %+v", i+1, marker)
		}
		for pkg, want := range originals {
			if pkg == maps || pkg == youtube {
				want = "3"
			}
			if got := read(t, dir, "enabled_"+pkg); got != want {
				t.Fatalf("after on #%d: %s = %s, want %s", i+1, pkg, got, want)
			}
		}
	}
	if _, err := c.Restore("e"); err != nil {
		t.Fatal(err)
	}
	for pkg, want := range originals {
		if got := read(t, dir, "enabled_"+pkg); got != want {
			t.Errorf("after off: %s = %s, want %s", pkg, got, want)
		}
	}
}

func TestEmptyOrMissingRecordDoesNotRestoreUnownedState(t *testing.T) {
	for _, marker := range []string{"", `{"disabled_packages":null}`, `{"disabled_packages":[]}`, `{"disabled_packages":null,"settings":{}}`} {
		t.Run(marker, func(t *testing.T) {
			c, dir := newFake(t)
			write(t, dir, "packages", "com.google.android.youtube\n")
			write(t, dir, "enabled_com.google.android.youtube", "3")
			write(t, dir, "global_window_animation_scale", "0.61")
			if marker != "" {
				write(t, dir, "state", marker)
			}
			if _, err := c.Restore("e"); err != nil {
				t.Fatal(err)
			}
			if got := read(t, dir, "enabled_com.google.android.youtube"); got != "3" {
				t.Errorf("unowned package became %q", got)
			}
			for _, mutation := range []string{"shell pm enable ", "shell pm default-state ", "shell settings put ", "shell settings delete ", "shell cmd bluetooth_manager "} {
				if strings.Contains(read(t, dir, "calls"), mutation) {
					t.Errorf("empty record ran %q", mutation)
				}
			}
			if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
				t.Errorf("off left a completed empty marker: %v", err)
			}
		})
	}
}

func TestZeroMatchingPackagesProduceAnEmptyArray(t *testing.T) {
	c, dir := newFake(t)
	write(t, dir, "packages", "android\n")
	if _, err := c.Slim("e", false, nil, skipAllSettings()); err != nil {
		t.Fatal(err)
	}
	var marker map[string]json.RawMessage
	if err := json.Unmarshal([]byte(read(t, dir, "state")), &marker); err != nil {
		t.Fatal(err)
	}
	if got := string(marker["disabled_packages"]); got != "[]" {
		t.Errorf("disabled_packages = %q, want []", got)
	}
}

func TestLegacyRestoreOnlyReenablesNamedDisabledUserPackages(t *testing.T) {
	c, dir := newFake(t)
	const maps, youtube = "com.google.android.apps.maps", "com.google.android.youtube"
	write(t, dir, "packages", maps+"\n"+youtube+"\ncom.google.android.gm\n")
	write(t, dir, "enabled_"+maps, "3")
	write(t, dir, "enabled_"+youtube, "2")
	write(t, dir, "enabled_com.google.android.gm", "3")
	write(t, dir, "state", `{"disabled_packages":["com.google.android.apps.maps","com.google.android.youtube"],"settings":{}}`)
	if _, err := c.Restore("e"); err != nil {
		t.Fatal(err)
	}
	for pkg, want := range map[string]string{maps: "1", youtube: "2", "com.google.android.gm": "3"} {
		if got := read(t, dir, "enabled_"+pkg); got != want {
			t.Errorf("%s = %s, want %s", pkg, got, want)
		}
	}
}

func TestReslimPreservesLegacySettingsRestore(t *testing.T) {
	c, dir := newFake(t)
	const pkg = "com.google.android.apps.maps"
	write(t, dir, "packages", pkg+"\n")
	write(t, dir, "enabled_"+pkg, "3")
	write(t, dir, "state", `{"disabled_packages":["com.google.android.apps.maps"]}`)
	write(t, dir, "global_auto_sync", "0")
	if _, err := c.Slim("e", false, nil, skipAllSettings()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Restore("e"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "global_auto_sync"); got != "1" {
		t.Errorf("re-slim lost the pre-1.0.6 settings fallback, auto_sync = %q", got)
	}
}

func TestReslimSnapshotsSettingsAfterAnEmptyLegacyRecord(t *testing.T) {
	c, dir := newFake(t)
	write(t, dir, "state", `{"disabled_packages":[]}`)
	write(t, dir, "global_auto_sync", "1")
	if _, err := c.Slim("e", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "global_auto_sync"); got != "0" {
		t.Fatalf("on left auto_sync = %q, want 0", got)
	}
	if _, err := c.Restore("e"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "global_auto_sync"); got != "1" {
		t.Errorf("off left auto_sync = %q, want the recorded value 1", got)
	}
}

func TestCorruptMarkerNeverMutatesPackagesOrSettings(t *testing.T) {
	for _, marker := range []string{
		`{`, `{"settings":{}}`, `{"version":99,"disabled_packages":[],"settings":{}}`,
		`{"version":1,"disabled_packages":["com.example.app"],"package_states":{}}`,
		`{"version":1,"disabled_packages":["com.example.app"],"package_states":{"com.example.app":2}}`,
	} {
		t.Run(marker, func(t *testing.T) {
			c, dir := newFake(t)
			write(t, dir, "state", marker)
			if _, err := c.Restore("e"); err == nil {
				t.Error("off succeeded with an invalid record")
			}
			if got := read(t, dir, "state"); got != marker {
				t.Errorf("original marker changed: %q", got)
			}
			for _, mutation := range []string{"shell pm enable ", "shell pm default-state ", "shell settings put ", "shell settings delete "} {
				if strings.Contains(read(t, dir, "calls"), mutation) {
					t.Errorf("invalid record ran %q", mutation)
				}
			}
		})
	}
}

// Model the window after PM acknowledges a change but before a cold boot
// would see it. Even an already-restored package needs the barrier on retry.
func TestRestoreWaitsForDurablePackageStateBeforeForgettingIt(t *testing.T) {
	c, dir := newFake(t)
	const pkg = "com.google.android.apps.maps"
	write(t, dir, "packages", pkg+"\n")
	write(t, dir, "enabled_"+pkg, "0")
	if _, err := c.Slim("e", false, nil, skipAllSettings()); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "durable_"+pkg, "3") // the slimmed state survived a boot
	write(t, dir, "flush_fails", "")
	if _, err := c.Restore("e"); err == nil {
		t.Fatal("off succeeded without persisting the restored PM state")
	}
	if !contains(readState(t, dir).DisabledPackages, pkg) {
		t.Fatal("off discarded the package before confirming persistence")
	}
	if got := read(t, dir, "durable_"+pkg); got != "3" {
		t.Fatalf("failed flush changed durable state to %q", got)
	}
	if err := os.Remove(filepath.Join(dir, "flush_fails")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Restore("e"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "durable_"+pkg); got != "0" {
		t.Errorf("cold boot after successful off would restore %q, want default", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Errorf("successful retry left the marker behind: %v", err)
	}
}

func TestFailedDisableIsRecoverable(t *testing.T) {
	c, dir := newFake(t)
	const pkg = "com.google.android.apps.maps"
	write(t, dir, "packages", pkg+"\n")
	write(t, dir, "enabled_"+pkg, "1")
	write(t, dir, "disable_fails", pkg+"\n")
	if _, err := c.Slim("e", false, nil, skipAllSettings()); err == nil {
		t.Fatal("disable failure reported as a successful slim")
	}
	if !contains(readState(t, dir).DisabledPackages, pkg) {
		t.Fatal("missing write-ahead record after failed disable")
	}
	if err := os.Remove(filepath.Join(dir, "disable_fails")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Slim("e", false, nil, skipAllSettings()); err == nil {
		t.Fatal("second on silently accepted an incomplete slim")
	}
	if _, err := c.Restore("e"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "enabled_"+pkg); got != "1" {
		t.Errorf("restore after interrupted slim changed the initial override to %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
		t.Errorf("off left the recovered marker behind: %v", err)
	}
}

func TestSlimRetainsWriteAheadJournalOnFlushFailure(t *testing.T) {
	c, dir := newFake(t)
	const pkg = "com.google.android.youtube"
	write(t, dir, "packages", pkg+"\n")
	write(t, dir, "enabled_"+pkg, "0")
	write(t, dir, "flush_fails", "")
	if _, err := c.Slim("e", false, nil, skipAllSettings()); err == nil {
		t.Fatal("on succeeded without persisting package state")
	}
	if !readState(t, dir).Applying {
		t.Fatal("on lost the in-progress record before the PM write finished")
	}
	if err := os.Remove(filepath.Join(dir, "flush_fails")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Restore("e"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "durable_"+pkg); got != "0" {
		t.Errorf("off after failed flush left durable override %q", got)
	}
}

func TestReslimAndRestoreRespectLaterPackageChanges(t *testing.T) {
	c, dir := newFake(t)
	const pkg = "com.google.android.apps.maps"
	write(t, dir, "packages", pkg+"\n")
	if _, err := c.Slim("e", false, nil, skipAllSettings()); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "enabled_"+pkg, "2") // a separate owner explicitly disabled it
	write(t, dir, "calls", "")
	if _, err := c.Slim("e", false, nil, skipAllSettings()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Restore("e"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "enabled_"+pkg); got != "2" {
		t.Errorf("later user override changed to %q", got)
	}
	for _, command := range []string{"enable", "disable-user", "default-state"} {
		if strings.Contains(read(t, dir, "calls"), "shell pm "+command+" --user 0 "+pkg) {
			t.Errorf("later user override was overwritten with pm %s", command)
		}
	}
}

func TestMutationsCannotInterleaveWithAnotherJournalOwner(t *testing.T) {
	c, dir := newFake(t)
	release, err := c.lockJournal("e")
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer release()
		if _, err := c.Slim("e", false, nil, skipAllSettings()); err == nil {
			t.Error("on acquired an already-held guest lock")
		}
		if _, err := c.Restore("e"); err == nil {
			t.Error("off acquired an already-held guest lock")
		}
		if _, err := c.Enable("e", "maps"); err == nil {
			t.Error("enable acquired an already-held guest lock")
		}
		if _, err := os.Stat(filepath.Join(dir, "state")); !os.IsNotExist(err) {
			t.Errorf("a competing operation wrote the guest marker: %v", err)
		}
	}()
	if _, err := c.Restore("e"); err != nil {
		t.Fatalf("journal stayed locked after release: %v", err)
	}
}

func TestPackagePersistenceBarrierMustMatchTheGuestAPIAndAcknowledge(t *testing.T) {
	for _, tc := range []struct{ api, command string }{
		{"32", "dumpsys package write"},
		{"33", "sleep 11 && pm trim-caches 0 && echo drained"},
		{"34", "sleep 11 && cmd package wait-for-background-handler --timeout 30000"},
	} {
		t.Run(tc.api, func(t *testing.T) {
			c, dir := newFake(t)
			write(t, dir, "prop_ro.build.version.sdk", tc.api)
			if err := c.flushPackageRestrictions("e"); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(read(t, dir, "calls"), "shell "+tc.command+"\n") {
				t.Fatalf("API %s did not use %q", tc.api, tc.command)
			}
			write(t, dir, "flush_unexpected", "")
			if err := c.flushPackageRestrictions("e"); err == nil {
				t.Error("barrier accepted unrelated output with exit status 0")
			}
		})
	}
}
