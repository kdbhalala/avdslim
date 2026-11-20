package host

import (
	"encoding/csv"
	"strconv"
	"strings"
)

// parseNetstatListenerPid returns the PID of the process listening on the TCP
// port in `netstat -ano -p TCP` output, or 0. A listener is recognised by its
// foreign address ending in ":0" (0.0.0.0:0, [::]:0), not by the localized
// LISTENING state word. Established connections, such as adb's own socket to
// the console port, have a real foreign port and never match.
func parseNetstatListenerPid(out, port string) int {
	if port == "" {
		return 0
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || !strings.HasPrefix(fields[0], "TCP") {
			continue
		}
		if !hasPortSuffix(fields[1], port) || !strings.HasSuffix(fields[2], ":0") {
			continue
		}
		pid, err := strconv.Atoi(fields[len(fields)-1])
		if err == nil && pid > 0 {
			return pid
		}
	}
	return 0
}

// hasPortSuffix reports whether addr ("127.0.0.1:5554", "[::]:5554") ends in
// exactly ":<port>".
func hasPortSuffix(addr, port string) bool {
	i := strings.LastIndex(addr, ":")
	return i >= 0 && addr[i+1:] == port
}

// parseTasklistKb reads the working set in KB from `tasklist /FO CSV /NH`
// output. The memory column is the last CSV field, e.g. "1,234,567 K" in
// English, "1.234.567 K" in German, "1 234 567 K" in French. Only digits are
// kept, so any thousands separator works. Returns 0 when no task matched.
func parseTasklistKb(out string) int {
	r := csv.NewReader(strings.NewReader(strings.TrimSpace(out)))
	r.FieldsPerRecord = -1
	rec, err := r.Read()
	if err != nil || len(rec) == 0 {
		return 0
	}
	var digits strings.Builder
	for _, ch := range rec[len(rec)-1] {
		if ch >= '0' && ch <= '9' {
			digits.WriteRune(ch)
		}
	}
	kb, err := strconv.Atoi(digits.String())
	if err != nil {
		return 0
	}
	return kb
}
