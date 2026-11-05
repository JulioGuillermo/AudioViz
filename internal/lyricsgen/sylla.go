package lyricsgen

/*
#include <stdlib.h>
#include "syl.h"
*/
import "C"

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/cmplx"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"audioviz/internal/progress"
)

// Sylla 600m (radiant-lyrics, GPL-3.0): a w2v-BERT 2.0 CTC model trained on sung lyrics. With
// the lyrics known it times every word straight from the vocals (CTC forced alignment), far
// closer than the recognition of a speech model. It runs on ggml (syl.cpp). The weights are
// downloaded as published (pinned revision), never redistributed with AudioViz.
const syllaRev = "0a228acf0c1351ad0e2734604ee921d497d861e8"

var (
	SyllaModel = Model{Name: "sylla", Label: "Lyrics alignment (Sylla 600m, GPL-3.0)", File: "sylla-600m-v1.safetensors",
		URL: "https://huggingface.co/radiant-lyrics/sylla-600m-v1/resolve/" + syllaRev + "/model.safetensors", Size: 2325850852}
	SyllaVocab = Model{Name: "sylla-vocab", Label: "Sylla vocabulary", File: "sylla-600m-v1.vocab.json",
		URL: "https://huggingface.co/radiant-lyrics/sylla-600m-v1/resolve/" + syllaRev + "/vocab.json", Size: 12488}
)

// Aligners: how the known lyrics get their timing
const (
	AlignerWhisper = "whisper"
	AlignerSylla   = "sylla"
)

const (
	sylVocab  = 921
	sylFeat   = 160
	sylWin    = 30.0 // seconds per window, plus sylCtx of context on each side (dropped)
	sylCtx    = 5.0
	sylBlank  = 0
	sylDelim  = 1
)

// Sylla is the loaded model
type Sylla struct {
	p      unsafe.Pointer
	Device string
}

// loadSyllaVocab: the model's characters (single letters, digits, apostrophe) by id
func loadSyllaVocab(path string) (map[rune]int, error) {
	vb, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var vm map[string]int
	if err := json.Unmarshal(vb, &vm); err != nil {
		return nil, fmt.Errorf("sylla vocabulary: %w", err)
	}
	voc := map[rune]int{}
	for k, id := range vm {
		if r := []rune(k); len(r) == 1 && id > sylDelim {
			voc[r[0]] = id
		}
	}
	return voc, nil
}

