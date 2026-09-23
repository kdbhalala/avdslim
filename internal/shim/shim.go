package shim

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/kdbhalala/avdslim/internal/config"
)

const shimHeader = "# avdslim emulator shim"

func GetEmulatorBinaryPaths() (emuPath, realPath string, err error) {
	sdkDir := config.GetAndroidSdkDir()
	if sdkDir == "" {
		return "", "", fmt.Errorf("android SDK directory not found ($ANDROID_HOME or default SDK path)")
	}

	emuDir := filepath.Join(sdkDir, "emulator")
	binaryName := "emulator"
	realName := "emulator.real"
	if runtime.GOOS == "windows" {
		binaryName = "emulator.exe"
		realName = "emulator.real.exe"
	}

	emuPath = filepath.Join(emuDir, binaryName)
	realPath = filepath.Join(emuDir, realName)

	if _, err := os.Stat(emuDir); os.IsNotExist(err) {
		return "", "", fmt.Errorf("emulator directory %s does not exist", emuDir)
	}
	return emuPath, realPath, nil
}

func IsShimInstalled() (bool, error) {
	emuPath, realPath, err := GetEmulatorBinaryPaths()
	if err != nil {
		return false, err
	}

	if _, err := os.Stat(realPath); err == nil {
		return hasShimHeader(emuPath), nil
	}
	return false, nil
}

// IsShimOverwritten reports whether an emulator update replaced the shim:
// emulator.real is still there but emulator is no longer our wrapper.
func IsShimOverwritten() bool {
	emuPath, realPath, err := GetEmulatorBinaryPaths()
	if err != nil {
		return false
	}
	if _, err := os.Stat(realPath); err != nil {
		return false
	}
	if _, err := os.Stat(emuPath); err != nil {
		return false
	}
	return !hasShimHeader(emuPath)
}

// IsShimOutdated reports whether an installed shim predates the defaults file
// (avdslim <= 1.0.5) or the avdslim.* feature markers (avdslim <= 1.0.12), so it
// ignores --ram or `avdslim enable audio|camera` until reinstalled. With
// ANDROID_AVD_HOME set, shims from <= 1.0.13 also look for AVDs in the wrong place.
func IsShimOutdated() bool {
	emuPath, _, err := GetEmulatorBinaryPaths()
	if err != nil || !hasShimHeader(emuPath) {
		return false
	}
	data, err := os.ReadFile(emuPath) // our script, small
	if err != nil {
		return false
	}
	script := string(data)
	return !strings.Contains(script, "DEFAULTS_FILE=") || !strings.Contains(script, "avdslim_flag") ||
		(os.Getenv("ANDROID_AVD_HOME") != "" && !strings.Contains(script, "ANDROID_AVD_HOME"))
}

// RefreshIfOutdated rewrites an installed but outdated shim script with the
// current one, keeping its --ram. It only touches our script, never
// emulator.real, so it is safe to run after every avdslim upgrade.
func RefreshIfOutdated() (bool, error) {
	if installed, _ := IsShimInstalled(); !installed || !IsShimOutdated() {
		return false, nil
	}
	emuPath, realPath, err := GetEmulatorBinaryPaths()
	if err != nil {
		return false, err
	}
	data, err := os.ReadFile(emuPath)
	if err != nil {
		return false, err
	}
	ram := 1536
	if m := regexp.MustCompile(`(?m)^RAM=(\d+)`).FindSubmatch(data); m != nil {
		ram, _ = strconv.Atoi(string(m[1]))
	} else if m := regexp.MustCompile(`"-memory" "?(\d+)`).FindSubmatch(data); m != nil {
		ram, _ = strconv.Atoi(string(m[1])) // <= 1.0.5 shims baked it into the args
	}
	if err := writeShimScript(emuPath, realPath, ram); err != nil {
		return false, err
	}
	return true, nil
}

// hasShimHeader checks only the first bytes; the real emulator binary is large.
func hasShimHeader(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 256)
	n, _ := f.Read(buf)
	return strings.Contains(string(buf[:n]), shimHeader)
}

func InstallShim(defaultRam int) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("install-shim is not supported on Windows: Android Studio runs emulator.exe directly, " +
			"so a script wrapper cannot replace it. Use `avdslim start` and `avdslim tune-avd` instead")
	}

	emuPath, realPath, err := GetEmulatorBinaryPaths()
	if err != nil {
		return err
	}

	if defaultRam <= 0 {
		defaultRam = 1536
	}

	// Check if already installed
	alreadyInstalled, _ := IsShimInstalled()
	if alreadyInstalled {
		// Update existing shim with new RAM if changed
		return writeShimScript(emuPath, realPath, defaultRam)
	}

	// Verify original emulator exists
	if _, err := os.Stat(emuPath); os.IsNotExist(err) {
		return fmt.Errorf("emulator binary not found at %s", emuPath)
	}

	if hasShimHeader(emuPath) {
		return writeShimScript(emuPath, realPath, defaultRam)
	}

	// Move original emulator to emulator.real
	if err := os.Rename(emuPath, realPath); err != nil {
		return fmt.Errorf("failed to backup original emulator: %w", err)
	}

	// Write shim script
	if err := writeShimScript(emuPath, realPath, defaultRam); err != nil {
		// Rollback
		_ = os.Rename(realPath, emuPath)
		return fmt.Errorf("failed to install shim: %w", err)
	}

	return nil
}

