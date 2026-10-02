package audio

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/go-mp3"
	"github.com/jfreymuth/oggvorbis"
	"github.com/mewkiz/flac"
)

// DecodedAudio holds the decoded stereo PCM audio samples.
// Everything is decoded in pure Go (no ffmpeg): FLAC, WAV, MP3 and OGG Vorbis.
type DecodedAudio struct {
	SampleRate int
	Channels   int
	// Samples holds the interleaved normalized samples [-1.0, 1.0]: [L0, R0, L1, R1, ...]
	// (with the analysis volume applied)
	Samples     []float32
	DurationSec float64

	// Original interleaved stereo integer PCM (no volume), used to losslessly encode
	// the audio track of the final video.
	PCM           []int32
	BitsPerSample int
}

// LeftSample returns the left-channel sample for audio frame index i
func (d *DecodedAudio) LeftSample(i int) float32 {
	idx := i * 2
	if idx < 0 || idx >= len(d.Samples) {
		return 0
	}
	return d.Samples[idx]
}

// RightSample returns the right-channel sample for audio frame index i
func (d *DecodedAudio) RightSample(i int) float32 {
	idx := i*2 + 1
	if idx < 0 || idx >= len(d.Samples) {
		return 0
	}
	return d.Samples[idx]
}

// TotalFrames returns the number of audio frames (stereo pairs)
func (d *DecodedAudio) TotalFrames() int {
	return len(d.Samples) / 2
}

// DecodeAudio decodes the file to stereo PCM at its native sample rate.
// The volume only affects the analysis samples, not the final video's audio.
func DecodeAudio(filePath string, volume float64) (*DecodedAudio, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open audio '%s': %w", filePath, err)
	}
	defer f.Close()

	var magic [12]byte
	n, _ := io.ReadFull(f, magic[:])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	var d *DecodedAudio
	ext := strings.ToLower(filepath.Ext(filePath))
	switch {
	case n >= 4 && string(magic[:4]) == "fLaC":
		d, err = decodeFLAC(f)
	case n >= 12 && string(magic[:4]) == "RIFF" && string(magic[8:12]) == "WAVE":
		d, err = decodeWAV(f)
	case n >= 4 && string(magic[:4]) == "OggS":
		d, err = decodeOGG(f)
	case ext == ".mp3" || (n >= 3 && string(magic[:3]) == "ID3") || (n >= 2 && magic[0] == 0xFF && magic[1]&0xE0 == 0xE0):
		d, err = decodeMP3(f)
	default:
		return nil, fmt.Errorf("unsupported audio format '%s' (supported: FLAC, WAV, MP3, OGG Vorbis)", filePath)
	}
	if err != nil {
		return nil, fmt.Errorf("error decoding '%s': %w", filePath, err)
	}

	stereoFrames := len(d.PCM) / 2
	if stereoFrames == 0 {
		return nil, fmt.Errorf("the decoded audio file is empty")
	}

	scale := float32(1.0 / float64(int64(1)<<(d.BitsPerSample-1)))
	vol := float32(volume)
	d.Samples = make([]float32, len(d.PCM))
	for i, s := range d.PCM {
		v := float32(s) * scale * vol
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		d.Samples[i] = v
	}
	d.Channels = 2
	d.DurationSec = float64(stereoFrames) / float64(d.SampleRate)
	return d, nil
}

// toStereo converts planar per-channel samples to interleaved stereo (downmixes if >2 channels).
func toStereo(dst []int32, frame [][]int32) []int32 {
	nch := len(frame)
	n := len(frame[0])
	for i := 0; i < n; i++ {
		switch nch {
		case 1:
			dst = append(dst, frame[0][i], frame[0][i])
		case 2:
			dst = append(dst, frame[0][i], frame[1][i])
		default:
			// Simple downmix: even channels to L, odd to R
			var l, r int64
			var nl, nr int64
			for c := 0; c < nch; c++ {
				if c%2 == 0 {
					l += int64(frame[c][i])
					nl++
				} else {
					r += int64(frame[c][i])
					nr++
				}
			}
			dst = append(dst, int32(l/nl), int32(r/nr))
		}
	}
	return dst
}

