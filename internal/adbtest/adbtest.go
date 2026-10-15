// Package adbtest provides a fake adb for tests that need no emulator.
package adbtest

import (
	"os"
	"path/filepath"
)

// script emulates the adb calls avdslim makes, keeping all device state as
// files next to itself:
//
//	devices             `adb devices` output
//	avd_<serial>        `emu avd name` output
//	prop_<name>         getprop values
//	packages            installed packages, one per line
//	<ns>_<name>         settings values
//	state               the guest state JSON
//	calls               every invocation, one per line (appended)
//
// Failure switches (create the file to trigger):
//
//	pm_broken           `pm list packages` fails
//	enable_fails        packages (one per line) that `pm enable` rejects
//	readonly            writing the state file fails
//	snapshot_fails      `emu avd snapshot save` answers KO
//	root_denied         `adb root` refuses (Play Store image)
//
// `emu avd snapshot save NAME` creates $ANDROID_AVD_HOME/<avd>.avd/snapshots/NAME
// with a "marker" file reading "new-snapshot"; `emu kill` clears `devices`.
const script = `#!/bin/bash
D="$(dirname "$0")"
echo "$*" >> "$D/calls"
case "$1" in
  devices) cat "$D/devices" 2>/dev/null || printf "List of devices attached\n"; exit 0 ;;
  version) echo "Android Debug Bridge version 1.0.41 (fake)"; exit 0 ;;
esac
if [ "$1" = "-s" ] && [ "$3" = "emu" ]; then
  name=$(cat "$D/avd_$2" 2>/dev/null || echo "Test_AVD")
  case "$4 $5 $6" in
    "avd snapshot save")
      [ -f "$D/snapshot_fails" ] && { echo "KO: snapshot save failed"; exit 0; }
      mkdir -p "$ANDROID_AVD_HOME/$name.avd/snapshots/$7" && echo "new-snapshot" > "$ANDROID_AVD_HOME/$name.avd/snapshots/$7/marker"
      echo OK ;;
    "kill "*) rm -f "$D/devices"; echo OK ;;
    *) echo "$name" ;;
  esac
  exit 0
fi
if [ "$1" = "-s" ] && [ "$3" = "root" ]; then
  [ -f "$D/root_denied" ] && echo "adbd cannot run as root in production builds"
  exit 0
fi
if [ "$1" = "-s" ] && [ "$3" = "logcat" ]; then
  [ "$4" = "-d" ] && cat "$D/crash" 2>/dev/null
  # Clearing the crash log follows a repair: the framework then finishes booting.
  [ "$6" = "-c" ] && [ -f "$D/crash" ] && { rm -f "$D/crash"; echo 1 > "$D/prop_sys.boot_completed"; }
  exit 0
fi
if [ "$1" = "-s" ] && [ "$3" = "get-state" ]; then
  grep -q "^$2[[:space:]]*device" "$D/devices" 2>/dev/null && { echo device; exit 0; }
  echo "error: device '$2' not found" >&2
  exit 1
fi
[ "$3" = "shell" ] || exit 0
shift 3 # drop: -s SERIAL shell
case "$1 $2" in
  "settings get") cat "$D/$3_$4" 2>/dev/null || echo null ;;
  "settings put") printf '%s' "$5" > "$D/$3_$4" ;;
  "settings delete") rm -f "$D/$3_$4" ;;
  "pm list")
    [ -f "$D/pm_broken" ] && { echo "cmd: Can't find service: package"; exit 1; }
    if [ -f "$D/packages" ]; then sed 's/^/package:/' "$D/packages"; else echo "package:android"; fi ;;
  "service check")
    if [ -f "$D/pm_broken" ]; then echo "Service $3: not found"; else echo "Service $3: found"; fi ;;
  "pm enable")
    grep -qx "$3" "$D/enable_fails" 2>/dev/null && { echo "Error: Unknown package: $3"; exit 1; }
    echo "Package $3 new state: enabled" ;;
  "pm disable-user") echo "Package $5 new state: disabled-user" ;;
  "getprop "*) cat "$D/prop_$2" 2>/dev/null ;;
  "cat "*) cat "$D/state" 2>/dev/null ;;
  "ls "*) [ -f "$D/state" ] && echo "$2" ;;
  "rm -f") rm -f "$D/state" ;;
  "echo "*)
    [ -f "$D/readonly" ] && { echo "/system/bin/sh: can't create $4: Read-only file system"; exit 1; }
    eval "printf '%s' $2" > "$D/state" ;; # parse the quoting like the device shell
esac
exit 0
`

// Install writes the fake adb into dir and returns its path.
func Install(dir string) (string, error) {
	p := filepath.Join(dir, "adb")
	return p, os.WriteFile(p, []byte(script), 0755)
}
