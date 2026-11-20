package host

import "testing"

// netstat -ano -p TCP output. Headers and the LISTENING/ABHÖREN/ÉCOUTE state
// words are localized, so the parser must not depend on them.
func TestParseNetstatListenerPid(t *testing.T) {
	tests := []struct {
		name     string
		out      string
		port     string
		expected int
	}{
		{
			name: "English listener",
			out: `
Active Connections

  Proto  Local Address          Foreign Address        State           PID
  TCP    0.0.0.0:135            0.0.0.0:0              LISTENING       1036
  TCP    127.0.0.1:5554         0.0.0.0:0              LISTENING       4242
  TCP    127.0.0.1:5554         127.0.0.1:61000        ESTABLISHED     9999
`,
			port:     "5554",
			expected: 4242,
		},
		{
			name: "German listener (ABHÖREN)",
			out: `
Aktive Verbindungen

  Proto  Lokale Adresse         Remoteadresse          Status          PID
  TCP    127.0.0.1:5554         0.0.0.0:0              ABHÖREN         4242
`,
			port:     "5554",
			expected: 4242,
		},
		{
			name: "French listener (ÉCOUTE)",
			out: `
Connexions actives

  Proto  Adresse locale        Adresse distante       État            PID
  TCP    127.0.0.1:5554         0.0.0.0:0              ÉCOUTE          4242
`,
			port:     "5554",
			expected: 4242,
		},
		{
			name: "IPv6 listener",
			out: `
  TCP    [::]:5554              [::]:0                 LISTENING       5151
  TCP    [::1]:5554             [::]:0                 LISTENING       5151
`,
			port:     "5554",
			expected: 5151,
		},
		{
			name: "adb ESTABLISHED connection to the console port is ignored",
			out: `
  TCP    127.0.0.1:50123        127.0.0.1:5554         ESTABLISHED     7777
`,
			port:     "5554",
			expected: 0,
		},
		{
			name: "ESTABLISHED row on the emulator side is ignored",
			out: `
  TCP    127.0.0.1:5554         127.0.0.1:50123        ESTABLISHED     4242
`,
			port:     "5554",
			expected: 0,
		},
		{
			name: "port must match exactly, not as a suffix",
			out: `
  TCP    127.0.0.1:55540        0.0.0.0:0              LISTENING       3333
`,
			port:     "5554",
			expected: 0,
		},
		{
			name: "listener on another port is ignored",
			out: `
  TCP    127.0.0.1:5556         0.0.0.0:0              LISTENING       3333
`,
			port:     "5554",
			expected: 0,
		},
		{
			name:     "empty output",
			out:      "",
			port:     "5554",
			expected: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseNetstatListenerPid(tt.out, tt.port); got != tt.expected {
				t.Errorf("parseNetstatListenerPid() = %d, expected %d", got, tt.expected)
			}
		})
	}
}

// tasklist /FI "PID eq N" /FO CSV /NH prints the working set as the last CSV
// field, with locale-specific thousands separators.
func TestParseTasklistKb(t *testing.T) {
	tests := []struct {
		name     string
		out      string
		expected int
	}{
		{
			name:     "English comma separators",
			out:      `"qemu-system-x86_64.exe","4242","Console","1","1,234,567 K"`,
			expected: 1234567,
		},
		{
			name:     "German dot separators",
			out:      `"qemu-system-x86_64.exe","4242","Konsole","1","1.234.567 K"`,
			expected: 1234567,
		},
		{
			name:     "French space separators (narrow no-break space)",
			out:      "\"qemu-system-x86_64.exe\",\"4242\",\"Console\",\"1\",\"1 234 567 K\"",
			expected: 1234567,
		},
		{
			name:     "space separators (no-break space)",
			out:      "\"qemu-system-x86_64.exe\",\"4242\",\"Console\",\"1\",\"1 234 567 K\"",
			expected: 1234567,
		},
		{
			name:     "no matching task",
			out:      "INFO: No tasks are running which match the specified criteria.",
			expected: 0,
		},
		{
			name:     "empty output",
			out:      "",
			expected: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseTasklistKb(tt.out); got != tt.expected {
				t.Errorf("parseTasklistKb() = %d, expected %d", got, tt.expected)
			}
		})
	}
}
