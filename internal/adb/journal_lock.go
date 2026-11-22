package adb

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
)

// Keep the lock in the guest so a watcher and a CLI command, including ones
// using different adb clients, cannot overwrite each other's restore journal.
// The guest shell holds the descriptor until its adb connection closes. Keep
// the lock file itself in place so every invocation locks the same inode.
func (c *Client) lockJournal(serial string) (func(), error) {
	// Android's mksh closes its private fd 9 on exec. Give flock a duplicate
	// via stdout; the shell keeps the same open file description on fd 9.
	const script = "exec 9>>" + StateFilePath + ".lock || exit 1; " +
		"flock -xn 1 >&9 || { echo busy; exit 1; }; echo locked; read -r done"
	cmd := exec.Command(c.adbPath, "-s", serial, "shell", script)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	release := func() {
		fmt.Fprintln(in, "done")
		in.Close()
		cmd.Wait()
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "locked" {
		release()
		return nil, fmt.Errorf("cannot acquire the device restore journal lock (check adb, flock, or another running avdslim operation)")
	}
	return release, nil
}
