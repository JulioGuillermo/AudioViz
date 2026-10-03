// Package mux implements a minimal pure-Go MP4 (ISO BMFF) muxer:
// one H.264 video track (avc1/avcC) and one FLAC audio track (fLaC/dfLa).
// The result is written with 'moov' first (faststart) so it can be played
// when streaming and in browsers.
package mux

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

type videoSample struct {
	off      int64 // offset within the temporary file
	size     uint32
	pts, dts int64 // in frame units
	key      bool
}

// AudioTrack describes an already encoded (FLAC) audio track in memory
type AudioTrack struct {
	SampleRate    int
	Channels      int
	BitsPerSample int
	StreamInfo    [34]byte
	Frames        [][]byte
	FrameSamples  []int
}

type MP4Writer struct {
	path     string
	tmpPath  string
	tmp      *os.File
	tmpW     *bufio.Writer
	tmpOff   int64
	width    int
	height   int
	fps      int
	sps, pps []byte
	samples  []videoSample
	audio    *AudioTrack
}

func NewMP4Writer(path string, width, height, fps int) (*MP4Writer, error) {
	tmpPath := path + ".video.tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return nil, err
	}
	return &MP4Writer{path: path, tmpPath: tmpPath, tmp: f, tmpW: bufio.NewWriterSize(f, 4<<20),
		width: width, height: height, fps: fps}, nil
}

// SetAVCConfig sets SPS and PPS (without start code)
func (m *MP4Writer) SetAVCConfig(sps, pps []byte) {
	m.sps = append([]byte(nil), sps...)
	m.pps = append([]byte(nil), pps...)
}

func (m *MP4Writer) SetAudio(a *AudioTrack) { m.audio = a }

// WriteVideoAnnexB takes an access unit in Annex B format (00 00 01 / 00 00 00 01),
// drops SPS/PPS (already known parameters) and AUD, keeps SEI, and stores it in AVCC format (4-byte length).
func (m *MP4Writer) WriteVideoAnnexB(au []byte, pts, dts int64, key bool) error {
	var size uint32
	for _, nal := range SplitAnnexB(au) {
		if len(nal) == 0 {
			continue
		}
		switch nal[0] & 0x1F {
		case 7: // SPS
			if m.sps == nil {
				m.sps = append([]byte(nil), nal...)
			}
			continue
		case 8: // PPS
			if m.pps == nil {
				m.pps = append([]byte(nil), nal...)
			}
			continue
		case 9: // AUD
			continue
		}
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(nal)))
		if _, err := m.tmpW.Write(l[:]); err != nil {
			return err
		}
		if _, err := m.tmpW.Write(nal); err != nil {
			return err
		}
		size += 4 + uint32(len(nal))
	}
	m.samples = append(m.samples, videoSample{off: m.tmpOff, size: size, pts: pts, dts: dts, key: key})
	m.tmpOff += int64(size)
	return nil
}

// SplitAnnexB splits an Annex B stream into NAL units (without start codes)
func SplitAnnexB(b []byte) [][]byte {
	var nals [][]byte
	start := -1
	i := 0
	for i+2 < len(b) {
		if b[i] == 0 && b[i+1] == 0 && b[i+2] == 1 {
			if start >= 0 {
				end := i
				if end > start && b[end-1] == 0 {
					end--
				}
				nals = append(nals, b[start:end])
			}
			i += 3
			start = i
			continue
		}
		i++
	}
	if start >= 0 && start < len(b) {
		nals = append(nals, b[start:])
	}
	return nals
}