func UninstallShim() error {
	emuPath, realPath, err := GetEmulatorBinaryPaths()
	if err != nil {
		return err
	}

	if _, err := os.Stat(realPath); os.IsNotExist(err) {
		return fmt.Errorf("shim is not installed (original %s not found)", realPath)
	}

	// An SDK update replaced the shim: emulator is already the new stock
	// binary and emulator.real is the old one. Swapping would downgrade.
	if IsShimOverwritten() {
		return fmt.Errorf("an emulator update already replaced the shim, so %s is stock and newer than %s; "+
			"nothing to restore. %s is a leftover copy of the old emulator you can delete", emuPath, realPath, realPath)
	}

	// Rename over the shim in one step, so a failure never leaves no emulator.
	if err := os.Rename(realPath, emuPath); err != nil {
		return fmt.Errorf("failed to restore original emulator: %w", err)
	}

	return nil
}

func writeShimScript(emuPath, realPath string, defaultRam int) error {
	// ponytail: --ram in the defaults file wins at launch, so changing it needs no reinstall.
	script := fmt.Sprintf(`#!/bin/bash
%s — transparently injects low-memory flags
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REAL_EMU="$DIR/emulator.real"

if [ ! -f "$REAL_EMU" ]; then
    echo "❌ [avdslim] Error: Real emulator binary not found at $REAL_EMU" >&2
    exit 1
fi

HAS_MEM=0
HAS_LOWRAM=0
NO_LOWRAM=0
NO_SLIM=0
HEADLESS=0
HAS_SNAP=0
AVD_NAME=""

DEFAULTS_FILE=%s
RAM=%d
if [ -f "$DEFAULTS_FILE" ]; then
    FILE_RAM=$(sed 's/#.*//' "$DEFAULTS_FILE" | grep -oE -- '--ram=[0-9]+' | tail -n 1 | cut -d= -f2)
    if [ -n "$FILE_RAM" ]; then RAM="$FILE_RAM"; fi
fi

PREV=""
for arg in "$@"; do
    if [ "$arg" = "-memory" ]; then HAS_MEM=1; fi
    if [ "$arg" = "-lowram" ]; then HAS_LOWRAM=1; fi
    if [ "$arg" = "--no-lowram" ] || [ "$arg" = "-no-lowram" ]; then NO_LOWRAM=1; fi
    if [ "$arg" = "--no-slim" ] || [ "$arg" = "-no-slim" ]; then NO_SLIM=1; fi
    if [ "$arg" = "--headless" ] || [ "$arg" = "-no-window" ]; then HEADLESS=1; fi
    if [ "$arg" = "-snapshot" ] || [ "$arg" = "-no-snapshot" ] || [ "$arg" = "--cold" ]; then HAS_SNAP=1; fi
    if [ "$PREV" = "-avd" ]; then AVD_NAME="$arg"; fi
    PREV="$arg"
done

# avdslim_flag reads an avdslim.* marker from the AVD's config.ini; "yes" means
# the user re-enabled that feature with `+"`avdslim enable <feature>`"+`.
AVD_HOME="${ANDROID_AVD_HOME:-$HOME/.android/avd}"
CFG="$AVD_HOME/${AVD_NAME}.avd/config.ini"
avdslim_flag() {
    [ -n "$AVD_NAME" ] && [ -f "$CFG" ] || return 0
    sed -n "s/^[[:space:]]*$1[[:space:]]*=[[:space:]]*//p" "$CFG" | tr -d ' \r' | tail -n 1
}

EXTRA=()
if [ "$NO_SLIM" -ne 1 ]; then
    if [ "$HAS_MEM" -eq 0 ]; then
        EXTRA+=("-memory" "$RAM")
    fi
    if [ "$HAS_LOWRAM" -eq 0 ] && [ "$NO_LOWRAM" -eq 0 ]; then
        EXTRA+=("-lowram")
    fi
    if [ "$HEADLESS" -eq 1 ]; then
        EXTRA+=("-no-window")
    fi
    if [ "$HAS_SNAP" -eq 0 ] && [ -n "$AVD_NAME" ]; then
        SNAP_DIR="$AVD_HOME/${AVD_NAME}.avd/snapshots/avdslim_clean"
        if [ -d "$SNAP_DIR" ]; then
            EXTRA+=("-snapshot" "avdslim_clean" "-no-snapshot-save")
        fi
    fi
    if [ "$(avdslim_flag avdslim.audio)" != "yes" ]; then
        EXTRA+=("-no-audio")
    fi
    if [ "$(avdslim_flag avdslim.camera)" != "yes" ]; then
        EXTRA+=("-camera-back" "none" "-camera-front" "none")
    fi
fi

exec "$REAL_EMU" "${EXTRA[@]}" "$@"
`, shimHeader, shellQuote(config.DefaultsFilePath()), defaultRam)

	if err := os.WriteFile(emuPath, []byte(script), 0755); err != nil {
		return err
	}
	return os.Chmod(emuPath, 0755)
}

// shellQuote single-quotes s for bash.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