// OpenSylla loads the weights (model.safetensors, f32) to the device, one tensor at a time
func OpenSylla(modelPath string) (*Sylla, error) {
	s := &Sylla{}
	f, err := os.Open(modelPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var hl [8]byte
	if _, err := io.ReadFull(f, hl[:]); err != nil {
		return nil, err
	}
	hb := make([]byte, binary.LittleEndian.Uint64(hl[:]))
	if _, err := io.ReadFull(f, hb); err != nil {
		return nil, err
	}
	var hdr map[string]json.RawMessage
	if err := json.Unmarshal(hb, &hdr); err != nil {
		return nil, fmt.Errorf("sylla weights: %w", err)
	}
	base := int64(8 + len(hb))

	var cerr [256]C.char
	var dev [128]C.char
	s.p = C.syl_new(C.int(max(1, runtime.NumCPU()-1)), &dev[0], 128, &cerr[0], 256)
	if s.p == nil {
		return nil, fmt.Errorf("sylla: %s", C.GoString(&cerr[0]))
	}
	s.Device = C.GoString(&dev[0])
	var buf []float32
	for name, raw := range hdr {
		if name == "__metadata__" {
			continue
		}
		var ti struct {
			Dtype   string   `json:"dtype"`
			Offsets [2]int64 `json:"data_offsets"`
		}
		if err := json.Unmarshal(raw, &ti); err != nil || ti.Dtype != "F32" {
			s.Close()
			return nil, fmt.Errorf("sylla weights: %s is not f32", name)
		}
		n := (ti.Offsets[1] - ti.Offsets[0]) / 4
		if int64(cap(buf)) < n {
			buf = make([]float32, n)
		}
		buf = buf[:n]
		if _, err := f.ReadAt(unsafe.Slice((*byte)(unsafe.Pointer(&buf[0])), n*4), base+ti.Offsets[0]); err != nil {
			s.Close()
			return nil, err
		}
		cn := C.CString(name)
		r := C.syl_set(s.p, cn, (*C.float)(&buf[0]), C.long(n))
		C.free(unsafe.Pointer(cn))
		if r < 0 {
			s.Close()
			return nil, fmt.Errorf("sylla weights: %s has the wrong size", name)
		}
	}
	if C.syl_check(s.p, &cerr[0], 256) != 0 {
		s.Close()
		return nil, fmt.Errorf("sylla weights: %s", C.GoString(&cerr[0]))
	}
	return s, nil
}

func (s *Sylla) Close() {
	if s.p != nil {
		C.syl_free(s.p)
		s.p = nil
	}
}

// LogProbs runs the model over the whole song (mono 16 kHz) in windows with context: the CTC
// log-probabilities, sylVocab per frame (≈ 20 ms)
func (s *Sylla) LogProbs(ctx context.Context, mono16 []float32, progress func(float64)) ([]float32, int, error) {
	const sr = 16000
	dur := float64(len(mono16)) / sr
	var out []float32
	var cerr [256]C.char
	for t := 0.0; t < dur; t += sylWin {
		if ctx.Err() != nil {
			return nil, 0, ctx.Err()
		}
		a, b := math.Max(0, t-sylCtx), math.Min(dur, t+sylWin+sylCtx)
		feat, T := fbankFeatures(mono16[int(a*sr):min(len(mono16), int(b*sr))])
		if T == 0 {
			break
		}
		lg := make([]float32, T*sylVocab)
		if C.syl_logits(s.p, (*C.float)(&feat[0]), C.int(T), (*C.float)(&lg[0]), &cerr[0], 256) != 0 {
			return nil, 0, fmt.Errorf("sylla: %s", C.GoString(&cerr[0]))
		}
		fps := float64(T) / (b - a)
		i0 := int(math.Round((t - a) * fps))
		i1 := min(T, int(math.Round((math.Min(dur, t+sylWin)-a)*fps)))
		for i := i0; i < i1; i++ {
			out = append(out, logSoftmax(lg[i*sylVocab:(i+1)*sylVocab])...)
		}
		if progress != nil {
			progress(math.Min(1, (t+sylWin)/dur))
		}
	}
	return out, len(out) / sylVocab, nil
}

func logSoftmax(x []float32) []float32 {
	m := x[0]
	for _, v := range x {
		m = max(m, v)
	}
	var sum float64
	for _, v := range x {
		sum += math.Exp(float64(v - m))
	}
	ls := float32(math.Log(sum)) + m
	out := make([]float32, len(x))
	for i, v := range x {
		out[i] = v - ls
	}
	return out
}

// ---------------------------------------------------------------- features

// The SeamlessM4T feature extractor (Kaldi-style fbank): 80 log-mel bins, 25 ms windows every
// 10 ms (povey window, DC removal, pre-emphasis 0.97, 512-point FFT, power), each bin normalized
// over the clip, then pairs of frames stacked: [T][160]
func fbankFeatures(x []float32) ([]float32, int) {
	const flen, hop, nfft, nmel = 400, 160, 512, 80
	nf := 0
	if len(x) >= flen {
		nf = 1 + (len(x)-flen)/hop
	}
	T := nf / 2
	if T == 0 {
		return nil, 0
	}
	nf = T * 2
	win, mel := fbankTables()
	feats := make([]float64, nf*nmel)
	buf := make([]complex128, nfft)
	fr := make([]float64, flen)
	pw := make([]float64, nfft/2+1)
	for f := 0; f < nf; f++ {
		var mean float64
		for i := 0; i < flen; i++ {
			fr[i] = float64(x[f*hop+i]) * 32768
			mean += fr[i]
		}
		mean /= flen
		for i := range fr {
			fr[i] -= mean
		}
		for i := flen - 1; i > 0; i-- {
			fr[i] -= 0.97 * fr[i-1]
		}
		fr[0] *= 1 - 0.97
		for i := range buf {
			buf[i] = 0
		}
		for i := 0; i < flen; i++ {
			buf[i] = complex(fr[i]*win[i], 0)
		}
		fft(buf)
		for k := range pw {
			a := cmplx.Abs(buf[k])
			pw[k] = a * a
		}
		for m := 0; m < nmel; m++ {
			var e float64
			for k, w := range mel[m] {
				e += w * pw[k]
			}
			feats[f*nmel+m] = math.Log(math.Max(e, 1.192092955078125e-07))
		}
	}
	out := make([]float32, nf*nmel)
	for m := 0; m < nmel; m++ {
		var mean, v float64
		for f := 0; f < nf; f++ {
			mean += feats[f*nmel+m]
		}
		mean /= float64(nf)
		for f := 0; f < nf; f++ {
			d := feats[f*nmel+m] - mean
			v += d * d
		}
		if nf > 1 {
			v /= float64(nf - 1)
		}
		sd := math.Sqrt(v + 1e-7)
		for f := 0; f < nf; f++ {
			out[f*nmel+m] = float32((feats[f*nmel+m] - mean) / sd)
		}
	}
	// (frames 2t and 2t+1 side by side are already row t of [T][160])
	return out, T
}

var fbWin []float64
var fbMel [][]float64

func fbankTables() ([]float64, [][]float64) {
	if fbWin != nil {
		return fbWin, fbMel
	}
	const flen, nbins, nmel, sr = 400, 257, 80, 16000
	win := make([]float64, flen)
	for i := range win {
		win[i] = math.Pow(0.5-0.5*math.Cos(2*math.Pi*float64(i)/float64(flen-1)), 0.85)
	}
	toMel := func(f float64) float64 { return 1127 * math.Log(1+f/700) }
	lo, hi := toMel(20), toMel(sr/2)
	centers := make([]float64, nmel+2)
	for i := range centers {
		centers[i] = lo + (hi-lo)*float64(i)/float64(nmel+1)
	}
	mel := make([][]float64, nmel)
	for m := 0; m < nmel; m++ {
		mel[m] = make([]float64, nbins)
		for k := 0; k < nbins; k++ {
			f := toMel(float64(sr) / 2 / float64(nbins-1) * float64(k))
			down := (f - centers[m]) / (centers[m+1] - centers[m])
			up := (centers[m+2] - f) / (centers[m+2] - centers[m+1])
			mel[m][k] = math.Max(0, math.Min(down, up))
		}
	}
	fbWin, fbMel = win, mel
	return win, mel
}

// fft: in-place radix-2 (len a power of two)
func fft(a []complex128) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for l := 2; l <= n; l <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(l)))
		for i := 0; i < n; i += l {
			wk := complex(1, 0)
			for k := 0; k < l/2; k++ {
				u, v := a[i+k], a[i+k+l/2]*wk
				a[i+k], a[i+k+l/2] = u+v, u-v
				wk *= w
			}
		}
	}
}

