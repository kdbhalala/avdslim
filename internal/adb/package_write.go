package adb

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// pm enable/default-state acknowledges the in-memory change. On Android 14+
// PackageManagerService.scheduleWritePackageRestrictions posts the disk write
// to its background handler with WRITE_SETTINGS_DELAY (10 seconds). Queue a
// barrier on that same handler after the write is due, using the guest clock.
// Waiting for an empty queue immediately after pm returns can overtake it.
func (c *Client) flushPackageRestrictions(serial string) error {
	version, err := c.Exec("-s", serial, "shell", "getprop", "ro.build.version.sdk")
	sdk, parseErr := strconv.Atoi(strings.TrimSpace(version))
	if err != nil || parseErr != nil || sdk <= 0 {
		return fmt.Errorf("cannot determine Android version to confirm package persistence")
	}
	barrier, want := "sleep 11 && cmd package wait-for-background-handler --timeout 30000", "Success"
	switch {
	case sdk <= 32:
		// Older AOSP versions implement this as a synchronous settings write,
		// including user package restrictions. Later versions only print a dump.
		barrier, want = "dumpsys package write", "Settings written."
	case sdk == 33:
		// Android 13 moved the write out of dumpsys but still uses PM's main
		// handler. freeStorageAndNotify queues work there and waits for it.
		// A target of zero free bytes returns without touching any caches.
		barrier, want = "sleep 11 && pm trim-caches 0 && echo drained", "drained"
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, c.adbPath, "-s", serial, "shell", barrier).CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != want {
		return fmt.Errorf("could not confirm the package manager finished writing user 0 restrictions on Android API %d; keep the journal and retry", sdk)
	}
	return nil
}