func decodeFLAC(r io.Reader) (*DecodedAudio, error) {
	stream, err := flac.New(bufio.NewReaderSize(r, 1<<20))
	if err != nil {
		return nil, err
	}
	defer stream.Close()

	info := stream.Info
	bps := int(info.BitsPerSample)
	pcm := make([]int32, 0, int(info.NSamples)*2)
	planes := make([][]int32, info.NChannels)
	for {
		frame, err := stream.ParseNext()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		for c := range planes {
			planes[c] = frame.Subframes[c].Samples
		}
		pcm = toStereo(pcm, planes)
	}
	pcm, bps = normalizeDepth(pcm, bps)
	return &DecodedAudio{SampleRate: int(info.SampleRate), PCM: pcm, BitsPerSample: bps}, nil
}

// normalizeDepth brings the bit depth to 16 or 24 bits (those supported by the output FLAC encoder).
func normalizeDepth(pcm []int32, bps int) ([]int32, int) {
	switch {
	case bps == 16 || bps == 24:
		return pcm, bps
	case bps < 16:
		sh := uint(16 - bps)
		for i := range pcm {
			pcm[i] <<= sh
		}
		return pcm, 16
	case bps < 24:
		sh := uint(24 - bps)
		for i := range pcm {
			pcm[i] <<= sh
		}
		return pcm, 24
	default:
		sh := uint(bps - 24)
		for i := range pcm {
			pcm[i] >>= sh
		}
		return pcm, 24
	}
}

func decodeMP3(r io.Reader) (*DecodedAudio, error) {
	dec, err := mp3.NewDecoder(bufio.NewReaderSize(r, 1<<20))
	if err != nil {
		return nil, err
	}
	// go-mp3 always outputs 16-bit interleaved stereo little-endian
	raw, err := io.ReadAll(dec)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	pcm := make([]int32, len(raw)/2)
	for i := range pcm {
		pcm[i] = int32(int16(uint16(raw[2*i]) | uint16(raw[2*i+1])<<8))
	}
	if len(pcm)%2 == 1 {
		pcm = pcm[:len(pcm)-1]
	}
	return &DecodedAudio{SampleRate: dec.SampleRate(), PCM: pcm, BitsPerSample: 16}, nil
}

func decodeOGG(r io.Reader) (*DecodedAudio, error) {
	data, format, err := oggvorbis.ReadAll(bufio.NewReaderSize(r, 1<<20))
	if err != nil {
		return nil, err
	}
	nch := format.Channels
	n := len(data) / nch
	planes := make([][]int32, nch)
	for c := range planes {
		planes[c] = make([]int32, n)
		for i := 0; i < n; i++ {
			planes[c][i] = floatToInt(data[i*nch+c], 16)
		}
	}
	return &DecodedAudio{SampleRate: format.SampleRate, PCM: toStereo(make([]int32, 0, n*2), planes), BitsPerSample: 16}, nil
}

func floatToInt(v float32, bps int) int32 {
	max := float64(int64(1)<<(bps-1)) - 1
	x := math.Round(float64(v) * max)
	if x > max {
		x = max
	} else if x < -max-1 {
		x = -max - 1
	}
	return int32(x)
}