// ---------------------------------------------------------------- alignment

// ctcAlign times the lyrics words lw (Start/End, all anchored) by Viterbi over the CTC
// log-probabilities (T frames of frame seconds): the characters of each word, words separated
// by the delimiter
func ctcAlign(vocab map[rune]int, lw []LWord, lp []float32, T int, frame float64) error {
	var toks []int
	span := make([][2]int, len(lw))
	for wi, w := range lw {
		if wi > 0 && len(toks) > 0 {
			toks = append(toks, sylDelim)
		}
		a := len(toks)
		for _, r := range strings.ToLower(strings.NewReplacer("’", "'", "‘", "'").Replace(w.Text)) {
			if id, ok := vocab[r]; ok {
				toks = append(toks, id)
			}
		}
		span[wi] = [2]int{a, len(toks)}
	}
	L := len(toks)
	if L == 0 {
		return fmt.Errorf("the lyrics have no letters the model knows")
	}
	S := 2*L + 1
	if T < L {
		return fmt.Errorf("the lyrics are too long for the audio")
	}
	ext := make([]int, S) // blank, t1, blank, t2, …, blank
	for k, id := range toks {
		ext[2*k+1] = id
	}
	skip := make([]bool, S)
	for i := 2; i < S; i++ {
		skip[i] = ext[i] != sylBlank && ext[i] != ext[i-2]
	}
	const neg = -1e30
	dp, nd := make([]float64, S), make([]float64, S)
	for i := range dp {
		dp[i] = neg
	}
	dp[0] = float64(lp[sylBlank])
	dp[1] = float64(lp[ext[1]])
	bp := make([]int8, T*S)
	for t := 1; t < T; t++ {
		row := lp[t*sylVocab : (t+1)*sylVocab]
		for i := 0; i < S; i++ {
			best, arg := dp[i], int8(0)
			if i > 0 && dp[i-1] > best {
				best, arg = dp[i-1], 1
			}
			if skip[i] && dp[i-2] > best {
				best, arg = dp[i-2], 2
			}
			nd[i] = best + float64(row[ext[i]])
			bp[t*S+i] = arg
		}
		dp, nd = nd, dp
	}
	st := S - 1
	if dp[S-2] > dp[S-1] {
		st = S - 2
	}
	first, last := make([]int, L), make([]int, L)
	for k := range first {
		first[k], last[k] = -1, -1
	}
	for t := T - 1; t >= 0; t-- {
		if st%2 == 1 {
			k := st / 2
			first[k] = t
			if last[k] < 0 {
				last[k] = t
			}
		}
		st -= int(bp[t*S+st])
	}
	prevEnd := 0.0
	for wi := range lw {
		a, b := span[wi][0], span[wi][1]
		if a == b || first[a] < 0 { // (no letters: right after the previous word)
			lw[wi].Start, lw[wi].End = prevEnd, prevEnd
		} else {
			lw[wi].Start = float64(first[a]) * frame
			lw[wi].End = float64(last[b-1]+1) * frame
		}
		lw[wi].anchored = true
		prevEnd = lw[wi].End
	}
	return nil
}

// syllaAlign: steps 4 and 5 of Generate with Sylla. The log-probabilities are cached next to
// the separated vocals (aligning a corrected text again then takes a second).
func syllaAlign(ctx context.Context, t *progress.Tracker, lines []string, vocals16 []float32, v *voice, total float64,
	cachePath string, separate bool) (*Result, error) {
	vocab, err := loadSyllaVocab(SyllaVocab.Path())
	if err != nil {
		return nil, err
	}
	lpPath := ""
	if cachePath != "" {
		mode := "mix"
		if separate {
			mode = "vocals"
		}
		lpPath = strings.TrimSuffix(cachePath, ".f32") + ".sylla." + mode + ".f32"
	}
	device := "cache"
	var lp []float32
	if lpPath != "" {
		if lp = loadF32(lpPath); len(lp)%sylVocab != 0 {
			lp = nil
		}
	}
	if lp != nil {
		t.End("recognize", "skipped", "Reused from the cache")
	} else {
		t.Start("recognize", "Loading the model…")
		s, err := OpenSylla(SyllaModel.Path())
		if err != nil {
			t.End("recognize", "error", err.Error())
			return nil, err
		}
		defer s.Close()
		device = s.Device
		t.SetDevice(device)
		t.Progress("recognize", 0, "Sylla 600m · "+device)
		if lp, _, err = s.LogProbs(ctx, vocals16, func(p float64) { t.Progress("recognize", p, "") }); err != nil {
			t.End("recognize", "error", err.Error())
			return nil, err
		}
		if lpPath != "" && len(lp) > 0 {
			saveF32(lpPath, lp)
		}
		t.End("recognize", "done", "Sylla 600m · "+device)
	}
	t.Start("align", "Aligning…")
	T := len(lp) / sylVocab
	lw := lyricWords(lines)
	if err := ctcAlign(vocab, lw, lp, T, total/float64(max(1, T))); err != nil {
		t.End("align", "error", err.Error())
		return nil, err
	}
	out := finish(lw, v, total)
	t.End("align", "done", fmt.Sprintf("%d lines · %d words timed by Sylla", len(out), len(lw)))
	return &Result{Lines: out, LRC: LRC(out), Device: device}, nil
}
