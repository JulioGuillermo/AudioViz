package studio

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"audioviz/internal/imagegen"
)

// Plugins (internal/imagegen/plugins.go): installed from a .zip (Settings › Plugins) into
// ~/.cache/audioviz/plugins/<id>/, listed, removed; and the image engine they offer.

// hPlugins: the installed plugins, the engine setting and the device in use
func (s *Server) hPlugins(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"plugins": imagegen.Plugins(), "engine": imagegen.Engine(), "device": imagegen.Device(),
		"intel": imagegen.IntelGPU(), "dir": imagegen.PluginsDir()})
}

// hPluginEngine: PUT {engine}: auto, vulkan, sycl
func (s *Server) hPluginEngine(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Engine string `json:"engine"`
	}
	if err := readJSON(r, &req); err != nil {
		fail(w, 400, err)
		return
	}
	if err := imagegen.SetEngine(req.Engine); err != nil {
		fail(w, 400, err)
		return
	}
	s.hPlugins(w, r)
}

// hPluginInstall: a plugin's .zip (multipart "file", or "path" on this machine) unpacked into
// its folder; loaded at once if it can be (else: when AudioViz starts again)
func (s *Server) hPluginInstall(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		fail(w, 400, err)
		return
	}
	var (
		ra   io.ReaderAt
		size int64
	)
	if f, h, err := r.FormFile("file"); err == nil {
		defer f.Close()
		ra, size = f, h.Size
	} else if p := r.FormValue("path"); p != "" {
		f, err := os.Open(p)
		if err != nil {
			fail(w, 400, err)
			return
		}
		defer f.Close()
		st, _ := f.Stat()
		ra, size = f, st.Size()
	} else {
		fail(w, 400, errors.New("no plugin given"))
		return
	}
	p, restart, err := installPlugin(ra, size)
	if err != nil {
		fail(w, 400, err)
		return
	}
	if !restart {
		_ = imagegen.Refresh()
	}
	s.logf("🧩 Plugin installed: %s %s", p.Name, p.Version)
	writeJSON(w, map[string]any{"plugin": p, "restart": restart})
}

// installPlugin unpacks a plugin zip (plugin.json at its root or in its one folder) into
// PluginsDir/<id>; restart: one with that id was loaded (the new one is used after a restart)
func installPlugin(ra io.ReaderAt, size int64) (imagegen.Plugin, bool, error) {
	var none imagegen.Plugin
	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return none, false, fmt.Errorf("not a zip: %w", err)
	}
	if err := os.MkdirAll(imagegen.PluginsDir(), 0o755); err != nil {
		return none, false, err
	}
	tmp, err := os.MkdirTemp(imagegen.PluginsDir(), ".install-")
	if err != nil {
		return none, false, err
	}
	defer os.RemoveAll(tmp)
	for _, f := range zr.File {
		name := filepath.Clean(filepath.FromSlash(f.Name))
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return none, false, fmt.Errorf("unsafe path in the zip: %s", f.Name)
		}
		dst := filepath.Join(tmp, name)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return none, false, err
			}
			continue
		}
		if f.Mode()&os.ModeSymlink != 0 {
			continue // (links are not followed: the libraries come as files)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return none, false, err
		}
		rc, err := f.Open()
		if err != nil {
			return none, false, err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err == nil {
			_, err = io.Copy(out, rc)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
		}
		rc.Close()
		if err != nil {
			return none, false, err
		}
	}
	// plugin.json at the root, or in the zip's one folder
	root := tmp
	if _, err := os.Stat(filepath.Join(root, "plugin.json")); err != nil {
		es, _ := os.ReadDir(tmp)
		if len(es) == 1 && es[0].IsDir() {
			root = filepath.Join(tmp, es[0].Name())
		}
	}
	p, err := imagegen.ReadPlugin(root)
	if err != nil {
		return none, false, err
	}
	if msg := p.Compatible(); msg != "" {
		return none, false, fmt.Errorf("this plugin can't be used: %s", msg)
	}
	dst := filepath.Join(imagegen.PluginsDir(), p.ID)
	restart := imagegen.Loaded(dst)
	if restart {
		// (the loaded one stays mapped: the new one goes beside it, swapped in at the next start)
		dst += ".new"
	}
	_ = os.RemoveAll(dst)
	if err := os.Rename(root, dst); err != nil {
		return none, false, err
	}
	p.Dir = dst
	return p, restart, nil
}

// hPluginRemove: DELETE ?id= (a loaded one goes when AudioViz restarts)
func (s *Server) hPluginRemove(w http.ResponseWriter, r *http.Request) {
	id := filepath.Base(r.URL.Query().Get("id"))
	dir := filepath.Join(imagegen.PluginsDir(), id)
	if id == "" || id == "." || strings.HasPrefix(id, ".") {
		fail(w, 400, errors.New("no such plugin"))
		return
	}
	if _, err := os.Stat(dir); err != nil {
		fail(w, 404, errors.New("no such plugin"))
		return
	}
	restart := imagegen.Loaded(dir)
	if restart {
		_ = os.WriteFile(filepath.Join(dir, ".remove"), nil, 0o644)
	} else if err := os.RemoveAll(dir); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "restart": restart})
}
