package depth

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Depth maps edited by hand (the studio's depth editor): a whole new map per frame (a still's
// one map, or a frame of a video's sequence), kept in the project's depth-edits folder (not in the
// cache: it is the user's work), which wins over the worked out map wherever there is one.

// EditDir: the folder of the hand edits of a request's map
func EditDir(cacheDir string, r Request) string {
	base := strings.TrimSuffix(filepath.Base(CachePath(cacheDir, r)), ".png")
	return filepath.Join(filepath.Dir(cacheDir), "depth-edits", base)
}

// EditPath: the edited map of frame i (a still: any i, its one map)
func EditPath(cacheDir string, r Request, i int) string {
	if !r.All {
		return filepath.Join(EditDir(cacheDir, r), "map.png")
	}
	return filepath.Join(EditDir(cacheDir, r), fmt.Sprintf("%05d.png", i))
}

// EditStamp: when frame i was last edited (0: never), so what keeps fitted maps sees a new edit
func EditStamp(cacheDir string, r Request, i int) int64 {
	if st, err := os.Stat(EditPath(cacheDir, r, i)); err == nil {
		return st.ModTime().UnixNano()
	}
	return 0
}

// Edited: the frames edited by hand, in order (a still: [0] or none)
func Edited(cacheDir string, r Request) []int {
	es, err := os.ReadDir(EditDir(cacheDir, r))
	if err != nil {
		return nil
	}
	var out []int
	for _, e := range es {
		n := e.Name()
		if !strings.HasSuffix(n, ".png") {
			continue
		}
		if n == "map.png" {
			if !r.All {
				out = append(out, 0)
			}
			continue
		}
		if i, err := strconv.Atoi(strings.TrimSuffix(n, ".png")); err == nil && r.All {
			out = append(out, i)
		}
	}
	sort.Ints(out)
	return out
}

// SaveEdit keeps m as the edited map of frame i
func SaveEdit(cacheDir string, r Request, i int, m *Map) error {
	return m.Save(EditPath(cacheDir, r, i))
}

// DropEdit: frame i back to the worked out map (i < 0: every frame)
func DropEdit(cacheDir string, r Request, i int) error {
	if i < 0 {
		return os.RemoveAll(EditDir(cacheDir, r))
	}
	err := os.Remove(EditPath(cacheDir, r, i))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Original: the worked out map of frame i, without the hand edits (ok false: not worked out)
func Original(cacheDir string, r Request, i int) (*Map, bool) {
	if !r.All {
		m, err := Load(CachePath(cacheDir, r))
		if err != nil { // (only the hand edit came along: it is the map)
			m, err = Load(EditPath(cacheDir, r, 0))
		}
		return m, err == nil
	}
	sq, ok := OpenSeq(cacheDir, r)
	if !ok {
		return nil, false
	}
	m, err := Load(sq.framePath(max(0, min(sq.N-1, i))))
	return m, err == nil
}

// Size: the size of a request's maps (of its first frame), without reading them whole
func Size(cacheDir string, r Request) (w, h int, ok bool) {
	p := CachePath(cacheDir, r)
	if r.All {
		sq, found := OpenSeq(cacheDir, r)
		if !found {
			return 0, 0, false
		}
		p = sq.framePath(0)
	}
	f, err := os.Open(p)
	if err != nil && !r.All { // (only the hand edit came along: it is the map)
		f, err = os.Open(EditPath(cacheDir, r, 0))
	}
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	c, err := png.DecodeConfig(f)
	if err != nil {
		return 0, 0, false
	}
	return c.Width, c.Height, true
}
