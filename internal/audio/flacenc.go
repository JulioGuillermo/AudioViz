package audio

import (
	"crypto/md5"
	"runtime"
	"sync"
)

// Minimal pure-Go FLAC encoder (fixed predictors + partitioned Rice, stereo
// with L/S, R/S or M/S decorrelation). Used to put lossless audio into the output
// MP4 without depending on ffmpeg.

const FLACBlockSize = 4096

// FLACStream is the encoding result: STREAMINFO + independent frames.
type FLACStream struct {
	StreamInfo [34]byte
	Frames     [][]byte
	// Samples per frame (all FLACBlockSize except perhaps the last one)
	FrameSamples []int
	SampleRate   int
}

// EncodeFLAC encodes interleaved stereo PCM (bps = 16 or 24). maxFrames limits the duration (<=0 = all).
func EncodeFLAC(pcm []int32, sampleRate, bps int, maxSamples int) *FLACStream {
	total := len(pcm) / 2
	if maxSamples > 0 && maxSamples < total {
		total = maxSamples
	}
	nFrames := (total + FLACBlockSize - 1) / FLACBlockSize
	st := &FLACStream{Frames: make([][]byte, nFrames), FrameSamples: make([]int, nFrames), SampleRate: sampleRate}

	var wg sync.WaitGroup
	jobs := make(chan int, nFrames)
	for i := 0; i < nFrames; i++ {
		jobs <- i
	}
	close(jobs)
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			enc := newFrameEncoder(bps, sampleRate)
			for fi := range jobs {
				start := fi * FLACBlockSize
				n := FLACBlockSize
				if start+n > total {
					n = total - start
				}
				st.Frames[fi] = enc.encode(pcm[start*2:(start+n)*2], fi)
				st.FrameSamples[fi] = n
			}
		}()
	}
	wg.Wait()

	minF, maxF := 0, 0
	for i, f := range st.Frames {
		if i == 0 || len(f) < minF {
			minF = len(f)
		}
		if len(f) > maxF {
			maxF = len(f)
		}
	}

	// MD5 of the unencoded signal (interleaved little-endian samples)
	h := md5.New()
	bytesPer := bps / 8
	buf := make([]byte, 0, 1<<16)
	for i := 0; i < total*2; i++ {
		v := uint32(pcm[i])
		for b := 0; b < bytesPer; b++ {
			buf = append(buf, byte(v>>(8*b)))
		}
		if len(buf) >= 1<<16-8 {
			h.Write(buf)
			buf = buf[:0]
		}
	}
	h.Write(buf)

	var bw bitWriter
	bw.write(FLACBlockSize, 16)
	bw.write(FLACBlockSize, 16)
	bw.write(uint64(minF), 24)
	bw.write(uint64(maxF), 24)
	bw.write(uint64(sampleRate), 20)
	bw.write(2-1, 3)
	bw.write(uint64(bps-1), 5)
	bw.write(uint64(total), 36)
	copy(st.StreamInfo[:], bw.bytes())
	copy(st.StreamInfo[18:], h.Sum(nil))
	return st
}

// FileBytes serializes the stream as a native .flac file (useful for testing)
func (s *FLACStream) FileBytes() []byte {
	out := []byte("fLaC")
	out = append(out, 0x80, 0, 0, 34) // last metadata block, STREAMINFO, 34 bytes
	out = append(out, s.StreamInfo[:]...)
	for _, f := range s.Frames {
		out = append(out, f...)
	}
	return out
}

type frameEncoder struct {
	bps      int
	rateCode uint64
	bw       bitWriter
	chans    [4][]int64
	res      []int64
	u        []uint64
}

func newFrameEncoder(bps, rate int) *frameEncoder {
	codes := map[int]uint64{88200: 1, 176400: 2, 192000: 3, 8000: 4, 16000: 5, 22050: 6, 24000: 7, 32000: 8, 44100: 9, 48000: 10, 96000: 11}
	e := &frameEncoder{bps: bps, rateCode: codes[rate]}
	for i := range e.chans {
		e.chans[i] = make([]int64, FLACBlockSize)
	}
	e.res = make([]int64, FLACBlockSize)
	e.u = make([]uint64, FLACBlockSize)
	return e
}

