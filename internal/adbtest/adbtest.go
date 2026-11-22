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
//	enabled_<pkg>       user 0's PM enabled-setting integer (default 0)
//	manifest_disabled   packages whose manifest default is disabled
//	<ns>_<name>         settings values
//	state               the guest state JSON
//	calls               every invocation, one per line (appended)
//
// Failure switches (create the file to trigger):
//
//	pm_broken           `pm list packages` fails
//	enable_fails        packages (one per line) that `pm enable` rejects
//	disable_fails       packages that reject `pm disable-user`
//	flush_fails         the package-manager durability barrier fails
//	flush_unexpected    the barrier returns unexpected text but exits zero
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
pm_state() { cat "$D/enabled_$1" 2>/dev/null || echo 0; }
case "$1" in
  "if [ -e /data/local/tmp/avdslim_state.json "*|"tmp=\$(mktemp /data/local/tmp/avdslim_state.json."*|"exec 9>>/data/local/tmp/avdslim_state.json.lock "*)
    [[ "$1" == tmp=* && -f "$D/readonly" ]] && { echo "Read-only file system"; exit 1; }
    cd "$D" || exit 1
    sync() { :; } # journal writes in a local test do not need a host-wide sync
    if ! command -v flock >/dev/null; then # macOS has no host flock(1)
      flock() { mkdir held 2>/dev/null || return 1; trap 'rmdir held' EXIT; }
    fi
    journal=/data/local/tmp/avdslim_state.json
    eval "${1//"$journal"/state}"
    exit $? ;;
esac
case "$*" in
  "dumpsys package write && sync") barrier="Settings written." ;;
  "sleep 11 && pm trim-caches 0 && echo drained") barrier=drained ;;
  "sleep 11 && cmd package wait-for-background-handler --timeout 30000") barrier=Success ;;
esac
if [ -n "$barrier" ]; then
  [ -f "$D/flush_fails" ] && { echo "Failure"; exit 1; }
  [ -f "$D/flush_unexpected" ] && { echo "unrecognized command"; exit 0; }
  for f in "$D"/enabled_*; do
    [ ! -f "$f" ] || cp "$f" "$D/durable_${f##*/enabled_}" || exit 1
  done
  echo "$barrier"; exit 0
fi
case "$1 $2" in
  "settings get") cat "$D/$3_$4" 2>/dev/null || echo null ;;
  "settings put") printf '%s' "$5" > "$D/$3_$4" ;;
  "settings delete") rm -f "$D/$3_$4" ;;
  "pm list")
    [ -f "$D/pm_broken" ] && { echo "cmd: Can't find service: package"; exit 1; }
    while IFS= read -r pkg; do
      if [[ " $* " == *" -e "* ]]; then
        state=$(pm_state "$pkg")
        [ "$state" = 1 ] || { [ "$state" = 0 ] && ! grep -Fxq "$pkg" "$D/manifest_disabled" 2>/dev/null; } || continue
      fi
      echo "package:$pkg"
    done < <(if [ -f "$D/packages" ]; then cat "$D/packages"; else echo android; fi) ;;
  "service check")
    if [ -f "$D/pm_broken" ]; then echo "Service $3: not found"; else echo "Service $3: found"; fi ;;
  "dumpsys package")
    pkg="$3"
    grep -Fxq "$pkg" "$D/packages" 2>/dev/null || { echo "Unable to find package: $pkg"; exit 1; }
    echo "Packages:"
    echo "  Package [$pkg] (12345):"
    echo "    User 10: installed=true hidden=false enabled=2"
    echo "    User 0: installed=true hidden=false enabled=$(pm_state "$pkg")"
    echo "Hidden system packages:"
    echo "  Package [$pkg] (54321):"
    echo "    User 0: installed=true hidden=false enabled=1" ;;
  "pm enable"|"pm disable-user"|"pm default-state")
    pkg="$5"
    [ "$3 $4" = "--user 0" ] || { echo "Error: expected explicit user 0"; exit 1; }
    grep -Fxq "$pkg" "$D/packages" 2>/dev/null || { echo "Error: Unknown package: $pkg"; exit 1; }
    case "$2" in
      enable) state=1; label=enabled; fails=enable_fails ;;
      default-state) state=0; label=default; fails=enable_fails ;;
      disable-user) state=3; label=disabled-user; fails=disable_fails ;;
    esac
    grep -Fxq "$pkg" "$D/$fails" 2>/dev/null && { echo "Error changing state of $pkg"; exit 1; }
    printf '%s' "$state" > "$D/enabled_$pkg"
    echo "Package $pkg new state: $label" ;;
  "getprop ro.build.version.sdk") cat "$D/prop_$2" 2>/dev/null || echo 36 ;;
  "getprop "*) cat "$D/prop_$2" 2>/dev/null ;;
  "cat "*) cat "$D/state" 2>/dev/null ;;
  "test -f") [ -f "$D/state" ] && echo yes ;;
  "rm -f") rm -f "$D/state" ;;
esac
exit 0
`

// Install writes the fake adb into dir and returns its path.
func Install(dir string) (string, error) {
	p := filepath.Join(dir, "adb")
	return p, os.WriteFile(p, []byte(script), 0755)
}
