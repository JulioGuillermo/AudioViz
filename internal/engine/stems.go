package engine

import (
	"fmt"
	"path/filepath"
	"time"

	"audioviz/internal/audio"
)

// The song separated into its instruments (audio.StemFrame): when its stems are in the project's
// cache (the studio separates it as a job), each frame of every analysis points at its part, and
// the effects can react to the drums, the kick, the vocals...

// StemsDir: where the song's stems go in the project's cache ("" without a song or a project)
func (s *Session) StemsDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stemsDirLocked()
}

func (s *Session) stemsDirLocked() string {
	if s.Cfg == nil || s.Cfg.Audio.Path == "" || s.Cfg.Dir == "" {
		return ""
	}
	return audio.StemsDir(filepath.Join(s.Cfg.Dir, "cache"), s.Cfg.Audio.Path)
}

// HasStems: the song is separated
func (s *Session) HasStems() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stems != nil
}

// ReloadStems takes the stems again from the cache (after separating the song)
func (s *Session) ReloadStems() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stemsKey = ""
	s.loadStemsLocked()
}

// loadStemsLocked: the separated song analyzed (once per song, frame rate and window), or none
func (s *Session) loadStemsLocked() {
	dir := s.stemsDirLocked()
	key := ""
	if dir != "" && audio.StemsReady(dir) {
		key = fmt.Sprintf("%s|%d|%d|%g", dir, s.FPS, s.Frames, s.Cfg.Audio.NormalizeWindow)
	}
	if key == s.stemsKey {
		return
	}
	s.stems, s.stemsKey = nil, key
	if key != "" {
		t0 := time.Now()
		var st [4][]float32
		ok := true
		for i, n := range audio.StemNames {
			x, err := audio.LoadStem(filepath.Join(dir, n+".wav"))
			if err != nil {
				s.Logf("⚠️  Stem %s: %v", n, err)
				ok = false
				break
			}
			st[i] = x
		}
		if ok {
			s.stems = audio.AnalyzeStems(st, s.FPS, s.Frames, s.Cfg.Audio.NormalizeWindow)
			s.Logf("🥁 Separated song: drums, bass, vocals and the rest analyzed (%v)", time.Since(t0).Round(time.Millisecond))
		}
	}
	for _, f := range s.feats {
		s.attachStems(f)
	}
	s.attachStems(s.silent)
}

// attachStems points each frame of an analysis at its part of the separated song
func (s *Session) attachStems(f []audio.FrameFeatures) {
	for i := range f {
		f[i].Stem = nil
		if i < len(s.stems) {
			f[i].Stem = &s.stems[i]
		}
	}
}
