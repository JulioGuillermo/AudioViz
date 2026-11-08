package depth

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"

	"audioviz/internal/video"
)

// A video's depth on every frame with Video Depth Anything, as its own inference does
// (video_depth.py): windows of 32 frames, each starting 22 frames after the last; a window's
// first 10 frames are the previous window's key frames (its frames 0, 12 and 24…31), so each
// window is fitted (scale and shift) to the one before on two of them, and the 8 frames both
// windows share are blended from one to the other. All of the video is then one depth scale,
// made 0…1 by the first window's range: no flicker from frame to frame.

const (
	vdaLen    = 32
	vdaOver   = 10
	vdaInterp = 8
)

var vdaKeys = [vdaOver]int{0, 12, 24, 25, 26, 27, 28, 29, 30, 31}

// vdaSeq fills s (the sequence's .part folder) with every frame's map, then makes it final
// (meta.json, renamed to final); returns the first frame's map
func vdaSeq(ctx context.Context, cacheDir string, r Request, sp Spec, paths []string, s *Seq, final string,
	stage func(string, float64)) (*Map, error) {
	rd, err := video.NewLayerReader(r.Image, seqMaxSide, seqMaxSide, false)
	if err != nil {
		return nil, err
	}
	defer rd.Close()
	n, fps := s.N, s.FPS
	decode := func(i int) *image.RGBA {
		fr, _ := rd.FrameAt(float64(min(i, n-1)) / fps)
		img := image.NewRGBA(fr.Bounds())
		copy(img.Pix, fr.Pix)
		return img
	}
	type pend struct {
		i   int
		d   []float32
		img *image.RGBA
	}
	var (
		pending    []pend
		prevIn     []*image.RGBA
		ref0, ref1 []float32
		top        float32
		lo, hi     float32
		first      *Map
		mw, mh     int
	)
	// a frame done: its nearness (on the first window's scale) fitted to its picture's edges
	emit := func(p pend) error {
		if p.i >= n {
			return nil
		}
		raw := toRaw(sp, p.d, mw, mh, top)
		m := raw.nearnessIn(lo, hi)
		if m.W != p.img.Bounds().Dx() || m.H != p.img.Bounds().Dy() {
			m = Guided(m, p.img)
		}
		if p.i == 0 {
			first = m
		}
		return m.Save(s.framePath(p.i))
	}
	clamp0 := func(d []float32, scale, shift float32) []float32 {
		out := make([]float32, len(d))
		for i, v := range d {
			out[i] = max(0, v*scale+shift)
		}
		return out
	}
	stage("count", float64(n))
	step := vdaLen - vdaOver
	for s0 := 0; s0 < n; s0 += step {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		in := make([]*image.RGBA, vdaLen)
		for i := range in {
			if prevIn != nil && i < vdaOver {
				in[i] = prevIn[vdaKeys[i]]
			} else {
				in[i] = decode(s0 + i)
			}
		}
		t0 := time.Now()
		w, h, d, err := InferVideo(sp, paths, in)
		if err != nil {
			return nil, err
		}
		debugf("vda window at %d: %v", s0, time.Since(t0))
		if dir := os.Getenv("AUDIOVIZ_VDA_DUMP"); dir != "" && s0 == 0 { // (parity checks)
			vdaDump(dir, in, d, w, h)
		}
		mw, mh = w, h
		if prevIn == nil {
			// the first window as it is; its range sets the scale of all the video
			for _, f := range d {
				for _, v := range f {
					top = max(top, v)
				}
			}
			var sample []float32
			var skyS []float32
			for k := 0; k < vdaLen; k += 4 {
				rw := toRaw(sp, d[k], w, h, top)
				sample = append(sample, rw.V...)
				skyS = append(skyS, rw.Sky...)
			}
			lo, hi = percentiles(sample, skyS, 0.002, 0.998)
			for i := 0; i < vdaLen; i++ {
				pending = append(pending, pend{i, d[i], in[i]})
			}
			ref0, ref1 = d[0], d[12]
		} else {
			// fitted to the previous windows on their two shared key frames
			scale, shift := fitScaleShift(append(append([]float32(nil), d[0]...), d[1]...), append(append([]float32(nil), ref0...), ref1...))
			tail := pending[len(pending)-vdaInterp:]
			for k := range tail {
				post := clamp0(d[2+k], scale, shift)
				wk := float32(k) / float32(vdaInterp-1)
				for j := range tail[k].d {
					tail[k].d[j] = tail[k].d[j]*(1-wk) + post[j]*wk
				}
			}
			for i := vdaOver; i < vdaLen; i++ {
				pending = append(pending, pend{s0 + i, clamp0(d[i], scale, shift), in[i]})
			}
			ref1 = clamp0(d[12], scale, shift)
		}
		// all but the frames the next window still blends
		keep := vdaInterp
		if s0+step >= n {
			keep = 0
		}
		for _, p := range pending[:len(pending)-keep] {
			if err := emit(p); err != nil {
				return nil, err
			}
		}
		pending = slices.Clone(pending[len(pending)-keep:])
		prevIn = in
		stage("frames", min(1, float64(s0+step)/float64(n)))
	}
	if first == nil {
		return nil, os.ErrInvalid
	}
	if err := first.Save(CachePath(cacheDir, r)); err != nil {
		return nil, err
	}
	b, _ := json.Marshal(seqMeta{FPS: fps, N: n, Dur: s.Dur})
	if err := os.WriteFile(filepath.Join(s.Dir, "meta.json"), b, 0o644); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(final); err != nil {
		return nil, err
	}
	if err := os.Rename(s.Dir, final); err != nil {
		return nil, err
	}
	stage("done", 1)
	return first, nil
}

