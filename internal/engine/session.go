package engine

import (
	"fmt"
	"image"
	"math"
	"os"
	"sync"
	"time"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"audioviz/internal/lyrics"
	"audioviz/internal/particles"
	"audioviz/internal/video"

	"gopkg.in/yaml.v3"
)

// Session es un proyecto cargado y listo para renderizar cualquier cuadro (preview) o
// exportarlo entero. La comparten el CLI y el estudio.
type Session struct {
	Cfg    *config.ProjectConfig
	W, H   int
	FPS    int
	Frames int
	Audio  *audio.DecodedAudio

	Logf func(format string, args ...any)

	mu        sync.Mutex
	audioKey  string
	feats     map[anaKey][]audio.FrameFeatures
	baseKey   anaKey
	media     map[string]video.BackgroundReader
	lyr       *lyrics.Multi
	lyrKey    string
	LyrVer    int // cambia cada vez que se reconstruye el atlas de la letra
	sim       particleSim
	silent    []audio.FrameFeatures // cuando el proyecto aún no tiene audio
}

type anaKey struct {
	bars                       int
	smoothing, decay, sens     float64
	normalize                  bool
	window                     float64
}

const silentDuration = 30.0 // s de línea de tiempo cuando no hay audio

// Open carga el proyecto: audio, análisis, fondos y letra
func Open(cfg *config.ProjectConfig, logf func(string, ...any)) (*Session, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	s := &Session{Logf: logf, feats: map[anaKey][]audio.FrameFeatures{}, media: map[string]video.BackgroundReader{}}
	if err := s.Update(cfg); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, r := range s.media {
		r.Close()
		delete(s.media, k)
	}
}

// Update aplica una config nueva reutilizando todo lo que no ha cambiado (audio, análisis, fondos)
func (s *Session) Update(cfg *config.ProjectConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Resolución: la de salida o la del primer fondo
	w, h := cfg.Output.Width, cfg.Output.Height
	if w <= 0 || h <= 0 {
		w, h = 1080, 1920
		if p := cfg.Timeline.Background[0].Cfg.Path; p != "" {
			probe, err := video.ProbeMedia(p)
			if err != nil {
				return fmt.Errorf("fondo: %w", err)
			}
			w, h = probe.Width, probe.Height
		}
	}
	w, h = max(w/2*2, 16), max(h/2*2, 16)
	if w != s.W || h != s.H {
		// Los fondos están escalados a la resolución: hay que recargarlos
		for k, r := range s.media {
			r.Close()
			delete(s.media, k)
		}
	}
	s.W, s.H = w, h
	if cfg.Output.FPS <= 0 {
		cfg.Output.FPS = 60
	}
	if cfg.Output.FPS != s.FPS {
		s.feats = map[anaKey][]audio.FrameFeatures{}
	}
	s.FPS = cfg.Output.FPS

	// Audio
	ak := fmt.Sprintf("%s|%g", cfg.Audio.Path, cfg.Audio.Volume)
	if ak != s.audioKey {
		s.Audio = nil
		s.feats = map[anaKey][]audio.FrameFeatures{}
		if cfg.Audio.Path != "" {
			t0 := time.Now()
			d, err := audio.DecodeAudio(cfg.Audio.Path, cfg.Audio.Volume)
			if err != nil {
				return err
			}
			s.Audio = d
			s.Logf("🎵 Audio: %.2f s, %d Hz, %d bits (%v)", d.DurationSec, d.SampleRate, d.BitsPerSample, time.Since(t0).Round(time.Millisecond))
		}
		s.audioKey = ak
	}
	if s.Audio != nil {
		s.Frames = int(s.Audio.DurationSec * float64(s.FPS))
	} else {
		s.Frames = int(silentDuration * float64(s.FPS))
	}
	s.silent = nil

	s.Cfg = cfg
	s.baseKey = s.keyFor(&cfg.Timeline.Visualizer[0].Cfg)
	if _, err := s.featuresLocked(s.baseKey); err != nil {
		return err
	}

	// Letra: se reconstruye solo si cambia su pista
	lk := ""
	if b, err := yaml.Marshal(cfg.Timeline.Lyrics); err == nil {
		lk = fmt.Sprintf("%dx%d|%s", w, h, b)
		if st, err := os.Stat(cfg.Timeline.Lyrics[0].Cfg.Path); err == nil {
			lk += st.ModTime().String()
		}
	}
	if lk != s.lyrKey {
		s.lyr = nil
		anyOn := false
		for _, sg := range cfg.Timeline.Lyrics {
			anyOn = anyOn || sg.Cfg.Enabled
		}
		if anyOn && cfg.Timeline.Lyrics[0].Cfg.Path != "" {
			t0 := time.Now()
			m, err := lyrics.NewMulti(cfg.Timeline.Lyrics, w, h)
			if err != nil {
				return err
			}
			s.lyr = m
			s.Logf("🎤 Letra: atlas %dx%d (%v)", m.Atlas.W, m.Atlas.H, time.Since(t0).Round(time.Millisecond))
		}
		s.lyrKey = lk
		s.LyrVer++
	}
	s.sim = particleSim{last: -1}
	return nil
}

