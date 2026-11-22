package adb

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	packageAbsent       = -1
	packageDefault      = 0
	packageEnabled      = 1
	packageDisabledUser = 3
)

var packageName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)*$`)

func (s SlimState) validatePackageState() error {
	if s.Version != 0 && s.Version != 1 {
		return fmt.Errorf("unsupported state version %d", s.Version)
	}
	if s.Version == 0 && (s.PackageStates != nil || s.Applying || s.LegacySettings) {
		return errors.New("unversioned state with versioned package metadata")
	}
	if s.LegacySettings && s.Settings != nil {
		return errors.New("conflicting legacy and recorded settings")
	}
	if s.Version == 1 && (s.DisabledPackages == nil || s.PackageStates == nil || len(s.PackageStates) != len(s.DisabledPackages)) {
		return errors.New("incomplete package ownership record")
	}
	seen := make(map[string]bool)
	for _, pkg := range s.DisabledPackages {
		if !packageName.MatchString(pkg) || seen[pkg] {
			return fmt.Errorf("invalid or duplicate recorded package %q", pkg)
		}
		seen[pkg] = true
		if s.Version == 1 {
			original, ok := s.PackageStates[pkg]
			if !ok || (original != nil && *original != packageDefault && *original != packageEnabled) {
				return fmt.Errorf("invalid original override for %q", pkg)
			}
		}
	}
	return nil
}

// Upgrade in memory. Keep legacy ownership limited to the recorded names.
func (s *SlimState) initPackageStates() {
	if s.Version == 0 && s.Settings == nil && len(s.DisabledPackages) > 0 {
		s.LegacySettings = true
	}
	if s.PackageStates == nil {
		s.PackageStates = make(map[string]*int)
		for _, pkg := range s.DisabledPackages {
			s.PackageStates[pkg] = nil
		}
	}
	if s.DisabledPackages == nil {
		s.DisabledPackages = []string{}
	}
	s.Version = 1
}

func (s *SlimState) forgetPackage(pkg string) {
	delete(s.PackageStates, pkg)
	s.DisabledPackages = slices.DeleteFunc(s.DisabledPackages, func(p string) bool { return p == pkg })
}

// ApplicationInfo.enabled also accounts for the manifest. A manifest-disabled
// app may have override 0, just like an enabled app, so check both when slimming.
func (c *Client) listedPackages(serial string, onlyEnabled bool) (map[string]bool, error) {
	args := []string{"-s", serial, "shell", "pm", "list", "packages"}
	if onlyEnabled {
		args = append(args, "-e")
	}
	out, err := c.Exec(append(args, "--user", "0")...)
	if strings.Contains(out, "Can't find service: package") {
		return nil, errors.New("the guest's package manager is not running; if its framework is crash-looping, use `avdslim repair`")
	}
	if err != nil {
		return nil, fmt.Errorf("cannot list guest packages: %v: %s", err, strings.TrimSpace(out))
	}
	packages := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pkg, ok := strings.CutPrefix(line, "package:")
		if !ok || !packageName.MatchString(pkg) {
			return nil, fmt.Errorf("unexpected package list entry %q", line)
		}
		packages[pkg] = true
	}
	if !onlyEnabled && len(packages) == 0 {
		return nil, errors.New("cannot list guest packages (is the emulator fully booted?)")
	}
	return packages, nil
}

func (c *Client) packageOverride(serial, pkg string) (int, error) {
	out, err := c.Exec("-s", serial, "shell", "dumpsys", "package", pkg)
	if strings.TrimSpace(out) == "Unable to find package: "+pkg {
		return packageAbsent, nil
	}
	if err != nil {
		return 0, fmt.Errorf("cannot inspect %s: %w", pkg, err)
	}
	// The dump can include a second, inactive copy under "Hidden system packages:".
	// Only the matching package in the active Packages section describes this user.
	inPackages, match := false, false
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if line[0] != ' ' && line[0] != '\t' {
			inPackages = trimmed == "Packages:"
			match = false
			continue
		}
		if !inPackages {
			continue
		}
		if strings.HasPrefix(trimmed, "Package [") {
			match = strings.HasPrefix(trimmed, "Package ["+pkg+"] (")
		}
		if !match || !strings.HasPrefix(trimmed, "User 0:") {
			continue
		}
		fields := strings.Fields(trimmed)
		if slices.Contains(fields, "installed=false") {
			return packageAbsent, nil
		}
		if slices.Contains(fields, "installed=true") {
			for _, field := range fields {
				if value, ok := strings.CutPrefix(field, "enabled="); ok {
					if n, err := strconv.Atoi(value); err == nil && n >= 0 && n <= 4 {
						return n, nil
					}
				}
			}
		}
		break
	}
	return 0, fmt.Errorf("cannot determine user 0's package override for %s", pkg)
}

func (c *Client) setPackageOverride(serial, pkg string, override int) error {
	command, label := "default-state", "default"
	switch override {
	case packageEnabled:
		command, label = "enable", "enabled"
	case packageDisabledUser:
		command, label = "disable-user", "disabled-user"
	case packageDefault:
	default:
		return fmt.Errorf("invalid target override %d for %s", override, pkg)
	}
	out, err := c.Exec("-s", serial, "shell", "pm", command, "--user", "0", pkg)
	if err != nil || strings.TrimSpace(out) != "Package "+pkg+" new state: "+label {
		return fmt.Errorf("cannot set %s to %s for user 0: %s", pkg, label, strings.TrimSpace(out))
	}
	return nil
}

// Release only the selected journal entries. A different live override belongs
// to the user; preserve it. Completed entries remain recoverable on flush failure.
func (c *Client) restorePackages(serial string, state *SlimState, selected []string) ([]string, error) {
	var restored []string
	var problems []error
	type check struct {
		pkg     string
		want    int
		changed bool
	}
	var checks []check
	for _, pkg := range selected {
		current, err := c.packageOverride(serial, pkg)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		changed := current == packageDisabledUser
		if changed {
			current = packageEnabled
			if original := state.PackageStates[pkg]; original != nil {
				current = *original
			} else {
				fmt.Printf("   ⚠️  Legacy state has no original override for %s; restoring as enabled.\n", pkg)
			}
			if err := c.setPackageOverride(serial, pkg, current); err != nil {
				problems = append(problems, err)
				continue
			}
		}
		checks = append(checks, check{pkg, current, changed})
	}
	if len(checks) == 0 {
		return nil, errors.Join(problems...)
	}
	if err := c.flushPackageRestrictions(serial); err != nil {
		return nil, errors.Join(append(problems, err)...)
	}
	for _, pending := range checks {
		got, err := c.packageOverride(serial, pending.pkg)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		if got != pending.want {
			problems = append(problems, fmt.Errorf("restored override for %s: got %d, want %d", pending.pkg, got, pending.want))
			continue
		}
		state.forgetPackage(pending.pkg)
		if pending.changed {
			restored = append(restored, pending.pkg)
		}
	}
	return restored, errors.Join(problems...)
}