func (e *frameEncoder) encode(pcm []int32, frameNum int) []byte {
	n := len(pcm) / 2
	L, R, M, S := e.chans[0][:n], e.chans[1][:n], e.chans[2][:n], e.chans[3][:n]
	for i := 0; i < n; i++ {
		l, r := int64(pcm[2*i]), int64(pcm[2*i+1])
		L[i], R[i] = l, r
		M[i] = (l + r) >> 1
		S[i] = l - r
	}

	// Cost estimate of each channel with its best predictor
	cost := [4]uint64{}
	order := [4]int{}
	for c := 0; c < 4; c++ {
		order[c], cost[c] = e.bestFixedOrder(e.chans[c][:n])
	}
	// 0001 independent, 1000 left/side, 1001 right/side, 1010 mid/side
	type choice struct {
		assign uint64
		a, b   int
	}
	choices := []choice{{1, 0, 1}, {8, 0, 3}, {9, 3, 1}, {10, 2, 3}}
	best := choices[0]
	bestCost := cost[0] + cost[1]
	for _, ch := range choices[1:] {
		if c := cost[ch.a] + cost[ch.b]; c < bestCost {
			best, bestCost = ch, c
		}
	}

	bw := &e.bw
	bw.reset()
	bw.write(0xFFF8, 16)
	bw.write(7, 4) // block size: 16 bits (n-1) at the end of the header
	bw.write(e.rateCode, 4)
	bw.write(best.assign, 4)
	if e.bps == 16 {
		bw.write(4, 3)
	} else {
		bw.write(6, 3)
	}
	bw.write(0, 1)
	writeUTF8(bw, uint64(frameNum))
	bw.write(uint64(n-1), 16)
	bw.write(uint64(crc8(bw.buf)), 8)

	for idx, c := range []int{best.a, best.b} {
		sideBits := 0
		// The "side" channel needs an extra bit (2nd channel in L/S and M/S, 1st in R/S)
		if (best.assign == 8 || best.assign == 10) && idx == 1 || best.assign == 9 && idx == 0 {
			sideBits = 1
		}
		e.writeSubframe(e.chans[c][:n], e.bps+sideBits, order[c])
	}
	bw.align()
	crc := crc16(bw.buf)
	bw.write(uint64(crc), 16)

	out := make([]byte, len(bw.buf))
	copy(out, bw.buf)
	return out
}

func (e *frameEncoder) bestFixedOrder(x []int64) (int, uint64) {
	n := len(x)
	bestOrder, bestSum := 0, ^uint64(0)
	for ord := 0; ord <= 4 && ord < n; ord++ {
		var sum uint64
		for i := ord; i < n; i++ {
			r := fixedResidual(x, i, ord)
			if r < 0 {
				r = -r
			}
			sum += uint64(r)
		}
		if sum < bestSum {
			bestOrder, bestSum = ord, sum
		}
	}
	return bestOrder, bestSum
}

func fixedResidual(x []int64, i, ord int) int64 {
	switch ord {
	case 0:
		return x[i]
	case 1:
		return x[i] - x[i-1]
	case 2:
		return x[i] - 2*x[i-1] + x[i-2]
	case 3:
		return x[i] - 3*x[i-1] + 3*x[i-2] - x[i-3]
	default:
		return x[i] - 4*x[i-1] + 6*x[i-2] - 4*x[i-3] + x[i-4]
	}
}

func (e *frameEncoder) writeSubframe(x []int64, bps, ord int) {
	bw := &e.bw
	n := len(x)

	constant := true
	for i := 1; i < n; i++ {
		if x[i] != x[0] {
			constant = false
			break
		}
	}
	if constant {
		bw.write(0, 1)
		bw.write(0, 6)
		bw.write(0, 1)
		bw.writeSigned(x[0], bps)
		return
	}
	if ord >= n {
		ord = 0
	}

	bw.write(0, 1)
	bw.write(uint64(8|ord), 6) // 001xxx = fixed predictor of order xxx
	bw.write(0, 1)
	for i := 0; i < ord; i++ {
		bw.writeSigned(x[i], bps)
	}

	u := e.u[:n]
	for i := ord; i < n; i++ {
		r := fixedResidual(x, i, ord)
		u[i] = uint64((r << 1) ^ (r >> 63))
	}

	// Choose the optimal partition order and Rice parameters
	bestPO, bestBits := 0, ^uint64(0)
	var bestParams [256]int
	var params [256]int
	for po := 0; po <= 8; po++ {
		parts := 1 << po
		if n%parts != 0 || n/parts <= ord {
			break
		}
		plen := n / parts
		var total uint64
		for p := 0; p < parts; p++ {
			s := p * plen
			if p == 0 {
				s = ord
			}
			k, bits := bestRice(u[s : (p+1)*plen])
			params[p] = k
			total += bits + 5
		}
		if total < bestBits {
			bestBits, bestPO = total, po
			copy(bestParams[:parts], params[:parts])
		}
	}

	parts := 1 << bestPO
	rice2 := false
	for p := 0; p < parts; p++ {
		if bestParams[p] > 14 {
			rice2 = true
		}
	}
	if rice2 {
		bw.write(1, 2)
	} else {
		bw.write(0, 2)
	}
	bw.write(uint64(bestPO), 4)
	plen := n / parts
	for p := 0; p < parts; p++ {
		k := bestParams[p]
		if rice2 {
			bw.write(uint64(k), 5)
		} else {
			bw.write(uint64(k), 4)
		}
		s := p * plen
		if p == 0 {
			s = ord
		}
		for _, v := range u[s : (p+1)*plen] {
			bw.writeRice(v, uint(k))
		}
	}
}