func (s *Session) keyFor(v *config.VisualizerConfig) anaKey {
	a := &s.Cfg.Audio
	return anaKey{bars: v.Bars, smoothing: v.Smoothing, decay: v.DecayRate, sens: a.Sensitivity, normalize: a.Normalize, window: a.NormalizeWindow}
}

// featuresLocked devuelve (calculándolo una vez) el análisis para unos parámetros de barras
func (s *Session) featuresLocked(k anaKey) ([]audio.FrameFeatures, error) {
	if f, ok := s.feats[k]; ok {
		return f, nil
	}
	if s.Audio == nil {
		if s.silent == nil {
			s.silent = make([]audio.FrameFeatures, s.Frames)
		}
		n := k.bars
		for i := range s.silent {
			if len(s.silent[i].LeftBars) != n {
				s.silent[i].LeftBars, s.silent[i].RightBars = make([]float64, n), make([]float64, n)
			}
		}
		return s.silent, nil
	}
	t0 := time.Now()
	an := audio.NewAudioAnalyzer(s.Audio, s.FPS, k.bars, k.sens, k.smoothing, k.decay)
	an.Normalize = k.normalize
	an.WindowSec = k.window
	f := an.AnalyzeAllFrames(s.Frames)
	s.feats[k] = f
	s.Logf("⚡ Análisis (%d bandas, %d cuadros) en %v", k.bars, s.Frames, time.Since(t0).Round(time.Millisecond))
	return f, nil
}

// Features devuelve el análisis base (energías por cuadro) para el estudio
func (s *Session) Features() []audio.FrameFeatures {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, _ := s.featuresLocked(s.baseKey)
	return f
}

func (s *Session) mediaFor(bg *config.BackgroundConfig) (video.BackgroundReader, error) {
	if bg.Path == "" {
		return nil, nil
	}
	key := fmt.Sprintf("%s|%s|%v", bg.Path, bg.FitMode, bg.LoopVideo)
	if r, ok := s.media[key]; ok {
		return r, nil
	}
	r, err := video.NewBackgroundReader(bg.Path, s.W, s.H, bg.FitMode, bg.LoopVideo)
	if err != nil {
		return nil, err
	}
	s.media[key] = r
	return r, nil
}

// ---------------------------------------------------------------- estado de un cuadro

