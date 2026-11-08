package audio

import (
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"

	"audioviz/internal/fileid"
)

// The song separated into its instruments (Demucs: drums, bass, other, vocals), kept as mono WAV
// files in the project's cache: each one's energy over time, and the hits of the drums (kick,
// snare, hi-hats) found in its low, middle and high parts. Effects can react to any of them.

// StemNames: the stems, in Demucs' order
var StemNames = [4]string{"drums", "bass", "other", "vocals"}

// StemRate: the stems' sample rate in the cache (enough for their energies and hits)
const StemRate = 22050

// StemFrame is the separated song at a frame, 0..1 (to its loud moments): the energy of each
// instrument, and a pulse at each hit of the drums (1 at the hit, fading)
type StemFrame struct {
	Drums, Bass, Other, Vocals float64
	Kick, Snare, Hats          float64
}

// StemsDir: the folder of a song's stems in a cache folder (by its content: a project copied,
// moved or imported keeps them)
func StemsDir(cacheDir, audioPath string) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s|stems2", fileid.Fingerprint(audioPath))
	dir := filepath.Join(cacheDir, "stems", hex.EncodeToString(h.Sum(nil))[:20])
	if fileid.Once(dir) { // (kept by the song's path before)
		o := sha1.New()
		fmt.Fprintf(o, "%s|", audioPath)
		if st, err := os.Stat(audioPath); err == nil {
			fmt.Fprintf(o, "%d|%d|", st.Size(), st.ModTime().UnixNano())
		}
		fileid.Move(filepath.Join(cacheDir, "stems", hex.EncodeToString(o.Sum(nil))[:20]), dir)
	}
	return dir
}

// StemsReady: whether a song's stems are in the cache
func StemsReady(dir string) bool {
	for _, n := range StemNames {
		if _, err := os.Stat(filepath.Join(dir, n+".wav")); err != nil {
			return false
		}
	}
	return true
}

