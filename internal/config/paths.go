package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// GetAndroidSdkDir locates the Android SDK directory across macOS, Linux, and Windows.
func GetAndroidSdkDir() string {
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if val := os.Getenv(env); val != "" {
			if _, err := os.Stat(val); err == nil {
				return val
			}
		}
	}

	home, _ := os.UserHomeDir()
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates, filepath.Join(home, "Library", "Android", "sdk"))
	case "linux":
		candidates = append(candidates,
			filepath.Join(home, "Android", "Sdk"),
			filepath.Join(home, "Android", "sdk"),
			"/usr/lib/android-sdk",
			"/opt/android-sdk",
		)
	case "windows":
		if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
			candidates = append(candidates, filepath.Join(localApp, "Android", "Sdk"))
		}
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// GetAvdBaseDir is the AVD home, resolved as the emulator does: the first
// existing of $ANDROID_AVD_HOME, $ANDROID_SDK_HOME/avd, $ANDROID_USER_HOME/avd,
// else ~/.android/avd.
func GetAvdBaseDir() string {
	var candidates []string
	if v := os.Getenv("ANDROID_AVD_HOME"); v != "" {
		candidates = append(candidates, v)
	}
	if v := os.Getenv("ANDROID_SDK_HOME"); v != "" {
		candidates = append(candidates, filepath.Join(v, "avd"))
	}
	if v := os.Getenv("ANDROID_USER_HOME"); v != "" {
		candidates = append(candidates, filepath.Join(v, "avd"))
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.IsDir() {
			return c
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".android", "avd")
}

// FindAdbExecutable locates adb on macOS, Linux, or Windows.
func FindAdbExecutable() string {
	if p, err := exec.LookPath("adb"); err == nil {
		return p
	}
	if sdk := GetAndroidSdkDir(); sdk != "" {
		name := "adb"
		if runtime.GOOS == "windows" {
			name = "adb.exe"
		}
		p := filepath.Join(sdk, "platform-tools", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "adb"
}

// FindEmulatorExecutable locates emulator on macOS, Linux, or Windows.
func FindEmulatorExecutable() string {
	if p, err := exec.LookPath("emulator"); err == nil {
		return p
	}
	if sdk := GetAndroidSdkDir(); sdk != "" {
		name := "emulator"
		if runtime.GOOS == "windows" {
			name = "emulator.exe"
		}
		p := filepath.Join(sdk, "emulator", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "emulator"
}

// GetRecommendedGpuMode returns the optimal GPU acceleration mode for the current platform.
func GetRecommendedGpuMode() string {
	switch runtime.GOOS {
	case "darwin":
		return "host"
	case "linux":
		// On headless Linux (e.g. CI runner/Docker without X11 or Wayland display)
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return "swiftshader_indirect"
		}
		return "auto"
	case "windows":
		return "auto"
	default:
		return "auto"
	}
}

// GetGpuBackendDescription returns a friendly description of what the GPU mode maps to.
func GetGpuBackendDescription(gpuMode string) string {
	switch gpuMode {
	case "host":
		switch runtime.GOOS {
		case "darwin":
			return "Apple Silicon Metal hardware acceleration"
		case "linux":
			if os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" {
				return "Native Linux DRI / OpenGL / Vulkan hardware acceleration"
			}
			return "Direct host GPU (Warning: headless Linux without display may require swiftshader_indirect)"
		case "windows":
			return "Direct3D 11 via ANGLE / native Windows GPU acceleration"
		default:
			return "Host physical GPU acceleration"
		}
	case "auto":
		switch runtime.GOOS {
		case "windows":
			return "Automatic (Direct3D 11 via ANGLE / native Windows GPU)"
		case "linux":
			return "Automatic (native DRI / OpenGL / Vulkan hardware)"
		default:
			return "Automatic GPU selection"
		}
	case "swiftshader_indirect":
		return "Google SwiftShader optimized CPU software renderer (headless CI friendly)"
	case "angle_indirect":
		return "Direct3D 11 via ANGLE"
	default:
		return gpuMode
	}
}