// FrameState es todo lo necesario para dibujar un cuadro, ya resuelto en la línea de tiempo
type FrameState struct {
	Idx  int
	T    float64
	Feat *audio.FrameFeatures // energías (análisis base)

	FX frameFX
	BG config.BackgroundConfig // oscurecer/opacidad/viñeta (interpolados)
	// Imágenes de fondo: A siempre; B y Mix durante un fundido cruzado. Ver* identifica la imagen
	// para no volver a subirla a la GPU si no ha cambiado.
	BgA, BgB     *image.RGBA
	VerA, VerB   string
	BgMix        float64
	GlitchAmount float64

	Viz      config.VisualizerConfig
	VizFeat  *audio.FrameFeatures
	VizAlpha float64

	Parts []*particles.ParticleSystem
	Lyr   *lyrics.Multi
	LyrSt config.LyricsConfig
}

func lerp(a, b, k float64) float64 { return a + (b-a)*k }

// Prepare calcula el estado del cuadro idx. Las partículas avanzan de forma continua si los
// cuadros se piden en orden; si se salta en el tiempo se re-simulan unos segundos antes.
func (s *Session) Prepare(idx int) (*FrameState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.Cfg
	tl := &cfg.Timeline
	idx = max(0, min(idx, s.Frames-1))
	t := float64(idx) / float64(s.FPS)
	base, err := s.featuresLocked(s.baseKey)
	if err != nil {
		return nil, err
	}
	fs := &FrameState{Idx: idx, T: t, Feat: &base[idx]}

	// Efectos del fondo: se mezclan sus resultados (así activar/desactivar también se funde)
	fs.FX, fs.GlitchAmount = s.effectsAt(t, idx, fs.Feat)

	// Fondo (con fundido cruzado entre medios)
	bi := config.Active(tl.Background, t)
	pbg, cbg, k := config.Blend(tl.Background, t)
	fs.BG = config.Lerp(pbg, cbg, k)
	ra, err := s.mediaFor(cbg)
	if err != nil {
		return nil, err
	}
	if ra != nil {
		img, ver := ra.FrameAt(t - tl.Background[bi].At)
		fs.BgA, fs.VerA = img, fmt.Sprintf("%p:%d", ra, ver)
	}
	if k < 1 && pbg.Path != cbg.Path {
		rb, err := s.mediaFor(pbg)
		if err != nil {
			return nil, err
		}
		if rb != nil {
			img, ver := rb.FrameAt(t - tl.Background[bi-1].At)
			// A = anterior, B = nuevo, Mix = k
			fs.BgB, fs.VerB = fs.BgA, fs.VerA
			fs.BgA, fs.VerA = img, fmt.Sprintf("%p:%d", rb, ver)
			fs.BgMix = k
		}
	}

	// Visualizador: mismo tipo -> se interpola; cambio de tipo -> se apaga uno y se enciende el otro
	pv, cv, kv := config.Blend(tl.Visualizer, t)
	fs.VizAlpha = 1
	switch {
	case kv >= 1:
		fs.Viz = *cv
	case pv.Type != cv.Type || pv.Bars != cv.Bars || pv.Enabled != cv.Enabled:
		if kv < 0.5 {
			fs.Viz, fs.VizAlpha = *pv, 1-2*kv
		} else {
			fs.Viz, fs.VizAlpha = *cv, 2*kv-1
		}
	default:
		fs.Viz = config.Lerp(pv, cv, kv)
	}
	vf, err := s.featuresLocked(s.keyFor(&fs.Viz))
	if err != nil {
		return nil, err
	}
	fs.VizFeat = &vf[idx]

	// Partículas
	s.stepParticles(idx, base)
	for _, e := range s.sim.entries {
		fs.Parts = append(fs.Parts, e.ps)
	}

	// Letra
	if s.lyr != nil {
		pl, cl, kl := config.Blend(tl.Lyrics, t)
		fs.Lyr, fs.LyrSt = s.lyr, config.Lerp(pl, cl, kl)
	}
	return fs, nil
}

// ---------------------------------------------------------------- efectos de fondo

type fxPart struct {
	zoom, shakeX, shakeY, shakeZoom float64
	rippleAmp, rippleFreq, rTim     float64
	chroma                          float64
	chromaMode                      int
	glitch                          bool
	glitchAmt                       float64
	bright, contrast                float64
}

