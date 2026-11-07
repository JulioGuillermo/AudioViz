package aimodel

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// The catalog: every model file AudioViz may download, registered by the feature that uses it
// (Register, from each package's init), so the studio can list what is on disk, what uses it, and
// delete what is not wanted. A deleted file is downloaded again the next time a feature needs it
// (Ensure).

var (
	catMu  sync.Mutex
	catLab = map[string]string{}   // file -> label
	catUse = map[string][]string{} // file -> features that use it
)

// Register records the files a feature uses (e.g. "Images · Z-Image Turbo")
func Register(feature string, ms ...Model) {
	catMu.Lock()
	defer catMu.Unlock()
	for _, m := range ms {
		if m.File == "" {
			continue
		}
		if catLab[m.File] == "" {
			catLab[m.File] = m.Label
		}
		used := false
		for _, f := range catUse[m.File] {
			used = used || f == feature
		}
		if !used {
			catUse[m.File] = append(catUse[m.File], feature)
		}
	}
}

// FileInfo is a file in the models folder
type FileInfo struct {
	File    string   `json:"file"`
	Label   string   `json:"label"`
	Size    int64    `json:"size"`    // on disk
	Partial bool     `json:"partial"` // an unfinished download (.part)
	Known   bool     `json:"known"`   // some feature uses it
	UsedBy  []string `json:"usedBy"`
}

// ModelsDir is where the model files are
func ModelsDir() string { return filepath.Join(CacheDir(), "models") }

// Files lists the files in the models folder, the largest first
func Files() []FileInfo {
	ents, _ := os.ReadDir(ModelsDir())
	catMu.Lock()
	defer catMu.Unlock()
	var out []FileInfo
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		st, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		fi := FileInfo{File: name, Size: st.Size()}
		base := strings.TrimSuffix(name, ".part")
		fi.Partial = base != name
		if uses, ok := catUse[base]; ok {
			fi.Known = true
			fi.Label = catLab[base]
			fi.UsedBy = append([]string{}, uses...)
			sort.Strings(fi.UsedBy)
		}
		out = append(out, fi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out
}

// Remove deletes a file of the models folder (and its unfinished download)
func Remove(file string) error {
	if file == "" || file != filepath.Base(file) || strings.HasPrefix(file, ".") {
		return errors.New("no such model file")
	}
	path := filepath.Join(ModelsDir(), file)
	if _, err := os.Stat(path); err != nil {
		return errors.New("no such model file")
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	if !strings.HasSuffix(file, ".part") {
		_ = os.Remove(path + ".part")
	}
	return nil
}
