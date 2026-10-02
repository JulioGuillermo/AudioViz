package lyricsgen

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Model is a weights file downloaded once to the local cache
type Model struct {
	Name  string
	File  string
	URL   string
	Size  int64
	Label string
}

var (
	// Quantized Whisper large-v3-turbo: nearly the accuracy of large-v3, much faster
	WhisperModel = Model{Name: "whisper", Label: "Speech recognition (Whisper large-v3-turbo)", File: "ggml-large-v3-turbo-q5_0.bin",
		URL: "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3-turbo-q5_0.bin", Size: 574041195}
	DemucsModel = Model{Name: "demucs", Label: "Vocal separation (Demucs htdemucs)", File: "ggml-model-htdemucs-4s-f16.bin",
		URL: "https://huggingface.co/datasets/Retrobear/demucs.cpp/resolve/main/ggml-model-htdemucs-4s-f16.bin", Size: 83994361}
)

// aheadsLargeV3Turbo = WHISPER_AHEADS_LARGE_V3_TURBO in whisper.h
const aheadsLargeV3Turbo = 14

// CacheDir is the folder for models and caches (~/.cache/audioviz)
func CacheDir() string {
	if d := os.Getenv("AUDIOVIZ_CACHE"); d != "" {
		return d
	}
	d, err := os.UserCacheDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "audioviz")
}

func (m Model) Path() string { return filepath.Join(CacheDir(), "models", m.File) }

// Present reports whether the model is already fully downloaded
func (m Model) Present() bool {
	st, err := os.Stat(m.Path())
	return err == nil && (m.Size == 0 || st.Size() == m.Size)
}

// Ensure downloads the model if missing (resumable). progress(done, total, bytes/s)
func (m Model) Ensure(ctx context.Context, progress func(done, total int64, rate float64)) (string, error) {
	path := m.Path()
	if m.Present() {
		return path, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	part := path + ".part"
	var have int64
	if st, err := os.Stat(part); err == nil {
		have = st.Size()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", m.URL, nil)
	if err != nil {
		return "", err
	}
	if have > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(have, 10)+"-")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", m.File, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		have = 0
	case http.StatusPartialContent:
	default:
		return "", fmt.Errorf("downloading %s: HTTP %s", m.File, resp.Status)
	}
	flag := os.O_CREATE | os.O_WRONLY
	if have > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flag, 0644)
	if err != nil {
		return "", err
	}
	total := m.Size
	if total == 0 && resp.ContentLength > 0 {
		total = have + resp.ContentLength
	}
	buf := make([]byte, 1<<20)
	done := have
	t0, last := time.Now(), time.Time{}
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				return "", err
			}
			done += int64(n)
			if progress != nil && time.Since(last) > 200*time.Millisecond {
				last = time.Now()
				progress(done, total, float64(done-have)/time.Since(t0).Seconds())
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return "", fmt.Errorf("downloading %s: %w", m.File, rerr)
		}
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if m.Size > 0 && done != m.Size {
		return "", fmt.Errorf("incomplete download of %s (%d of %d bytes)", m.File, done, m.Size)
	}
	if progress != nil {
		progress(done, total, 0)
	}
	return path, os.Rename(part, path)
}
