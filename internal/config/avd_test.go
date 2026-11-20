package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// avdFile writes body to path, creating parent directories.
func avdFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

// envPath joins rel onto base; "" stays "" so the variable is left unset.
func envPath(base, rel string) string {
	if rel == "" {
		return ""
	}
	return filepath.Join(base, rel)
}

func TestAvdHomeOrder(t *testing.T) {
	cases := []struct {
		name                       string
		avdHome, sdkHome, userHome string   // env values relative to the temp base; "" = unset
		existing                   []string // directories to create, relative to base
		want                       string   // expected AVD home, relative to base
	}{
		{"ANDROID_AVD_HOME wins", "ext/avd", "sdk", "user",
			[]string{"ext/avd", "sdk/avd", "user/avd"}, "ext/avd"},
		{"missing ANDROID_AVD_HOME falls to SDK home", "gone", "sdk", "user",
			[]string{"sdk/avd", "user/avd"}, "sdk/avd"},
		{"SDK home without avd dir falls to user home", "", "sdk", "user",
			[]string{"user/avd"}, "user/avd"},
		{"nothing set uses ~/.android/avd", "", "", "", nil, "home/.android/avd"},
		{"no candidate exists uses ~/.android/avd", "gone", "sdk", "user", nil, "home/.android/avd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			for _, d := range tc.existing {
				if err := os.MkdirAll(filepath.Join(base, d), 0755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("HOME", filepath.Join(base, "home"))
			t.Setenv("ANDROID_AVD_HOME", envPath(base, tc.avdHome))
			t.Setenv("ANDROID_SDK_HOME", envPath(base, tc.sdkHome))
			t.Setenv("ANDROID_USER_HOME", envPath(base, tc.userHome))
			if got, want := GetAvdBaseDir(), filepath.Join(base, tc.want); got != want {
				t.Errorf("GetAvdBaseDir() = %q, want %q", got, want)
			}
		})
	}
}

// Files may contain @AVD@ (the AVD home) and @BASE@ (its parent) placeholders.
func TestResolveAvd(t *testing.T) {
	cases := []struct {
		name     string
		files    map[string]string
		lookup   string
		wantName string // "" = not found
		wantDir  string // relative to the AVD home
	}{
		{
			name: "ini name differs from folder (mismatch)",
			files: map[string]string{
				"Medium_Phone_API_37.0.ini":   "path=@AVD@/Medium_Phone.avd\n",
				"Medium_Phone.avd/config.ini": "hw.ramSize=1536\n",
			},
			lookup: "Medium_Phone_API_37.0", wantName: "Medium_Phone_API_37.0", wantDir: "Medium_Phone.avd",
		},
		{
			name: "path.rel is relative to the AVD home's parent",
			files: map[string]string{
				"Foo.ini":            "path.rel=avd/Foo.avd\n",
				"Foo.avd/config.ini": "hw.ramSize=1536\n",
			},
			lookup: "Foo", wantName: "Foo", wantDir: "Foo.avd",
		},
		{
			name: "folder name is an alias for the ini name",
			files: map[string]string{
				"Medium_Phone_API_37.0.ini":   "path=@AVD@/Medium_Phone.avd\n",
				"Medium_Phone.avd/config.ini": "hw.ramSize=1536\n",
			},
			lookup: "Medium_Phone", wantName: "Medium_Phone_API_37.0", wantDir: "Medium_Phone.avd",
		},
		{
			name: "lookup is case-insensitive",
			files: map[string]string{
				"Medium_Phone_API_37.0.ini":   "path=@AVD@/Medium_Phone.avd\n",
				"Medium_Phone.avd/config.ini": "hw.ramSize=1536\n",
			},
			lookup: "medium_phone_api_37.0", wantName: "Medium_Phone_API_37.0", wantDir: "Medium_Phone.avd",
		},
		{
			name: "folder without ini is still an AVD",
			files: map[string]string{
				"Old.avd/config.ini": "hw.ramSize=4096\n",
			},
			lookup: "old", wantName: "Old", wantDir: "Old.avd",
		},
		{
			name: "ini without path falls back to <home>/<name>.avd",
			files: map[string]string{
				"Bare.ini":            "target=android-37\n",
				"Bare.avd/config.ini": "hw.ramSize=1536\n",
			},
			lookup: "Bare", wantName: "Bare", wantDir: "Bare.avd",
		},
		{
			name: "unknown name is not found",
			files: map[string]string{
				"Old.avd/config.ini": "hw.ramSize=4096\n",
			},
			lookup: "Nope",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			avdHome := filepath.Join(base, "avd")
			if err := os.MkdirAll(avdHome, 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("HOME", filepath.Join(base, "home"))
			t.Setenv("ANDROID_AVD_HOME", avdHome)
			t.Setenv("ANDROID_SDK_HOME", "")
			t.Setenv("ANDROID_USER_HOME", "")
			r := strings.NewReplacer("@AVD@", avdHome, "@BASE@", base)
			for rel, body := range tc.files {
				avdFile(t, filepath.Join(avdHome, rel), r.Replace(body))
			}

			name, dir, ok := ResolveAvd(tc.lookup)
			if tc.wantName == "" {
				if ok {
					t.Fatalf("ResolveAvd(%q) found %q, want not found", tc.lookup, name)
				}
				return
			}
			if !ok {
				t.Fatalf("ResolveAvd(%q) not found, want %q", tc.lookup, tc.wantName)
			}
			if name != tc.wantName {
				t.Errorf("name = %q, want %q", name, tc.wantName)
			}
			if want := filepath.Join(avdHome, tc.wantDir); dir != want {
				t.Errorf("dir = %q, want %q", dir, want)
			}
			if got := AvdDir(tc.lookup); got != filepath.Join(avdHome, tc.wantDir) {
				t.Errorf("AvdDir(%q) = %q, want the resolved dir", tc.lookup, got)
			}
		})
	}
}

// The list shows the emulator's names: the ini basename, and the folder's
// config.ini is read from wherever the ini points.
func TestGetInstalledAvdsUsesIniName(t *testing.T) {
	base := t.TempDir()
	avdHome := filepath.Join(base, "avd")
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("ANDROID_AVD_HOME", avdHome)
	t.Setenv("ANDROID_SDK_HOME", "")
	t.Setenv("ANDROID_USER_HOME", "")
	avdFile(t, filepath.Join(avdHome, "Medium_Phone_API_37.0.ini"), "path="+filepath.Join(avdHome, "Medium_Phone.avd")+"\n")
	avdFile(t, filepath.Join(avdHome, "Medium_Phone.avd", "config.ini"), "hw.ramSize=1536\n")
	avdFile(t, filepath.Join(avdHome, "Old.avd", "config.ini"), "hw.ramSize=4096\n")
	avdFile(t, filepath.Join(avdHome, "Orphan.ini"), "path.rel=avd/Gone.avd\n") // no config.ini: skipped

	got := map[string]string{}
	for _, a := range GetInstalledAvds() {
		got[a["name"]] = a["hw.ramSize"]
	}
	want := map[string]string{"Medium_Phone_API_37.0": "1536", "Old": "4096"}
	if len(got) != len(want) {
		t.Fatalf("GetInstalledAvds() = %v, want %v", got, want)
	}
	for name, ram := range want {
		if got[name] != ram {
			t.Errorf("%s: hw.ramSize = %q, want %q (all: %v)", name, got[name], ram, got)
		}
	}
}
