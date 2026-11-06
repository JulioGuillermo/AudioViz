package studio

import (
	"context"
	"os/exec"
	"runtime"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"audioviz/internal/audio"
	"audioviz/internal/lyricsgen"
)

// The song separated into its instruments (Demucs): worked out as a job (the model is downloaded
// the first time; it is the same one the lyrics use) and kept in the project's cache, then the
// effects can react to the drums, the kick, the snare, the hi-hats, the bass line, the vocals or
// the rest.

// GET /api/stems: whether the song is separated
func (s *Server) hStemsState(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	sess := s.sess
	s.mu.Unlock()
	if sess == nil {
		writeJSON(w, map[string]bool{"ready": false})
		return
	}
	dir := sess.StemsDir()
	writeJSON(w, map[string]any{"ready": sess.HasStems(), "model": lyricsgen.DemucsModel.Present(),
		"full": dir != "" && audio.StemsFull(dir), "dir": dir})
}

// POST /api/stems: separate the song (a job)
func (s *Server) hStems(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	sess := s.sess
	var path string
	full := false
	if s.cfg != nil {
		path, full = s.cfg.Audio.Path, s.cfg.Audio.StemsFull
	}
	s.mu.Unlock()
	if sess == nil || path == "" {
		fail(w, 400, errors.New("the project has no song"))
		return
	}
	dir := sess.StemsDir()
	if dir == "" {
		fail(w, 400, errors.New("no project is open"))
		return
	}
	j, err := s.startJob("stems", func(ctx context.Context, j *job) error {
		t0 := time.Now()
		say := func(msg string, p float64) {
			j.mu.Lock()
			j.Message, j.Progress = msg, p
			j.mu.Unlock()
			s.hub.send("job", j.snapshot())
		}
		m := lyricsgen.DemucsModel
		mp, err := m.Ensure(ctx, func(done, total int64, _ float64) {
			if total > 0 {
				say(fmt.Sprintf("Downloading the separation model · %d %%", done*100/total), 0.1*float64(done)/float64(total))
			}
		})
		if err != nil {
			return err
		}
		say("Reading the song", 0.1)
		d, err := audio.DecodeAudio(path, 1)
		if err != nil {
			return err
		}
		st := d.Samples
		if d.Channels == 1 {
			st = make([]float32, 2*len(d.Samples))
			for i, v := range d.Samples {
				st[2*i], st[2*i+1] = v, v
			}
		}
		stereo44 := lyricsgen.Resample(st, 2, d.SampleRate, 44100)
		sep := "Separating drums, bass, vocals and the rest · " + lyricsgen.DemucsDevice()
		say(sep, 0.12)
		ch := 1
		if full {
			ch = 2
		}
		err = lyricsgen.SeparateStemsTo(ctx, mp, stereo44, full, func(p float64) {
			say(sep, 0.12+0.83*p)
		}, func(i int, x []float32) error {
			say("Saving the parts", 0.96)
			return audio.SaveStem(filepath.Join(dir, audio.StemNames[i]+".wav"), x, ch, 44100, full)
		})
		if err != nil {
			return err
		}
		sess.ReloadStems()
		j.mu.Lock()
		j.Message = fmt.Sprintf("Song separated in %s", fmtSecs(time.Since(t0).Seconds()))
		j.mu.Unlock()
		return nil
	})
	if err != nil {
		fail(w, 409, err)
		return
	}
	s.tagJob(j, r, nil)
	writeJSON(w, j.snapshot())
}

// POST /api/stems/open: the folder of the song's parts in the file manager (on this machine)
func (s *Server) hStemsOpen(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	sess := s.sess
	s.mu.Unlock()
	if sess == nil || sess.StemsDir() == "" || !audio.StemsReady(sess.StemsDir()) {
		fail(w, 400, errors.New("the song is not separated"))
		return
	}
	if err := openInFileManager(sess.StemsDir()); err != nil {
		fail(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// openInFileManager shows a folder in the system's file manager
func openInFileManager(dir string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}
	return cmd.Start()
}