// fitScaleShift: the least-squares a, b with a·p + b ≈ q (1, 0 if it cannot be told)
func fitScaleShift(p, q []float32) (float32, float32) {
	var a00, a01, b0, b1 float64
	for i, x := range p {
		a00 += float64(x) * float64(x)
		a01 += float64(x)
		b0 += float64(x) * float64(q[i])
		b1 += float64(q[i])
	}
	a11 := float64(len(p))
	det := a00*a11 - a01*a01
	if det == 0 {
		return 1, 0
	}
	return float32((a11*b0 - a01*b1) / det), float32((-a01*b0 + a00*b1) / det)
}

// nearnessIn: as Nearness, stretched between lo and hi given (the same for all of a video)
func (r *Raw) nearnessIn(lo, hi float32) *Map {
	m := &Map{W: r.W, H: r.H, V: make([]float32, len(r.V))}
	span := max(hi-lo, 1e-9)
	for i, v := range r.V {
		m.V[i] = max(0, min(1, (v-lo)/span))
		if r.Sky != nil {
			m.V[i] *= 1 - smooth(0.3, 0.7, r.Sky[i])
		}
	}
	return m
}

// vdaDump writes a window's frames (RGB) and depth (float32) for a parity check
func vdaDump(dir string, in []*image.RGBA, d [][]float32, w, h int) {
	os.MkdirAll(dir, 0o755)
	fw, fh := in[0].Bounds().Dx(), in[0].Bounds().Dy()
	rgb := make([]byte, 0, len(in)*fw*fh*3)
	for _, f := range in {
		for y := 0; y < fh; y++ {
			for x := 0; x < fw; x++ {
				o := y*f.Stride + x*4
				rgb = append(rgb, f.Pix[o], f.Pix[o+1], f.Pix[o+2])
			}
		}
	}
	os.WriteFile(filepath.Join(dir, fmt.Sprintf("in_%dx%d.rgb", fw, fh)), rgb, 0o644)
	out := make([]byte, 0, len(d)*w*h*4)
	for _, f := range d {
		for _, v := range f {
			out = binary.LittleEndian.AppendUint32(out, math.Float32bits(v))
		}
	}
	os.WriteFile(filepath.Join(dir, fmt.Sprintf("out_%dx%d.f32", w, h)), out, 0o644)
}