// Close assembles the final file: ftyp + moov + mdat (interleaved per second)
func (m *MP4Writer) Close() error {
	if err := m.tmpW.Flush(); err != nil {
		return err
	}
	defer os.Remove(m.tmpPath)
	defer m.tmp.Close()
	if len(m.samples) == 0 {
		return fmt.Errorf("no video frames were written")
	}
	if m.sps == nil || m.pps == nil {
		return fmt.Errorf("missing H.264 SPS/PPS")
	}

	// Normalize DTS so it starts at 0
	dts0 := m.samples[0].dts
	for i := range m.samples {
		m.samples[i].dts -= dts0
		m.samples[i].pts -= dts0
	}

	vts := videoTimescale(m.fps)
	vtick := int64(vts / m.fps)

	// ---- Interleaved chunk plan (1 s per chunk)
	type chunk struct {
		audio      bool
		first, cnt int
		size       int64
	}
	var chunks []chunk
	vi, ai := 0, 0
	var audioPos int64 // in samples
	for sec := int64(0); vi < len(m.samples) || (m.audio != nil && ai < len(m.audio.Frames)); sec++ {
		c := chunk{first: vi}
		for vi < len(m.samples) && m.samples[vi].dts < (sec+1)*int64(m.fps) {
			c.size += int64(m.samples[vi].size)
			vi++
		}
		if c.cnt = vi - c.first; c.cnt > 0 {
			chunks = append(chunks, c)
		}
		if m.audio != nil {
			ac := chunk{audio: true, first: ai}
			for ai < len(m.audio.Frames) && audioPos < (sec+1)*int64(m.audio.SampleRate) {
				ac.size += int64(len(m.audio.Frames[ai]))
				audioPos += int64(m.audio.FrameSamples[ai])
				ai++
			}
			if ac.cnt = ai - ac.first; ac.cnt > 0 {
				chunks = append(chunks, ac)
			}
		}
	}

	build := func(mdatStart int64) []byte {
		var vOffs, aOffs []int64
		var vPer, aPer []int
		off := mdatStart + 16
		for _, c := range chunks {
			if c.audio {
				aOffs = append(aOffs, off)
				aPer = append(aPer, c.cnt)
			} else {
				vOffs = append(vOffs, off)
				vPer = append(vPer, c.cnt)
			}
			off += c.size
		}
		return m.buildMoov(vts, vtick, vOffs, vPer, aOffs, aPer)
	}

	ftyp := box("ftyp", []byte("isom"), u32(512), []byte("isomiso2avc1mp41"))
	moovLen := int64(len(build(0)))
	mdatStart := int64(len(ftyp)) + moovLen
	moov := build(mdatStart)

	out, err := os.Create(m.path)
	if err != nil {
		return err
	}
	defer out.Close()
	w := bufio.NewWriterSize(out, 4<<20)
	w.Write(ftyp)
	w.Write(moov)

	var mdatSize int64 = 16
	for _, c := range chunks {
		mdatSize += c.size
	}
	w.Write(u32(1))
	w.Write([]byte("mdat"))
	w.Write(u64(uint64(mdatSize)))

	for _, c := range chunks {
		if c.audio {
			for _, f := range m.audio.Frames[c.first : c.first+c.cnt] {
				w.Write(f)
			}
			continue
		}
		if _, err := m.tmp.Seek(m.samples[c.first].off, io.SeekStart); err != nil {
			return err
		}
		if _, err := io.CopyN(w, m.tmp, c.size); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return out.Close()
}

func videoTimescale(fps int) int {
	if 90000%fps == 0 {
		return 90000
	}
	return fps * 1000
}

func (m *MP4Writer) buildMoov(vts int, vtick int64, vOffs []int64, vPer []int, aOffs []int64, aPer []int) []byte {
	const movieTS = 1000
	n := len(m.samples)
	vDur := int64(n) * vtick
	videoDurMs := vDur * movieTS / int64(vts)

	// ---------- Video track
	var stts, ctts, stss, stsz []byte
	stts = append(stts, u32(1)...)
	stts = append(stts, u32(uint32(n))...)
	stts = append(stts, u32(uint32(vtick))...)

	var cttsEntries [][2]uint32
	needCtts := false
	for _, s := range m.samples {
		off := uint32((s.pts - s.dts) * vtick)
		if off != 0 {
			needCtts = true
		}
		if len(cttsEntries) > 0 && cttsEntries[len(cttsEntries)-1][1] == off {
			cttsEntries[len(cttsEntries)-1][0]++
		} else {
			cttsEntries = append(cttsEntries, [2]uint32{1, off})
		}
	}
	ctts = append(ctts, u32(uint32(len(cttsEntries)))...)
	for _, e := range cttsEntries {
		ctts = append(ctts, u32(e[0])...)
		ctts = append(ctts, u32(e[1])...)
	}

	var keys []uint32
	stsz = append(stsz, u32(0)...)
	stsz = append(stsz, u32(uint32(n))...)
	for i, s := range m.samples {
		if s.key {
			keys = append(keys, uint32(i+1))
		}
		stsz = append(stsz, u32(s.size)...)
	}
	stss = append(stss, u32(uint32(len(keys)))...)
	for _, k := range keys {
		stss = append(stss, u32(k)...)
	}

	// avcC
	avcc := []byte{1, m.sps[1], m.sps[2], m.sps[3], 0xFF, 0xE1}
	avcc = append(avcc, u16(uint16(len(m.sps)))...)
	avcc = append(avcc, m.sps...)
	avcc = append(avcc, 1)
	avcc = append(avcc, u16(uint16(len(m.pps)))...)
	avcc = append(avcc, m.pps...)
	if p := m.sps[1]; p == 100 || p == 110 || p == 122 || p == 144 {
		avcc = append(avcc, 0xFC|1, 0xF8|0, 0xF8|0, 0) // 4:2:0, 8 bits
	}

	avc1 := concat(
		make([]byte, 6), u16(1), // reserved + data_reference_index
		make([]byte, 16), // pre_defined/reserved
		u16(uint16(m.width)), u16(uint16(m.height)),
		u32(0x00480000), u32(0x00480000), u32(0), u16(1),
		make([]byte, 32), // compressorname
		u16(0x18), u16(0xFFFF),
		box("avcC", avcc),
	)

	// Edit list to compensate for B-frame delay (first PTS)
	firstPTS := m.samples[0].pts
	for _, s := range m.samples {
		if s.pts < firstPTS {
			firstPTS = s.pts
		}
	}
	elst := fullbox("elst", 0, 0, u32(1), u32(uint32(videoDurMs)), u32(uint32(firstPTS*vtick)), u32(0x00010000))

	vstbl := [][]byte{
		fullbox("stsd", 0, 0, u32(1), box("avc1", avc1)),
		fullbox("stts", 0, 0, stts),
	}
	if needCtts {
		vstbl = append(vstbl, fullbox("ctts", 0, 0, ctts))
	}
	vstbl = append(vstbl,
		fullbox("stss", 0, 0, stss),
		fullbox("stsc", 0, 0, stsc(vPer)),
		fullbox("stsz", 0, 0, stsz),
		fullbox("co64", 0, 0, co64(vOffs)),
	)

	videoTrak := box("trak",
		tkhd(1, videoDurMs, 0, m.width, m.height),
		box("edts", elst),
		box("mdia",
			mdhd(vts, vDur),
			hdlr("vide", "VideoHandler"),
			box("minf",
				fullbox("vmhd", 0, 1, make([]byte, 8)),
				dinf(),
				box("stbl", vstbl...),
			),
		),
	)

	traks := [][]byte{videoTrak}
	maxDurMs := videoDurMs

	// ---------- FLAC audio track
	if a := m.audio; a != nil && len(a.Frames) > 0 {
		var aDur int64
		var asz []byte
		asz = append(asz, u32(0)...)
		asz = append(asz, u32(uint32(len(a.Frames)))...)
		var sttsE [][2]uint32
		for i, f := range a.Frames {
			asz = append(asz, u32(uint32(len(f)))...)
			d := uint32(a.FrameSamples[i])
			aDur += int64(d)
			if len(sttsE) > 0 && sttsE[len(sttsE)-1][1] == d {
				sttsE[len(sttsE)-1][0]++
			} else {
				sttsE = append(sttsE, [2]uint32{1, d})
			}
		}
		var ast []byte
		ast = append(ast, u32(uint32(len(sttsE)))...)
		for _, e := range sttsE {
			ast = append(ast, u32(e[0])...)
			ast = append(ast, u32(e[1])...)
		}
		rateField := uint32(a.SampleRate) << 16
		if a.SampleRate > 65535 {
			rateField = 0
		}
		dfla := fullbox("dfLa", 0, 0, []byte{0x80, 0, 0, 34}, a.StreamInfo[:])
		flacEntry := concat(
			make([]byte, 6), u16(1),
			make([]byte, 8),
			u16(uint16(a.Channels)), u16(uint16(a.BitsPerSample)),
			u16(0), u16(0), u32(rateField),
			dfla,
		)
		aDurMs := aDur * movieTS / int64(a.SampleRate)
		if aDurMs > maxDurMs {
			maxDurMs = aDurMs
		}
		traks = append(traks, box("trak",
			tkhd(2, aDurMs, 0x0100, 0, 0),
			box("mdia",
				mdhd(a.SampleRate, aDur),
				hdlr("soun", "SoundHandler"),
				box("minf",
					fullbox("smhd", 0, 0, make([]byte, 4)),
					dinf(),
					box("stbl",
						fullbox("stsd", 0, 0, u32(1), box("fLaC", flacEntry)),
						fullbox("stts", 0, 0, ast),
						fullbox("stsc", 0, 0, stsc(aPer)),
						fullbox("stsz", 0, 0, asz),
						fullbox("co64", 0, 0, co64(aOffs)),
					),
				),
			),
		))
	}

	mvhd := fullbox("mvhd", 0, 0,
		u32(0), u32(0), u32(movieTS), u32(uint32(maxDurMs)),
		u32(0x00010000), u16(0x0100), make([]byte, 10),
		matrix(), make([]byte, 24), u32(uint32(len(traks)+1)),
	)
	return box("moov", append([][]byte{mvhd}, traks...)...)
}

func stsc(perChunk []int) []byte {
	var entries [][2]uint32
	for i, c := range perChunk {
		if len(entries) > 0 && entries[len(entries)-1][1] == uint32(c) {
			continue
		}
		entries = append(entries, [2]uint32{uint32(i + 1), uint32(c)})
	}
	b := u32(uint32(len(entries)))
	for _, e := range entries {
		b = append(b, u32(e[0])...)
		b = append(b, u32(e[1])...)
		b = append(b, u32(1)...)
	}
	return b
}

func co64(offs []int64) []byte {
	b := u32(uint32(len(offs)))
	for _, o := range offs {
		b = append(b, u64(uint64(o))...)
	}
	return b
}

func tkhd(id uint32, durMs int64, volume uint16, w, h int) []byte {
	return fullbox("tkhd", 0, 3,
		u32(0), u32(0), u32(id), u32(0), u32(uint32(durMs)),
		make([]byte, 8), u16(0), u16(0), u16(volume), u16(0),
		matrix(), u32(uint32(w)<<16), u32(uint32(h)<<16),
	)
}

func mdhd(ts int, dur int64) []byte {
	return fullbox("mdhd", 0, 0, u32(0), u32(0), u32(uint32(ts)), u32(uint32(dur)), u16(0x55C4), u16(0))
}

func hdlr(kind, name string) []byte {
	return fullbox("hdlr", 0, 0, u32(0), []byte(kind), make([]byte, 12), []byte(name), []byte{0})
}

func dinf() []byte {
	return box("dinf", fullbox("dref", 0, 0, u32(1), fullbox("url ", 0, 1)))
}

func matrix() []byte {
	return concat(u32(0x00010000), u32(0), u32(0), u32(0), u32(0x00010000), u32(0), u32(0), u32(0), u32(0x40000000))
}

func box(typ string, parts ...[]byte) []byte {
	body := concat(parts...)
	return concat(u32(uint32(8+len(body))), []byte(typ), body)
}

func fullbox(typ string, version uint8, flags uint32, parts ...[]byte) []byte {
	vf := u32(uint32(version)<<24 | flags&0xFFFFFF)
	return box(typ, append([][]byte{vf}, parts...)...)
}

func concat(parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func u16(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }
func u32(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func u64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }
