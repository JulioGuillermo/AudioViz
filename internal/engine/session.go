package engine

import (
	"fmt"
	"image"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"audioviz/internal/audio"
	"audioviz/internal/config"
	"audioviz/internal/depth"
	"audioviz/internal/effects"
	"audioviz/internal/lyrics"
	"audioviz/internal/particles"
	"audioviz/internal/video"

	"gopkg.in/yaml.v3"
)

// Session is a loaded project, ready to render any frame (preview) or
// export it whole. Shared by the CLI and the studio.
type Session struct {
	Cfg    *config.ProjectConfig
	W, H   int
	FPS    int
	Frames int
	Audio  *audio.DecodedAudio

	Logf func(format string, args ...any)

	mu       sync.Mutex
	audioKey string
	feats    map[anaKey][]audio.FrameFeatures
	baseKey  anaKey
	media    map[string]video.BackgroundReader
	// layerMedia: the files of the overlay clips (layers.go); masks: their combined masks
	layerMedia map[string]*layerMedia
	masks      map[string]maskEntry
	emitTabs   map[string]*emitTable // births of the emitter clips (emitters.go)
	textPts    map[string][]float32  // points on the letters of the texts emitters are born on
	stems      []audio.StemFrame     // the separated song per frame (stems.go), nil until separated
	stemsKey   string
	depths     map[string]*depthEntry // depth maps of the backgrounds, fitted to the frame (depth.go)
	depthSeqs  map[string]*depth.Seq  // a video's depth on every frame (nil: failed)
	// Live: the studio's preview: it never waits for a video's depth on every frame to be worked
	// out (an export, a fork, does)
	Live      bool
	maskSeq   int
	lyr       *lyrics.Multi
	lyrKey    string
	LyrVer    int // changes every time the lyrics atlas is rebuilt
	sim       particleSim
	fluids    map[string]*effects.Fluid // simulated fluid transitions (see fluidFor)
	exprCache map[string]exprEntry      // the expressions compiled (exprs.go)
	exprFeats []audio.FrameFeatures     // the analysis the expressions read (the frame being made)
	silent    []audio.FrameFeatures     // when the project has no audio yet
}

type anaKey struct {
	bars                   int
	smoothing, decay, sens float64
	normalize              bool
	window                 float64
}

const silentDuration = 30.0 // seconds of timeline when there is no audio

// Open loads the project: audio, analysis, backgrounds and lyrics
func Open(cfg *config.ProjectConfig, logf func(string, ...any)) (*Session, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	s := &Session{Logf: logf, feats: map[anaKey][]audio.FrameFeatures{}, media: map[string]video.BackgroundReader{},
		layerMedia: map[string]*layerMedia{}, masks: map[string]maskEntry{}}
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
	s.closeLayerMedia()
}

func (s *Session) closeLayerMedia() {
	for k, m := range s.layerMedia {
		m.r.Close()
		delete(s.layerMedia, k)
	}
}

// Update applies a new config, reusing everything that hasn't changed (audio, analysis, backgrounds)
func (s *Session) Update(cfg *config.ProjectConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exprCache = nil // the expressions and the library may have changed

	// Resolution: the output one or that of the first background
	w, h := cfg.Output.Width, cfg.Output.Height
	if w <= 0 || h <= 0 {
		w, h = 1080, 1920
		if p := cfg.Timeline.Background[0].Cfg.Path; p != "" {
			probe, err := video.ProbeMedia(p)
			if err != nil {
				return fmt.Errorf("background: %w", err)
			}
			w, h = probe.Width, probe.Height
		}
	}
	w, h = max(w/2*2, 16), max(h/2*2, 16)
	if w != s.W || h != s.H {
		// Backgrounds are scaled to the resolution: they must be reloaded
		for k, r := range s.media {
			r.Close()
			delete(s.media, k)
		}
		s.closeLayerMedia()
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
	s.loadStemsLocked()

	// Lyrics: rebuilt only if their track changes
	lk := ""
	if b, err := yaml.Marshal(struct {
		L any
		T any
	}{cfg.Timeline.Lyrics, cfg.Texts}); err == nil {
		lk = fmt.Sprintf("%dx%d|%s", w, h, b)
		if st, err := os.Stat(cfg.Timeline.Lyrics[0].Cfg.Path); err == nil {
			lk += st.ModTime().String()
		}
	}
	if lk != s.lyrKey {
		s.lyr = nil
		t0 := time.Now()
		m, err := lyrics.NewMulti(cfg.Timeline.Lyrics, cfg.Texts, w, h, s)
		if err != nil {
			return err
		}
		if m != nil {
			m.Ev = s
			s.lyr = m
			s.Logf("🎤 Lyrics and texts: atlas %dx%d (%v)", m.Atlas.W, m.Atlas.H, time.Since(t0).Round(time.Millisecond))
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

// featuresLocked returns (computing it once) the analysis for a set of bar parameters
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
	s.attachStems(f)
	s.feats[k] = f
	s.Logf("⚡ Analysis (%d bands, %d frames) in %v", k.bars, s.Frames, time.Since(t0).Round(time.Millisecond))
	return f, nil
}

// Features returns the base analysis (per-frame energies) for the studio
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

// ---------------------------------------------------------------- frame state

// motionAt places background segment i at time t. Its motion spans the whole time the segment
// is visible: from its start until the next segment has fully entered (its transition included).
func (s *Session) motionAt(i int, t float64) effects.Layer {
	segs := s.Cfg.Timeline.Background
	sg := segs[i]
	end := float64(s.Frames) / float64(s.FPS)
	if i+1 < len(segs) {
		n := segs[i+1]
		end = n.At
		if n.Transition.Animated() {
			end += n.Transition.Duration
		}
	}
	span := math.Max(end-sg.At, 1e-3)
	return effects.MotionAt(sg.CfgAtEv(t, config.SegDur(segs, i, s.Duration()), s, "motion"+strconv.Itoa(i)).Motion, (t-sg.At)/span, t-sg.At, s.W, s.H, i)
}

// FrameState is everything needed to draw a frame, already resolved on the timeline
type FrameState struct {
	Idx  int
	T    float64
	Feat *audio.FrameFeatures // energies (base analysis)

	FX frameFX
	BG config.BackgroundConfig // darken/opacity/vignette (interpolated)
	// Background images: A always (the previous one during a transition), B the new one during
	// a transition. Ver* identifies the image so it isn't re-uploaded to the GPU if unchanged.
	// Layer* place each image (segment motion); Trans says how B replaces A.
	BgA, BgB   *image.RGBA
	VerA, VerB string
	// Depth of the backgrounds A and B (fitted to the frame, 16-bit nearness; nil = none)
	DepthA, DepthB []uint16
	NormA, NormB   []uint16 // their surface normals (effects.NormalMap)
	VerDA, VerDB   string
	LayerA, LayerB effects.Layer
	Trans          effects.Trans
	GlitchAmount   float64

	Viz      config.VisualizerConfig
	VizFeat  *audio.FrameFeatures
	VizAlpha float64

	Parts []*particles.ParticleSystem
	Lyr   *lyrics.Multi
	LyrSt config.LyricsConfig
	// The texts on screen (lyrics and fixed texts) and their styles, with their masks made
	TextItems  []lyrics.Item
	TextStyles []lyrics.Style
	// The texts that light the scene: their style and light field
	TextLights []TextLight
	// The depth of the frame (backgrounds with a depth map; nil without)
	Scene *effects.Scene

	// Overlay clips on screen, bottom to top, and where the fixed elements go among them: stage
	// k = right above the k-th of them (0 = above the background; len(Layers) = above all)
	Layers               []effects.LayerFrame
	layerStack           []int // layer index + 1 of each clip (to resolve z_index)
	ZParts, ZViz, ZLyric int
}

// Stage of a z_index among the frame's clips: above the clips whose layer is at or below z
func (fs *FrameState) Stage(z *int) int {
	if z == nil {
		return len(fs.Layers)
	}
	n := 0
	for _, li := range fs.layerStack {
		if li <= *z {
			n++
		}
	}
	return n
}

func lerp(a, b, k float64) float64 { return a + (b-a)*k }

// Prepare computes the state of frame idx. Particles advance continuously if frames
// are requested in order; on a time jump they are re-simulated from a few seconds earlier.
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
	s.exprFeats = base
	fs := &FrameState{Idx: idx, T: t, Feat: &base[idx]}

	// Background effects: their results are blended (so enabling/disabling also fades)
	fs.FX, fs.GlitchAmount = s.effectsAt(t, idx, fs.Feat)

	// Background: each segment's image with its motion; during a transition the previous one
	// (A) and the new one (B) are both drawn and mixed by the transition
	bi := config.Active(tl.Background, t)
	pbg, cbg, k := config.BlendEv(tl.Background, t, s.Duration(), s, "Background")
	fs.BG = config.Lerp(pbg, cbg, k)
	ra, err := s.mediaFor(cbg)
	if err != nil {
		return nil, err
	}
	if ra != nil {
		img, ver := ra.FrameAt(t - tl.Background[bi].At)
		fs.BgA, fs.VerA = img, fmt.Sprintf("%p:%d", ra, ver)
	}
	fs.DepthA, fs.NormA, fs.VerDA = s.depthFor(cbg, t-tl.Background[bi].At)
	fs.LayerA = s.motionAt(bi, t)
	fs.Trans = effects.Trans{HasA: fs.BgA != nil}
	if k < 1 {
		rb, err := s.mediaFor(pbg)
		if err != nil {
			return nil, err
		}
		fs.BgB, fs.VerB, fs.LayerB = fs.BgA, fs.VerA, fs.LayerA
		fs.BgA, fs.VerA = nil, ""
		fs.DepthB, fs.NormB, fs.VerDB = fs.DepthA, fs.NormA, fs.VerDA
		fs.DepthA, fs.NormA, fs.VerDA = s.depthFor(pbg, t-tl.Background[bi-1].At)
		if rb != nil {
			img, ver := rb.FrameAt(t - tl.Background[bi-1].At)
			fs.BgA, fs.VerA = img, fmt.Sprintf("%p:%d", rb, ver)
		}
		fs.LayerA = s.motionAt(bi-1, t)
		tr := tl.Background[bi].Transition
		seed := effects.Hash(uint32(bi)) & 0x7fffffff
		if tr.Burn != nil && tr.Burn.Seed > 0 {
			seed = uint32(tr.Burn.Seed) & 0x7fffffff
		}
		fs.Trans = effects.Trans{Mode: tr.ModeIndex(), Dir: tr.DirectionIndex(), K: k,
			Seed: seed, HasA: fs.BgA != nil, HasB: fs.BgB != nil}
		if tr.Mode == "burn" {
			fs.Trans.Burn = effects.BurnSetup(tr.Burn, seed, s.W, s.H)
		}
		if (tr.Mode == "fluid" || tr.Mode == "liquid") && fs.BgA != nil && fs.BgB != nil {
			// Simulated once per transition (the first frame that needs it); each frame only
			// places the particles, and the CPU or the GPU draws them over both images
			fs.Trans.Fluid = s.fluidFor(bi, tr, seed, fs.BgA, fs.BgB).Frame(k)
		}
	}

	// With a depth map: the zoom's share that moves in goes to the camera, which hides the frame's
	// edges with a little zoom; without one the zoom is flat and nothing depends on depth
	if fs.DepthA != nil || fs.DepthB != nil {
		fs.FX.par.Dolly += fs.FX.zoomDolly
		fs.FX.zoom *= fs.FX.par.EdgeZoom(s.W, s.H)
	} else {
		fs.FX.zoom *= 1 + fs.FX.zoomDolly
		fs.FX.par, fs.FX.dof = effects.Parallax{}, effects.DoF{}
		fs.FX.rFlash, fs.FX.rChroma, fs.FX.rRipple, fs.FX.rGlitch = [4]float64{}, [4]float64{}, [4]float64{}, [4]float64{}
	}
	fs.FX.zoomDolly = 0

	// Visualizer: same type -> interpolate; type change -> one fades out and the other fades in
	pv, cv, kv := config.BlendEv(tl.Visualizer, t, s.Duration(), s, "Visualizer")
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

	// Particles
	s.stepParticles(idx, base)
	for _, e := range s.sim.entries {
		fs.Parts = append(fs.Parts, e.ps)
	}

	// Lyrics
	if s.lyr != nil {
		pl, cl, kl := config.BlendEv(tl.Lyrics, t, s.Duration(), s, "Lyrics")
		fs.Lyr, fs.LyrSt = s.lyr, config.Lerp(pl, cl, kl)
		fs.TextItems, fs.TextStyles = s.lyr.Frame(t, fs.Feat, &fs.LyrSt)
		for i := range fs.TextStyles {
			st := &fs.TextStyles[i]
			if len(st.Masks) > 0 {
				st.Mask, st.MaskVer = s.maskFor(st.Masks, s.W, s.H)
				if st.Mask != nil {
					st.MaskSX, st.MaskSY = float64(st.Mask.Rect.Dx())/float64(s.W), float64(st.Mask.Rect.Dy())/float64(s.H)
				}
			}
		}
	}

	// Overlay layers, and the place of the visualizer, particles and lyrics among them
	if fs.DepthA != nil || fs.DepthB != nil {
		fx := fs.FX
		if fs.Trans.Fluid != nil {
			fx.par = effects.Parallax{} // the fluid transitions don't move
		}
		fs.Scene = &effects.Scene{A: fs.DepthA, B: fs.DepthB, NA: fs.NormA, NB: fs.NormB, LA: fs.LayerA, LB: fs.LayerB, K: fs.Trans.K, Tr: fs.Trans,
			Zoom: fx.zoom, ShakeX: fx.shakeX, ShakeY: fx.shakeY, RippleAmp: fx.rippleAmp, RippleFreq: fx.rippleFreq, RippleTm: fx.rTim,
			W: s.W, H: s.H, Par: fx.par, N: fx.par.Steps(s.W, s.H), DoF: fx.dof}
	}
	if fs.Scene.On() && len(fs.TextStyles) > 0 {
		s.textsInDepth(fs)
	}
	if fs.Lyr != nil {
		s.textLights(fs)
	}
	if len(cfg.Layers) > 0 {
		if fs.Layers, fs.layerStack, err = s.layersAt(t, idx, base, fs.FX, fs.GlitchAmount, fs.Scene); err != nil {
			return nil, err
		}
	}
	fs.ZViz = fs.Stage(fs.Viz.ZIndex)
	fs.ZParts = fs.Stage(config.SegAtEv(tl.Particles, t, s.Duration(), s, "particles").ZIndex)
	fs.ZLyric = fs.Stage(fs.LyrSt.ZIndex)
	return fs, nil
}

// TextLight is a text (or the lyrics) lighting the scene: its style and its light field
type TextLight struct {
	Style int
	Light *effects.LightField
}

// textCenter: where a text style is: a fixed text's anchor, the middle of the lyrics' words now
func textCenter(fs *FrameState, si int) (float64, float64, bool) {
	st := &fs.TextStyles[si]
	if st.Text >= 0 {
		return float64(st.AnchorX), float64(st.AnchorY), true
	}
	ax, ay, n := 0.0, 0.0, 0
	for _, it := range fs.TextItems {
		if it.Style == si {
			ax, ay, n = ax+float64(it.CX), ay+float64(it.CY), n+1
		}
	}
	if n == 0 {
		return 0, 0, false
	}
	return ax / float64(n), ay / float64(n), true
}

// textLights: the light of the texts that light the scene, from their letters now (all of them);
// with a depth map in 2.5D from the text's place and depth
func (s *Session) textLights(fs *FrameState) {
	div := effects.TextLightDiv(s.W, s.H)
	gw, gh := (s.W+div-1)/div, (s.H+div-1)/div
	k := float64(s.H) / 1080
	for si := range fs.TextStyles {
		st := &fs.TextStyles[si]
		if !st.LightOn {
			continue
		}
		src := fs.Lyr.LightSources(fs.TextItems, si, div, gw, gh)
		lf := &effects.LightField{GW: gw, GH: gh, Div: div, Add: st.LightAdd,
			V:   effects.TextLight(src, gw, gh, div, float32(st.LightR*k), float32(st.LightE*k)),
			Col: [3]float32{float32(st.LightCol[0]), float32(st.LightCol[1]), float32(st.LightCol[2])}}
		l := &st.Light
		from, to, soft := l.From, l.To, l.RangeSoft
		if from == 0 && to == 0 {
			from = 1
		}
		if soft <= 0 {
			soft = 0.05
		}
		if sc := fs.Scene; sc.On() && (st.LightDepthK > 0 || st.LightShade > 0 || from < 1+soft || to > -soft || l.Rim || l.Gloss > 0 || l.Shadows > 0) {
			if cx, cy, ok := textCenter(fs, si); ok {
				z := float64(sc.Ground(float32(cx), float32(cy)))
				if st.InDepth {
					z = st.DepthZ
				}
				lf.D3, lf.CX, lf.CY, lf.LZ = true, float32(cx), float32(cy), float32(z)
				lf.DepthK, lf.Shade = float32(st.LightDepthK), float32(st.LightShade)
				lf.Gloss, lf.Shadow = float32(l.Gloss), float32(l.Shadows)
				lf.From, lf.To, lf.Soft = float32(from), float32(to), float32(soft)
				if l.Rim {
					pos := func(v, def float64) float64 {
						if v > 0 {
							return v
						}
						return def
					}
					lf.Rim, lf.RimFrom, lf.RimSoft = true, float32(l.RimFrom*k), float32(math.Max(l.RimSoft, 1)*k)
					lf.RimW, lf.RimGain, lf.RimEdge = float32(pos(l.RimWidth, 10)*k), float32(pos(l.RimGain, 2)), float32(pos(l.RimEdge, 0.04))
				}
			}
		}
		fs.TextLights = append(fs.TextLights, TextLight{si, lf})
	}
}

// textsInDepth: the lyrics and texts that have a depth: where it is (fixed, or the scene's at
// their place: a fixed text's anchor, the middle of the lyrics' words now), hidden behind what
// is nearer if they ask, moved by the 2.5D camera and blurred by the depth of field
func (s *Session) textsInDepth(fs *FrameState) {
	sc := fs.Scene
	for si := range fs.TextStyles {
		st := &fs.TextStyles[si]
		if st.ZTrack != nil && sc.On() {
			// its depth comes from its path, per point (blurred and moved per point when drawn)
			d := pathDepthSettings(st.Depth)
			st.InDepth, st.Behind, st.Scene = true, d.Behind, sc
			st.DepthSoft = math.Max(d.Soft, 0.005)
			n := 0
			for _, it := range fs.TextItems {
				if it.Style == si {
					st.DepthZ += float64(st.ZTrack.At(it.PS))
					n++
				}
			}
			st.DepthZ /= float64(max(n, 1))
			st.ZMargin = sc.Par.MaxDisp(s.W, s.H) + sc.DoF.Blur
			continue
		}
		st.ZTrack = nil
		if st.Depth.Mode == "" || st.Depth.Mode == "off" {
			continue
		}
		ax, ay := float64(st.AnchorX), float64(st.AnchorY)
		if st.Text < 0 {
			n := 0
			ax, ay = 0, 0
			for _, it := range fs.TextItems {
				if it.Style == si {
					ax, ay, n = ax+float64(it.CX), ay+float64(it.CY), n+1
				}
			}
			if n == 0 {
				continue
			}
			ax, ay = ax/float64(n), ay/float64(n)
		}
		z, ok := clipDepth(&st.Depth, sc, ax, ay)
		if !ok {
			continue
		}
		st.InDepth, st.Behind, st.Scene = true, st.Depth.Behind, sc
		st.DepthZ, st.DepthSoft = z, math.Max(st.Depth.Soft, 0.005)
		if sc.DoF.On() {
			st.DepthBlur = float64(sc.DoF.CoC(float32(z)))
		}
		if sc.Par.On() {
			dx, dy := sc.Par.Disp(z, ax, ay, s.W, s.H)
			st.AnchorX, st.AnchorY = st.AnchorX+float32(dx), st.AnchorY+float32(dy)
			for i := range fs.TextItems {
				if fs.TextItems[i].Style == si {
					fs.TextItems[i].CX += float32(dx)
					fs.TextItems[i].CY += float32(dy)
				}
			}
		}
	}
}

// ---------------------------------------------------------------- background effects

// One function per background effect, from one config block: shared by the background (its
// two segments in a fade) and the overlay layers' own effects
func zoomOf(c *config.BassZoomConfig, feat *audio.FrameFeatures) float64 {
	if !c.Enabled {
		return 1
	}
	v := 1 + (c.MaxZoom-1)*feat.Band(c.ReactTo, "bass")*c.Sensitivity
	if feat.BeatPulse > 0 {
		v += (c.MaxZoom - 1) * feat.BeatPulse * 0.5
	}
	return v
}

// shakeOf: displacement x, y and the zoom that hides the edges
func shakeOf(c *config.ShakeConfig, t float64, feat *audio.FrameFeatures, minDim float64) (float64, float64, float64) {
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

// zoomDepth: how much of a zoom is the camera moving in
func zoomDepth(c *config.BassZoomConfig) float64 {
	if !c.Enabled {
		return 0
	}
	return math.Max(0, math.Min(1, c.Depth))
}

// dofOf: the depth of field now: the band brings React of the blur and moves the focus
func dofOf(c *config.DepthOfFieldConfig, feat *audio.FrameFeatures) effects.DoF {
	if !c.Enabled || c.Blur <= 0 {
		return effects.DoF{From: c.From, To: c.To, Soft: c.Soft}
	}
	b := feat.Band(c.ReactTo, "bass")
	r := math.Max(0, math.Min(1, c.React))
	m := c.FocusMove * b
	return effects.DoF{From: c.From + m, To: c.To + m, Soft: c.Soft, Blur: c.Blur * ((1 - r) + r*b)}
}

// parallaxOf: the 2.5D camera now: how far the nearest things move (px), moving in, focus
func parallaxOf(c *config.ParallaxConfig, t float64, feat *audio.FrameFeatures) effects.Parallax {
	if !c.Enabled || (c.Amount == 0 && c.Dolly == 0) {
		return effects.Parallax{Focus: c.Focus}
	}
	sp := c.Speed
	if sp <= 0 {
		sp = 1
	}
	tt := t * sp
	mx, my, dolly := c.X, c.Y, c.Dolly
	switch c.Motion {
	case "beat":
		// towards X, Y and in, with the music; back at rest
		b := feat.Band(c.ReactTo, "bass")
		mx, my, dolly = mx*b, my*b, dolly*b
	case "still":
	default: // sway: a slow drift around X, Y, moving in and out a little
		mx += 0.8*math.Sin(tt*0.45+0.3) + 0.2*math.Sin(tt*1.1)
		my += 0.45*math.Sin(tt*0.31+1.1) + 0.1*math.Sin(tt*0.9+2)
		dolly *= 0.5 + 0.5*math.Sin(tt*0.23)
	}
	if c.Motion != "beat" && c.React > 0 {
		// the music pushes it further
		k := 1 + math.Min(c.React, 1)*feat.Band(c.ReactTo, "bass")
		mx, my, dolly = mx*k, my*k, dolly*k
	}
	return effects.Parallax{SX: -mx * c.Amount, SY: -my * c.Amount, Dolly: dolly, Focus: c.Focus}
}

// rippleOf: amplitude, frequency and phase
func rippleOf(c *config.RippleConfig, idx int, feat *audio.FrameFeatures) (float64, float64, float64) {
	if !c.Enabled {
		return 0, c.Frequency, 0
	}
	return c.Amplitude * (0.3 + feat.Band(c.ReactTo, "mid")*0.7), c.Frequency, float64(idx) * 0.05 * c.Speed
}

// chromaOf: shift (px) and mode (0 none, 1 horizontal, 2 radial)
func chromaOf(c *config.ChromaticAberrationConfig, feat *audio.FrameFeatures) (float64, int) {
	if !c.Enabled {
		return 0, 0
	}
	m := 1
	if c.Mode == "radial" {
		m = 2
	}
	return c.MaxShift * feat.Band(c.ReactTo, "bass"), m
}

func glitchOn(c *config.GlitchConfig, feat *audio.FrameFeatures) bool {
	if !c.Enabled {
		return false
	}
	if c.ReactTo == "" {
		return feat.SpectralFlux > c.Threshold || feat.IsBeat
	}
	return feat.Band(c.ReactTo, "") > c.Threshold
}

// flashOf: brightness and contrast boosts
func flashOf(c *config.ColorFlashConfig, feat *audio.FrameFeatures) (float64, float64) {
	if !c.Enabled {
		return 0, 0
	}
	v := feat.Band(c.ReactTo, "bass")
	return c.BrightnessBoost * v, c.ContrastBoost * v
}

func (s *Session) effectsAt(t float64, idx int, feat *audio.FrameFeatures) (frameFX, float64) {
	tl := &s.Cfg.Timeline
	minDim := math.Max(math.Min(float64(s.W), float64(s.H)), 64)
	var fx frameFX
	{
		a, b, k := config.BlendEv(tl.BassZoom, t, s.Duration(), s, "BassZoom")
		z := lerp(zoomOf(a, feat), zoomOf(b, feat), k)
		// with depth, part of it is the camera moving in (Prepare puts it back flat without one)
		d := lerp(zoomDepth(a), zoomDepth(b), k)
		fx.zoom, fx.zoomDolly = 1+(z-1)*(1-d), (z-1)*d
	}
	{
		a, b, k := config.BlendEv(tl.Shake, t, s.Duration(), s, "Shake")
		ax, ay, az := shakeOf(a, t, feat, minDim)
		bx, by, bz := shakeOf(b, t, feat, minDim)
		fx.shakeX, fx.shakeY = lerp(ax, bx, k), lerp(ay, by, k)
		fx.zoom *= lerp(az, bz, k)
	}
	{
		a, b, k := config.BlendEv(tl.Ripple, t, s.Duration(), s, "Ripple")
		aa, af, at := rippleOf(a, idx, feat)
		ba, bf, bt := rippleOf(b, idx, feat)
		fx.rippleAmp, fx.rippleFreq, fx.rTim = lerp(aa, ba, k), lerp(af, bf, k), lerp(at, bt, k)
		if !a.Enabled {
			fx.rippleFreq, fx.rTim = bf, bt
		} else if !b.Enabled {
			fx.rippleFreq, fx.rTim = af, at
		}
	}
	{
		a, b, k := config.BlendEv(tl.Chromatic, t, s.Duration(), s, "Chromatic")
		as, am := chromaOf(a, feat)
		bs, bm := chromaOf(b, feat)
		fx.chromaShift = lerp(as, bs, k)
		fx.chromaMode = bm
		if bm == 0 || (k < 0.5 && am != 0) {
			fx.chromaMode = am
		}
	}
	glitchAmt := 0.0
	{
		a, b, k := config.BlendEv(tl.Glitch, t, s.Duration(), s, "Glitch")
		c := a
		if k >= 0.5 {
			c = b
		}
		fx.glitch = glitchOn(c, feat)
		glitchAmt = c.Intensity
	}
	{
		a, b, k := config.BlendEv(tl.ColorFlash, t, s.Duration(), s, "ColorFlash")
		ab, ac := flashOf(a, feat)
		bb, bc := flashOf(b, feat)
		fx.bright, fx.contrast = lerp(ab, bb, k), lerp(ac, bc, k)
	}
	{
		a, b, k := config.BlendEv(tl.Parallax, t, s.Duration(), s, "Parallax")
		pa, pb := parallaxOf(a, t, feat), parallaxOf(b, t, feat)
		fx.par = effects.Parallax{SX: lerp(pa.SX, pb.SX, k), SY: lerp(pa.SY, pb.SY, k), Dolly: lerp(pa.Dolly, pb.Dolly, k),
			Focus: lerp(pa.Focus, pb.Focus, k)}
		switch {
		case !a.Enabled && !b.Enabled:
			fx.par.Focus = 0 // only the zoom moves in: what is far stays
		case !a.Enabled:
			fx.par.Focus = pb.Focus
		case !b.Enabled:
			fx.par.Focus = pa.Focus
		}
	}
	{
		a, b, k := config.BlendEv(tl.DoF, t, s.Duration(), s, "DoF")
		da, db := dofOf(a, feat), dofOf(b, feat)
		fx.dof = effects.DoF{From: lerp(da.From, db.From, k), To: lerp(da.To, db.To, k), Soft: lerp(da.Soft, db.Soft, k),
			Blur: lerp(da.Blur, db.Blur, k)}
		if !a.Enabled {
			fx.dof.From, fx.dof.To, fx.dof.Soft = db.From, db.To, db.Soft
		} else if !b.Enabled {
			fx.dof.From, fx.dof.To, fx.dof.Soft = da.From, da.To, da.Soft
		}
	}
	// only on a depth range (the segment in front during a fade)
	rangeOf := func(a, b *config.DepthRange, k float64) [4]float64 {
		r := a
		if k >= 0.5 {
			r = b
		}
		if !r.DepthOnly {
			return [4]float64{}
		}
		from, to, soft := r.Range()
		return [4]float64{from, to, soft, 1}
	}
	{
		a, b, k := config.BlendEv(tl.ColorFlash, t, s.Duration(), s, "ColorFlash")
		fx.rFlash = rangeOf(&a.DepthRange, &b.DepthRange, k)
	}
	{
		a, b, k := config.BlendEv(tl.Chromatic, t, s.Duration(), s, "Chromatic")
		fx.rChroma = rangeOf(&a.DepthRange, &b.DepthRange, k)
	}
	{
		a, b, k := config.BlendEv(tl.Ripple, t, s.Duration(), s, "Ripple")
		fx.rRipple = rangeOf(&a.DepthRange, &b.DepthRange, k)
	}
	{
		a, b, k := config.BlendEv(tl.Glitch, t, s.Duration(), s, "Glitch")
		fx.rGlitch = rangeOf(&a.DepthRange, &b.DepthRange, k)
	}
	return fx, glitchAmt
}

// ---------------------------------------------------------------- particles

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
		// Time jump: re-simulate from a few seconds earlier (particles live < 5 s)
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
	if sg.Transition.Animated() && t < sg.At+sg.Transition.Duration {
		k = math.Max(0, (t-sg.At)/sg.Transition.Duration)
	}
	alive := s.sim.entries[:0]
	for _, e := range s.sim.entries {
		if e.seg == cur {
			e.ps.Draining = false
			e.ps.Alpha = k
		} else {
			e.ps.Alpha = 1 - k
			if !sg.Transition.Animated() {
				e.ps.Alpha = 1
			}
		}
		if len(segs[e.seg].Keys) > 0 || len(segs[e.seg].Exprs) > 0 {
			e.ps.SetConfig(segs[e.seg].CfgAtEv(t, config.SegDur(segs, e.seg, s.Duration()), s, "particles"+strconv.Itoa(e.seg))) // animated properties
		}
		e.ps.Update(feat, 1/float64(s.FPS))
		if e.seg == cur || (e.ps.Alive() && e.ps.Alpha > 0) {
			alive = append(alive, e)
		}
	}
	s.sim.entries = alive
}

// Fork creates an independent session with a copy of the current config, reusing the audio
// and already computed analyses (to export while editing continues)
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
		layerMedia: map[string]*layerMedia{}, masks: map[string]maskEntry{},
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

// Duration returns the timeline duration in seconds
func (s *Session) Duration() float64 { return float64(s.Frames) / float64(s.FPS) }

// fluidFor returns the simulation of the fluid transition into background segment bi, running
// it the first time (it depends on both images, the size, the duration and the options)
func (s *Session) fluidFor(bi int, tr config.Transition, seed uint32, a, b *image.RGBA) *effects.Fluid {
	// Simulated on the GPU unless rendering is forced to the CPU (AUDIOVIZ_FLUID_CPU=1: always CPU)
	o := effects.FluidOpts{Seed: seed, Liquid: tr.Mode == "liquid", GPUDevice: s.Cfg.Output.GPUDevice,
		GPU: !strings.EqualFold(s.Cfg.Output.Renderer, "cpu") && os.Getenv("AUDIOVIZ_FLUID_CPU") == ""}
	if tr.Fluid != nil {
		o.ColorWeight, o.Interaction, o.Stagger, o.Viscosity = tr.Fluid.ColorWeight, tr.Fluid.Interaction, tr.Fluid.Stagger, tr.Fluid.Viscosity
		if tr.Fluid.Seed > 0 {
			o.Seed = uint32(tr.Fluid.Seed)
		}
	}
	frames := max(2, int(tr.Duration*float64(s.FPS)))
	key := fmt.Sprintf("%d|%dx%d|%d|%+v|%p|%p", bi, s.W, s.H, frames, o, a, b)
	// s.mu is held (called from Prepare)
	if f := s.fluids[key]; f != nil {
		return f
	}
	t0 := time.Now()
	f := effects.NewFluid(a, b, s.W, s.H, frames, o)
	if s.Logf != nil {
		where := "CPU"
		if f.SimGPU {
			where = "GPU"
		} else if f.SimErr != nil {
			where = fmt.Sprintf("CPU, GPU not available: %v", f.SimErr)
		}
		s.Logf("🌊 Fluid transition simulated in %v (%s)", time.Since(t0).Round(time.Millisecond), where)
	}
	if s.fluids == nil || len(s.fluids) >= 4 {
		s.fluids = map[string]*effects.Fluid{}
	}
	s.fluids[key] = f
	return f
}