func (s *Session) effectsAt(t float64, idx int, feat *audio.FrameFeatures) (frameFX, float64) {
	tl := &s.Cfg.Timeline
	minDim := math.Max(math.Min(float64(s.W), float64(s.H)), 64)
	var fx frameFX
	fx.zoom = 1

	// Zoom
	{
		a, b, k := config.Blend(tl.BassZoom, t)
		z := func(c *config.BassZoomConfig) float64 {
			if !c.Enabled {
				return 1
			}
			v := 1 + (c.MaxZoom-1)*feat.Band(c.ReactTo, "bass")*c.Sensitivity
			if feat.BeatPulse > 0 {
				v += (c.MaxZoom - 1) * feat.BeatPulse * 0.5
			}
			return v
		}
		fx.zoom = lerp(z(a), z(b), k)
	}
	// Temblor
	{
		a, b, k := config.Blend(tl.Shake, t)
		sh := func(c *config.ShakeConfig) (float64, float64, float64) {
			if !c.Enabled || c.Amplitude <= 0 {
				return 0, 0, 1
			}
			sp := c.Speed
			if sp <= 0 {
				sp = 1
			}
			tt := t * sp
			amp := c.Amplitude * feat.Band(c.ReactTo, "bass")
			return amp * (0.6*math.Sin(tt*7.3) + 0.4*math.Sin(tt*13.1+1.7)),
				amp * (0.6*math.Sin(tt*8.9+0.5) + 0.4*math.Sin(tt*11.7+2.3)),
				1 + 2*c.Amplitude/minDim
		}
		ax, ay, az := sh(a)
		bx, by, bz := sh(b)
		fx.shakeX, fx.shakeY = lerp(ax, bx, k), lerp(ay, by, k)
		fx.zoom *= lerp(az, bz, k)
	}
	// Ondulación
	{
		a, b, k := config.Blend(tl.Ripple, t)
		r := func(c *config.RippleConfig) (float64, float64, float64) {
			if !c.Enabled {
				return 0, c.Frequency, 0
			}
			return c.Amplitude * (0.3 + feat.Band(c.ReactTo, "mid")*0.7), c.Frequency, float64(idx) * 0.05 * c.Speed
		}
		aa, af, at := r(a)
		ba, bf, bt := r(b)
		fx.rippleAmp, fx.rippleFreq, fx.rTim = lerp(aa, ba, k), lerp(af, bf, k), lerp(at, bt, k)
		if !a.Enabled {
			fx.rippleFreq, fx.rTim = bf, bt
		} else if !b.Enabled {
			fx.rippleFreq, fx.rTim = af, at
		}
	}
	// Aberración cromática
	{
		a, b, k := config.Blend(tl.Chromatic, t)
		ch := func(c *config.ChromaticAberrationConfig) (float64, int) {
			if !c.Enabled {
				return 0, 0
			}
			m := 1
			if c.Mode == "radial" {
				m = 2
			}
			return c.MaxShift * feat.Band(c.ReactTo, "bass"), m
		}
		as, am := ch(a)
		bs, bm := ch(b)
		fx.chromaShift = lerp(as, bs, k)
		fx.chromaMode = bm
		if bm == 0 || (k < 0.5 && am != 0) {
			fx.chromaMode = am
		}
	}
	// Glitch
	glitchAmt := 0.0
	{
		a, b, k := config.Blend(tl.Glitch, t)
		on := func(c *config.GlitchConfig) bool {
			if !c.Enabled {
				return false
			}
			if c.ReactTo == "" {
				return feat.SpectralFlux > c.Threshold || feat.IsBeat
			}
			return feat.Band(c.ReactTo, "") > c.Threshold
		}
		c := a
		if k >= 0.5 {
			c = b
		}
		fx.glitch = on(c)
		glitchAmt = c.Intensity
	}
	// Flash de color
	{
		a, b, k := config.Blend(tl.ColorFlash, t)
		fl := func(c *config.ColorFlashConfig) (float64, float64) {
			if !c.Enabled {
				return 0, 0
			}
			v := feat.Band(c.ReactTo, "bass")
			return c.BrightnessBoost * v, c.ContrastBoost * v
		}
		ab, ac := fl(a)
		bb, bc := fl(b)
		fx.bright, fx.contrast = lerp(ab, bb, k), lerp(ac, bc, k)
	}
	return fx, glitchAmt
}

// ---------------------------------------------------------------- partículas

type partEntry struct {
	seg int
	ps  *particles.ParticleSystem
}

type particleSim struct {
	last    int
	entries []partEntry
}

func (s *Session) stepParticles(idx int, feats []audio.FrameFeatures) {
	from := s.sim.last + 1
	if s.sim.last < 0 || idx < s.sim.last || idx-s.sim.last > 600 {
		// Salto en el tiempo: re-simular desde unos segundos antes (las partículas viven < 5 s)
		s.sim = particleSim{last: -1}
		from = max(0, idx-5*s.FPS)
	}
	for i := from; i <= idx; i++ {
		s.stepParticle(i, &feats[i], i == from && s.sim.last < 0)
	}
	s.sim.last = idx
}

func (s *Session) stepParticle(i int, feat *audio.FrameFeatures, fresh bool) {
	segs := s.Cfg.Timeline.Particles
	t := float64(i) / float64(s.FPS)
	cur := config.Active(segs, t)
	found := false
	for _, e := range s.sim.entries {
		found = found || e.seg == cur
	}
	if !found {
		var ps *particles.ParticleSystem
		if fresh {
			ps = particles.NewParticleSystem(&segs[cur].Cfg, s.W, s.H)
		} else {
			ps = particles.NewEmptyParticleSystem(&segs[cur].Cfg, s.W, s.H, int64(1337+cur))
		}
		for j := range s.sim.entries {
			s.sim.entries[j].ps.Draining = true
		}
		s.sim.entries = append(s.sim.entries, partEntry{seg: cur, ps: ps})
	}
	sg := segs[cur]
	k := 1.0
	if sg.Transition.Mode == "fade" && sg.Transition.Duration > 0 && t < sg.At+sg.Transition.Duration {
		k = math.Max(0, (t-sg.At)/sg.Transition.Duration)
	}
	alive := s.sim.entries[:0]
	for _, e := range s.sim.entries {
		if e.seg == cur {
			e.ps.Draining = false
			e.ps.Alpha = k
		} else {
			e.ps.Alpha = 1 - k
			if sg.Transition.Mode != "fade" {
				e.ps.Alpha = 1
			}
		}
		e.ps.Update(feat, 1/float64(s.FPS))
		if e.seg == cur || (e.ps.Alive() && e.ps.Alpha > 0) {
			alive = append(alive, e)
		}
	}
	s.sim.entries = alive
}

// Fork crea una sesión independiente con una copia de la config actual que reutiliza el audio
// y los análisis ya calculados (para exportar mientras se sigue editando)
func (s *Session) Fork() (*Session, error) {
	s.mu.Lock()
	b, err := s.Cfg.Marshal()
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	cfg, err := config.ParseConfig(b, s.Cfg.Dir)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	ns := &Session{Logf: s.Logf, feats: map[anaKey][]audio.FrameFeatures{}, media: map[string]video.BackgroundReader{},
		Audio: s.Audio, audioKey: s.audioKey, W: s.W, H: s.H, FPS: s.FPS}
	for k, v := range s.feats {
		ns.feats[k] = v
	}
	s.mu.Unlock()
	if err := ns.Update(cfg); err != nil {
		return nil, err
	}
	return ns, nil
}

// Duration devuelve la duración de la línea de tiempo en segundos
func (s *Session) Duration() float64 { return float64(s.Frames) / float64(s.FPS) }