// SaveStem writes a stem (channels interleaved, at rate Hz) as a 16-bit WAV: as it is when full,
// else as a mono mix at StemRate (enough for its energies and hits, a quarter of the size)
func SaveStem(path string, x []float32, channels, rate int, full bool) error {
	if !full {
		x = toMono(x, channels)
		if rate != StemRate {
			x = resampleLinear(x, rate, StemRate)
		}
		channels, rate = 1, StemRate
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path + ".tmp")
	if err != nil {
		return err
	}
	n := len(x)
	hdr := []any{[4]byte{'R', 'I', 'F', 'F'}, uint32(36 + 2*n), [4]byte{'W', 'A', 'V', 'E'},
		[4]byte{'f', 'm', 't', ' '}, uint32(16), uint16(1), uint16(channels), uint32(rate), uint32(2 * channels * rate),
		uint16(2 * channels), uint16(16), [4]byte{'d', 'a', 't', 'a'}, uint32(2 * n)}
	for _, v := range hdr {
		if err := binary.Write(f, binary.LittleEndian, v); err != nil {
			f.Close()
			return err
		}
	}
	buf := make([]int16, n)
	for i, v := range x {
		buf[i] = int16(math.Max(-1, math.Min(1, float64(v))) * 32767)
	}
	if err := binary.Write(f, binary.LittleEndian, buf); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// stemHeader: a stem file's channels and rate
func stemHeader(f io.Reader) (channels, rate, n int, err error) {
	var hdr [44]byte
	if _, err = io.ReadFull(f, hdr[:]); err != nil {
		return
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[36:40]) != "data" || binary.LittleEndian.Uint16(hdr[20:]) != 1 ||
		binary.LittleEndian.Uint16(hdr[34:]) != 16 {
		return 0, 0, 0, errors.New("not a stem file")
	}
	channels = int(binary.LittleEndian.Uint16(hdr[22:]))
	rate = int(binary.LittleEndian.Uint32(hdr[24:]))
	n = int(binary.LittleEndian.Uint32(hdr[40:])) / 2
	return
}

// LoadStem reads a stem written by SaveStem as a mono mix at StemRate (for its analysis)
func LoadStem(path string) ([]float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ch, rate, n, err := stemHeader(f)
	if err != nil {
		return nil, err
	}
	buf := make([]int16, n)
	if err := binary.Read(f, binary.LittleEndian, buf); err != nil {
		return nil, err
	}
	x := make([]float32, n)
	for i, v := range buf {
		x[i] = float32(v) / 32767
	}
	x = toMono(x, ch)
	if rate != StemRate {
		x = resampleLinear(x, rate, StemRate)
	}
	return x, nil
}

// StemsFull: whether a song's stems are kept in full quality (stereo, as separated)
func StemsFull(dir string) bool {
	f, err := os.Open(filepath.Join(dir, StemNames[0]+".wav"))
	if err != nil {
		return false
	}
	defer f.Close()
	ch, _, _, err := stemHeader(f)
	return err == nil && ch == 2
}

// toMono: interleaved channels mixed down
func toMono(x []float32, ch int) []float32 {
	if ch <= 1 {
		return x
	}
	out := make([]float32, len(x)/ch)
	for i := range out {
		var s float32
		for c := 0; c < ch; c++ {
			s += x[i*ch+c]
		}
		out[i] = s / float32(ch)
	}
	return out
}

// resampleLinear: x at rate a, at rate b
func resampleLinear(x []float32, a, b int) []float32 {
	n := int(int64(len(x)) * int64(b) / int64(a))
	out := make([]float32, n)
	k := float64(a) / float64(b)
	for i := range out {
		p := float64(i) * k
		j := int(p)
		if j+1 >= len(x) {
			out[i] = x[len(x)-1]
			continue
		}
		f := float32(p - float64(j))
		out[i] = x[j] + (x[j+1]-x[j])*f
	}
	return out
}

// AnalyzeStems: the separated song at each of frames video frames (fps): the stems (mono at
// StemRate, StemNames' order) to their loud moments within ±window s, and the drums' hits
func AnalyzeStems(stems [4][]float32, fps, frames int, window float64) []StemFrame {
	out := make([]StemFrame, frames)
	if window <= 0 {
		window = 6
	}
	en := func(x []float32) []float64 { return normalize(frameRMS(x, fps, frames), int(window*float64(fps))) }
	set := func(v []float64, put func(*StemFrame, float64)) {
		for i := range out {
			put(&out[i], v[i])
		}
	}
	set(en(stems[0]), func(f *StemFrame, v float64) { f.Drums = v })
	set(en(stems[1]), func(f *StemFrame, v float64) { f.Bass = v })
	set(en(stems[2]), func(f *StemFrame, v float64) { f.Other = v })
	set(en(stems[3]), func(f *StemFrame, v float64) { f.Vocals = v })
	// the drums' hits: in its low part (kick), its middle (snare) and its highs (hi-hats)
	d := stems[0]
	low := lowPass(lowPass(d, 120), 120)
	mid := lowPass(highPass(d, 200), 2500)
	high := highPass(highPass(d, 6000), 6000)
	win := int(window * float64(fps))
	set(hits(normalize(frameRMS(low, fps, frames), win), fps, 0.15), func(f *StemFrame, v float64) { f.Kick = v })
	set(hits(normalize(frameRMS(mid, fps, frames), win), fps, 0.12), func(f *StemFrame, v float64) { f.Snare = v })
	set(hits(normalize(frameRMS(high, fps, frames), win), fps, 0.06), func(f *StemFrame, v float64) { f.Hats = v })
	return out
}

// frameRMS: the loudness of x (at StemRate) in each video frame
func frameRMS(x []float32, fps, frames int) []float64 {
	out := make([]float64, frames)
	hop := float64(StemRate) / float64(fps)
	for f := range out {
		a, b := int(float64(f)*hop), int(float64(f+1)*hop)
		a, b = min(a, len(x)), min(b, len(x))
		var s float64
		for _, v := range x[a:b] {
			s += float64(v) * float64(v)
		}
		if b > a {
			out[f] = math.Sqrt(s / float64(b-a))
		}
	}
	return out
}

// normalize: each value against the loudest around it (±w frames, a floor from the whole song so
// silence stays silent), then held: it rises at once and falls smoothly
func normalize(e []float64, w int) []float64 {
	n := len(e)
	out := make([]float64, n)
	if n == 0 {
		return out
	}
	s := append([]float64{}, e...)
	sort.Float64s(s)
	floor := s[int(0.95*float64(n-1))]*0.25 + 1e-6
	// the loudest within ±w: a sliding maximum
	mx := make([]float64, n)
	var dq []int
	for i := 0; i < n+w; i++ {
		if i < n {
			for len(dq) > 0 && e[dq[len(dq)-1]] <= e[i] {
				dq = dq[:len(dq)-1]
			}
			dq = append(dq, i)
		}
		if c := i - w; c >= 0 {
			for len(dq) > 0 && dq[0] < c-w {
				dq = dq[1:]
			}
			mx[c] = e[dq[0]]
		}
	}
	prev := 0.0
	for i, v := range e {
		x := math.Min(v/math.Max(mx[i]*0.8, floor), 1.2)
		prev = math.Max(x, prev*0.85)
		out[i] = prev
	}
	return out
}

// hits: a pulse at each sudden rise of e (a hit), fading in tau seconds
func hits(e []float64, fps int, tau float64) []float64 {
	n := len(e)
	flux := make([]float64, n)
	for i := 1; i < n; i++ {
		flux[i] = math.Max(0, e[i]-e[i-1])
	}
	out := make([]float64, n)
	w := max(1, fps/2)
	gap := max(1, int(0.07*float64(fps)))
	// the mean rise around each frame: a sliding sum
	var sum float64
	for i := 0; i < min(n, w); i++ {
		sum += flux[i]
	}
	last := -gap
	pulse, decay := 0.0, math.Exp(-1/(tau*float64(fps)))
	for i := 0; i < n; i++ {
		if i+w < n {
			sum += flux[i+w]
		}
		if i-w-1 >= 0 {
			sum -= flux[i-w-1]
		}
		cnt := float64(min(n-1, i+w) - max(0, i-w) + 1)
		thr := sum/cnt*1.8 + 0.04
		pulse *= decay
		peak := flux[i] > thr && (i == 0 || flux[i] >= flux[i-1]) && (i == n-1 || flux[i] > flux[i+1])
		if peak && i-last >= gap {
			pulse = math.Max(pulse, math.Min(1, 0.5+e[i]*0.5))
			last = i
		}
		out[i] = pulse
	}
	return out
}

// lowPass / highPass: one-pole filters at f Hz (StemRate)
func lowPass(x []float32, f float64) []float32 {
	a := float32(1 - math.Exp(-2*math.Pi*f/StemRate))
	out := make([]float32, len(x))
	var y float32
	for i, v := range x {
		y += a * (v - y)
		out[i] = y
	}
	return out
}

func highPass(x []float32, f float64) []float32 {
	lp := lowPass(x, f)
	for i := range lp {
		lp[i] = x[i] - lp[i]
	}
	return lp
}
