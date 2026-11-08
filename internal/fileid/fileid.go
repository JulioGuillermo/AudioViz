// Package fileid names a file by its content, not by where it is: what is worked out from a file
// and kept (depth maps, separated instruments, the song's vocals…) is found again when the
// project is copied, moved, collected or imported from a package.
package fileid

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"sync"
)

var (
	mu   sync.Mutex
	memo = map[string]string{}
	once = map[string]bool{}
)

// Fingerprint: a file's content in short (its size and the sha1 of its first and last 64 KiB),
// remembered while its size and date stay the same; "" for no path, the path itself if it cannot
// be read
func Fingerprint(p string) string {
	if p == "" {
		return ""
	}
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return p
	}
	key := fmt.Sprintf("%s|%d|%d", p, st.Size(), st.ModTime().UnixNano())
	mu.Lock()
	fp, ok := memo[key]
	mu.Unlock()
	if ok {
		return fp
	}
	f, err := os.Open(p)
	if err != nil {
		return p
	}
	defer f.Close()
	h := sha1.New()
	const chunk = 64 << 10
	io.Copy(h, io.LimitReader(f, chunk))
	if st.Size() > 2*chunk {
		f.Seek(-chunk, io.SeekEnd)
		io.Copy(h, f)
	} else if st.Size() > chunk {
		io.Copy(h, f)
	}
	fp = fmt.Sprintf("%d:%x", st.Size(), h.Sum(nil))
	mu.Lock()
	if len(memo) > 4096 {
		memo = map[string]string{}
	}
	memo[key] = fp
	mu.Unlock()
	return fp
}

// Once: true the first time it is asked for key (in this run), so a move from an old name to a
// new one is tried once
func Once(key string) bool {
	mu.Lock()
	defer mu.Unlock()
	if once[key] {
		return false
	}
	once[key] = true
	return true
}

// Move renames old to new when old exists and new does not (what was kept under an old name)
func Move(old, new string) {
	if old == new {
		return
	}
	if _, err := os.Stat(old); err != nil {
		return
	}
	if _, err := os.Stat(new); err == nil {
		return
	}
	os.Rename(old, new)
}
