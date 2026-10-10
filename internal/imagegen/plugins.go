package imagegen

/*
#include <stdlib.h>
int av_load_backend(const char *path);
int av_use(const char *prefix);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"audioviz/internal/aimodel"
)

// Backend plugins: ggml backends AudioViz loads at run time (scripts/build-sycl-plugin.sh), kept
// in ~/.cache/audioviz/plugins/<id>/ with their libraries and a plugin.json. The engine setting
// (plugins/engine: auto, vulkan, sycl) says which one the image models use; auto: a plugin's
// backend when it loaded (on Intel GPUs: SYCL), else the built-in one (Vulkan, Metal).

// mlStamp: the ML libraries' version (set at build, build.sh); a plugin must be built for it
var mlStamp string

// Plugin: an installed plugin (plugin.json) and how it went
type Plugin struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Kind    string `json:"kind"`    // ggml-backend
	Backend string `json:"backend"` // its devices' name prefix: SYCL
	Lib     string `json:"lib"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	ML      string `json:"ml"`
	Version string `json:"version"`
	Dir     string `json:"dir"`
	Loaded  bool   `json:"loaded"`
	Error   string `json:"error,omitempty"`
}

var (
	plugMu  sync.Mutex
	plugins []Plugin
)

// PluginsDir: where plugins are installed
func PluginsDir() string { return filepath.Join(aimodel.CacheDir(), "plugins") }

// Compatible: whether a plugin is for this system and this build of AudioViz ("" = yes)
func (p Plugin) Compatible() string {
	switch {
	case p.Kind != "ggml-backend":
		return fmt.Sprintf("unknown kind of plugin %q", p.Kind)
	case p.OS != runtime.GOOS || p.Arch != runtime.GOARCH:
		return fmt.Sprintf("it is for %s/%s, this is %s/%s", p.OS, p.Arch, runtime.GOOS, runtime.GOARCH)
	case mlStamp != "" && p.ML != mlStamp:
		return "it was built for another version of AudioViz"
	}
	return ""
}

// ReadPlugin: the plugin in dir (its plugin.json)
func ReadPlugin(dir string) (Plugin, error) {
	var p Plugin
	b, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
	if err != nil {
		return p, errors.New("not a plugin: it has no plugin.json")
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return p, fmt.Errorf("its plugin.json: %w", err)
	}
	if p.ID == "" || p.Lib == "" || strings.ContainsAny(p.ID, `/\.`) {
		return p, errors.New("its plugin.json has no valid id or lib")
	}
	p.Dir = dir
	return p, nil
}

// Engine: the engine setting (auto, vulkan, sycl)
func Engine() string {
	b, _ := os.ReadFile(filepath.Join(PluginsDir(), "engine"))
	if e := strings.TrimSpace(string(b)); e != "" {
		return e
	}
	if e := os.Getenv("AUDIOVIZ_IMAGE_ENGINE"); e != "" {
		return e
	}
	return "auto"
}

// SetEngine changes the engine (the models are loaded again on it)
func SetEngine(e string) error {
	switch e {
	case "auto", "vulkan", "sycl":
	default:
		return fmt.Errorf("unknown engine %q", e)
	}
	if err := os.MkdirAll(PluginsDir(), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(PluginsDir(), "engine"), []byte(e+"\n"), 0o644); err != nil {
		return err
	}
	gen.Lock()
	defer gen.Unlock()
	eng.free()
	setup()
	if e != "vulkan" {
		loadPlugins() // (one installed since)
	}
	return useEngine()
}

// Plugins: the installed plugins and whether they loaded
func Plugins() []Plugin {
	setup()
	plugMu.Lock()
	defer plugMu.Unlock()
	known := map[string]bool{}
	out := append([]Plugin{}, plugins...)
	for _, p := range out {
		known[p.Dir] = true
	}
	// (installed after the start: not loaded yet)
	for _, d := range pluginDirs() {
		if known[d] {
			continue
		}
		if p, err := ReadPlugin(d); err == nil {
			p.Error = p.Compatible()
			out = append(out, p)
		}
	}
	return out
}

// Refresh loads the plugins installed since and points the models at the engine's device (they
// are loaded again on their next use)
func Refresh() error {
	setup()
	gen.Lock()
	defer gen.Unlock()
	eng.free()
	loadPlugins()
	return useEngine()
}

// Loaded: whether the plugin in dir is loaded in this process (it stays until AudioViz restarts)
func Loaded(dir string) bool {
	plugMu.Lock()
	defer plugMu.Unlock()
	for _, p := range plugins {
		if p.Dir == dir && p.Loaded {
			return true
		}
	}
	return false
}

// pending: what was left for the next start while a plugin was loaded: a new version (<id>.new)
// takes its place, one removed (.remove inside) goes
func pending() {
	es, _ := os.ReadDir(PluginsDir())
	for _, e := range es {
		d := filepath.Join(PluginsDir(), e.Name())
		switch {
		case strings.HasPrefix(e.Name(), ".install-"):
			os.RemoveAll(d)
		case strings.HasSuffix(e.Name(), ".new"):
			base := strings.TrimSuffix(d, ".new")
			os.RemoveAll(base)
			os.Rename(d, base)
		default:
			if _, err := os.Stat(filepath.Join(d, ".remove")); err == nil {
				os.RemoveAll(d)
			}
		}
	}
}

// pluginDirs: the installed plugins' folders
func pluginDirs() []string {
	var out []string
	es, _ := os.ReadDir(PluginsDir())
	for _, e := range es {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !strings.HasSuffix(e.Name(), ".new") {
			if _, err := os.Stat(filepath.Join(PluginsDir(), e.Name(), ".remove")); err != nil {
				out = append(out, filepath.Join(PluginsDir(), e.Name()))
			}
		}
	}
	return out
}

var pendingOnce sync.Once

// loadPlugins loads the compatible plugins not loaded yet (unless the engine is the built-in one)
func loadPlugins() {
	pendingOnce.Do(pending)
	if Engine() == "vulkan" {
		return
	}
	plugMu.Lock()
	defer plugMu.Unlock()
	for _, d := range pluginDirs() {
		done := false
		for _, q := range plugins {
			done = done || q.Dir == d
		}
		if done {
			continue
		}
		p, err := ReadPlugin(d)
		if err != nil {
			continue
		}
		if p.Error = p.Compatible(); p.Error == "" {
			// the SYCL runtime: its Level Zero GPUs only (its persistent kernel cache stays off: reading
			// it back crashes in the SYCL runtime)
			if os.Getenv("ONEAPI_DEVICE_SELECTOR") == "" {
				os.Setenv("ONEAPI_DEVICE_SELECTOR", "level_zero:gpu")
			}
			cp := C.CString(filepath.Join(d, p.Lib))
			p.Loaded = C.av_load_backend(cp) != 0
			C.free(unsafe.Pointer(cp))
			if !p.Loaded {
				p.Error = "it did not load (see AUDIOVIZ_DEBUG_SD=1)"
			}
		}
		plugins = append(plugins, p)
	}
	_ = useEngine()
}

// useEngine points the image models at the engine's device
func useEngine() error {
	prefix := ""
	switch Engine() {
	case "sycl":
		prefix = "SYCL"
	case "vulkan":
		prefix = "Vulkan"
	default: // auto: a loaded plugin's backend
		for _, p := range plugins {
			if p.Loaded && p.Backend != "" {
				prefix = p.Backend
				break
			}
		}
	}
	cp := C.CString(prefix)
	defer C.free(unsafe.Pointer(cp))
	if C.av_use(cp) == 0 {
		C.av_use(nil)
		if Engine() != "auto" {
			return fmt.Errorf("no %s device (is its plugin installed?)", prefix)
		}
	}
	return nil
}