// bestRice picks the optimal Rice parameter for a block of folded residuals
func bestRice(u []uint64) (int, uint64) {
	if len(u) == 0 {
		return 0, 0
	}
	var sum uint64
	for _, v := range u {
		sum += v
	}
	mean := sum / uint64(len(u))
	k0 := 0
	for mean>>uint(k0+1) > 0 && k0 < 29 {
		k0++
	}
	bestK, bestBits := 0, ^uint64(0)
	for k := k0 - 1; k <= k0+1; k++ {
		if k < 0 || k > 30 {
			continue
		}
		bits := uint64(len(u)) * uint64(k+1)
		for _, v := range u {
			bits += v >> uint(k)
		}
		if bits < bestBits {
			bestK, bestBits = k, bits
		}
	}
	return bestK, bestBits
}

func writeUTF8(bw *bitWriter, v uint64) {
	switch {
	case v < 0x80:
		bw.write(v, 8)
	case v < 0x800:
		bw.write(0xC0|v>>6, 8)
		bw.write(0x80|v&0x3F, 8)
	case v < 0x10000:
		bw.write(0xE0|v>>12, 8)
		bw.write(0x80|(v>>6)&0x3F, 8)
		bw.write(0x80|v&0x3F, 8)
	case v < 0x200000:
		bw.write(0xF0|v>>18, 8)
		bw.write(0x80|(v>>12)&0x3F, 8)
		bw.write(0x80|(v>>6)&0x3F, 8)
		bw.write(0x80|v&0x3F, 8)
	default:
		bw.write(0xF8|v>>24, 8)
		bw.write(0x80|(v>>18)&0x3F, 8)
		bw.write(0x80|(v>>12)&0x3F, 8)
		bw.write(0x80|(v>>6)&0x3F, 8)
		bw.write(0x80|v&0x3F, 8)
	}
}

// bitWriter writes bits MSB first
type bitWriter struct {
	buf   []byte
	acc   uint64
	nbits uint
}

func (b *bitWriter) reset() { b.buf, b.acc, b.nbits = b.buf[:0], 0, 0 }

func (b *bitWriter) write(v uint64, n uint) {
	for n > 0 {
		take := n
		if take > 32 {
			take = 32
		}
		n -= take
		chunk := (v >> n) & (1<<take - 1)
		b.acc = b.acc<<take | chunk
		b.nbits += take
		for b.nbits >= 8 {
			b.nbits -= 8
			b.buf = append(b.buf, byte(b.acc>>b.nbits))
		}
	}
}

func (b *bitWriter) writeSigned(v int64, n int) { b.write(uint64(v)&(1<<uint(n)-1), uint(n)) }

func (b *bitWriter) writeRice(v uint64, k uint) {
	q := v >> k
	for q >= 32 {
		b.write(0, 32)
		q -= 32
	}
	b.write(1, uint(q)+1) // q zeros and a 1
	if k > 0 {
		b.write(v&(1<<k-1), k)
	}
}

func (b *bitWriter) align() {
	if b.nbits > 0 {
		b.write(0, 8-b.nbits)
	}
}

func (b *bitWriter) bytes() []byte {
	b.align()
	return b.buf
}

var crc8Table, crc16Table = func() ([256]uint8, [256]uint16) {
	var t8 [256]uint8
	var t16 [256]uint16
	for i := 0; i < 256; i++ {
		c := uint8(i)
		for j := 0; j < 8; j++ {
			if c&0x80 != 0 {
				c = c<<1 ^ 0x07
			} else {
				c <<= 1
			}
		}
		t8[i] = c
		c16 := uint16(i) << 8
		for j := 0; j < 8; j++ {
			if c16&0x8000 != 0 {
				c16 = c16<<1 ^ 0x8005
			} else {
				c16 <<= 1
			}
		}
		t16[i] = c16
	}
	return t8, t16
}()

func crc8(b []byte) uint8 {
	var c uint8
	for _, v := range b {
		c = crc8Table[c^v]
	}
	return c
}

func crc16(b []byte) uint16 {
	var c uint16
	for _, v := range b {
		c = c<<8 ^ crc16Table[byte(c>>8)^v]
	}
	return c
}
