package config

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// avdEntry is one AVD as the emulator names it (the .ini basename) and the
// folder holding its config.ini and snapshots.
type avdEntry struct {
	name string
	dir  string
}

// listAvds enumerates the AVDs in home. Every <name>.ini is an AVD (see
// iniAvdDir for its folder); every <name>.avd folder that no .ini names is an
// AVD too, named by its folder.
func listAvds(home string) []avdEntry {
	entries, err := os.ReadDir(home)
	if err != nil {
		return nil
	}

	var list []avdEntry
	taken := map[string]bool{} // lower-cased names already listed
	claimed := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ini") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".ini")
		dir := iniAvdDir(home, filepath.Join(home, e.Name()), name)
		list = append(list, avdEntry{name: name, dir: dir})
		taken[strings.ToLower(name)] = true
		claimed[dir] = true
	}
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), ".avd") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".avd")
		dir := filepath.Join(home, e.Name())
		if claimed[dir] || taken[strings.ToLower(name)] {
			continue
		}
		list = append(list, avdEntry{name: name, dir: dir})
		taken[strings.ToLower(name)] = true
	}
	sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })
	return list
}

// iniAvdDir reads the folder an AVD's .ini points at. path= is absolute (a
// leading ~/ is expanded); path.rel= is relative to the Android user home,
// which is the parent of the AVD home. Without either, it is <home>/<name>.avd.
func iniAvdDir(home, iniPath, name string) string {
	fallback := filepath.Join(home, name+".avd")
	kv, err := readConfigIni(iniPath)
	if err != nil {
		return fallback
	}
	if p := kv["path"]; p != "" {
		if rest, ok := strings.CutPrefix(p, "~/"); ok {
			if userHome, err := os.UserHomeDir(); err == nil {
				return filepath.Join(userHome, filepath.FromSlash(rest))
			}
		}
		return filepath.FromSlash(p)
	}
	if rel := kv["path.rel"]; rel != "" {
		return filepath.Join(filepath.Dir(home), filepath.FromSlash(rel))
	}
	return fallback
}

// ResolveAvd maps a user-typed AVD name to the name the emulator knows it by
// (for -avd) and its folder. The folder name without ".avd" is accepted as an
// alias. Matching is case-insensitive; ok is false when nothing matches.
func ResolveAvd(name string) (avdName, dir string, ok bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", false
	}
	list := listAvds(GetAvdBaseDir())
	for _, a := range list {
		if strings.EqualFold(a.name, name) {
			return a.name, a.dir, true
		}
	}
	for _, a := range list {
		if strings.EqualFold(filepath.Base(a.dir), name+".avd") {
			return a.name, a.dir, true
		}
	}
	return "", "", false
}
