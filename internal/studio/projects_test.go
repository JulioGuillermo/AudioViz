package studio

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func mkzip(t *testing.T, files map[string]string) *bytes.Reader {
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for n, c := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(c))
	}
	zw.Close()
	return bytes.NewReader(b.Bytes())
}

func TestUnpack(t *testing.T) {
	ws := t.TempDir()
	// Inside a top folder, as hPackage writes it
	z := mkzip(t, map[string]string{"song/project.yaml": "a", "song/assets/bg.png": "png", "song/letra.lrc": "l"})
	dir, err := unpack(z, z.Size(), "song.zip", ws)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "song" {
		t.Errorf("dir = %s", dir)
	}
	for _, f := range []string{"project.yaml", "assets/bg.png", "letra.lrc", "exports"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("missing %s", f)
		}
	}
	// Same name again: a new folder
	z = mkzip(t, map[string]string{"song/project.yaml": "a"})
	dir2, err := unpack(z, z.Size(), "song.zip", ws)
	if err != nil || filepath.Base(dir2) != "song-2" {
		t.Errorf("second import = %s, %v", dir2, err)
	}
	// project.yaml at the root: named after the zip
	z = mkzip(t, map[string]string{"project.yaml": "a"})
	dir3, err := unpack(z, z.Size(), "My Video.zip", ws)
	if err != nil || filepath.Base(dir3) != "My Video" {
		t.Errorf("root import = %s, %v", dir3, err)
	}
	// Escaping the folder is refused and leaves nothing behind
	z = mkzip(t, map[string]string{"evil/project.yaml": "a", "evil/../../x.txt": "x"})
	if _, err := unpack(z, z.Size(), "evil.zip", ws); err == nil {
		t.Error("unsafe zip accepted")
	}
	if _, err := os.Stat(filepath.Join(ws, "evil")); err == nil {
		t.Error("partial folder left after a refused import")
	}
	// No project inside
	z = mkzip(t, map[string]string{"a.txt": "a"})
	if _, err := unpack(z, z.Size(), "a.zip", ws); err == nil {
		t.Error("zip without project.yaml accepted")
	}
}