func decodeWAV(r io.Reader) (*DecodedAudio, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	var hdr [12]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil {
		return nil, err
	}

	var (
		format, nch, bps uint16
		rate             uint32
		haveFmt          bool
	)
	le16 := func(b []byte) uint16 { return uint16(b[0]) | uint16(b[1])<<8 }
	le32 := func(b []byte) uint32 { return uint32(le16(b)) | uint32(le16(b[2:]))<<16 }

	for {
		var ch [8]byte
		if _, err := io.ReadFull(br, ch[:]); err != nil {
			return nil, fmt.Errorf("WAV without 'data' chunk")
		}
		id := string(ch[:4])
		size := le32(ch[4:])
		switch id {
		case "fmt ":
			b := make([]byte, size+size%2)
			if _, err := io.ReadFull(br, b); err != nil {
				return nil, err
			}
			format, nch, rate, bps = le16(b), le16(b[2:]), le32(b[4:]), le16(b[14:])
			if format == 0xFFFE && size >= 26 { // WAVE_FORMAT_EXTENSIBLE
				format = le16(b[24:])
			}
			haveFmt = true
		case "data":
			if !haveFmt {
				return nil, fmt.Errorf("WAV with 'data' before 'fmt '")
			}
			if nch == 0 {
				return nil, fmt.Errorf("WAV without channels")
			}
			if size == 0 || size == 0xFFFFFFFF { // streaming/unknown size
				size = 0x7FFFFFFF
			}
			raw, err := io.ReadAll(io.LimitReader(br, int64(size)))
			if err != nil {
				return nil, err
			}
			return wavSamples(raw, format, int(nch), int(rate), int(bps))
		default:
			if _, err := br.Discard(int(size + size%2)); err != nil {
				return nil, err
			}
		}
	}
}

func wavSamples(raw []byte, format uint16, nch, rate, bps int) (*DecodedAudio, error) {
	bytesPer := bps / 8
	if bytesPer == 0 {
		return nil, fmt.Errorf("WAV with %d bits per sample not supported", bps)
	}
	n := len(raw) / (bytesPer * nch)
	planes := make([][]int32, nch)
	for c := range planes {
		planes[c] = make([]int32, n)
	}

	outBps := 16
	if bps > 16 {
		outBps = 24
	}

	for i := 0; i < n; i++ {
		for c := 0; c < nch; c++ {
			b := raw[(i*nch+c)*bytesPer:]
			var v int32
			switch {
			case format == 3 && bps == 32:
				bits := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
				v = floatToInt(math.Float32frombits(bits), outBps)
			case format == 3 && bps == 64:
				var bits uint64
				for k := 7; k >= 0; k-- {
					bits = bits<<8 | uint64(b[k])
				}
				v = floatToInt(float32(math.Float64frombits(bits)), outBps)
			case format == 1 && bps == 8:
				v = (int32(b[0]) - 128) << 8
			case format == 1 && bps == 16:
				v = int32(int16(uint16(b[0]) | uint16(b[1])<<8))
			case format == 1 && bps == 24:
				v = int32(uint32(b[0])<<8|uint32(b[1])<<16|uint32(b[2])<<24) >> 8
			case format == 1 && bps == 32:
				v = int32(uint32(b[0])|uint32(b[1])<<8|uint32(b[2])<<16|uint32(b[3])<<24) >> 8
			default:
				return nil, fmt.Errorf("WAV format %d / %d bits not supported", format, bps)
			}
			planes[c][i] = v
		}
	}
	return &DecodedAudio{SampleRate: rate, PCM: toStereo(make([]int32, 0, n*2), planes), BitsPerSample: outBps}, nil
}

// ExtractChannelSamples extracts a slice of consecutive samples for a channel (0 = L, 1 = R)
// centered on centerSampleIndex with length windowSize. Out-of-bounds positions are zero-filled.
func (d *DecodedAudio) ExtractChannelSamples(channel int, centerSampleIndex int, windowSize int, dest []float32) {
	halfWin := windowSize / 2
	start := centerSampleIndex - halfWin
	total := d.TotalFrames()
	for i := 0; i < windowSize; i++ {
		sampleIdx := start + i
		if sampleIdx < 0 || sampleIdx >= total {
			dest[i] = 0
		} else {
			dest[i] = d.Samples[sampleIdx*2+channel]
		}
	}
}

// ComputeRMS computes the root mean square over a window of samples
func ComputeRMS(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		v := float64(s)
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(samples)))
}
