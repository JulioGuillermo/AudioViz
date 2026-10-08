// Package video loads the background: images and GIFs in pure Go; videos with a minimal
// statically linked FFmpeg (nothing needs to be installed).
package video

import (
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// BackgroundReader is the common interface for feeding background frames
type BackgroundReader interface {
	Width() int
	Height() int
	// FrameAt returns the background frame for time t (seconds) and an index
	// that changes only when the image changes (to avoid re-uploading it to the GPU)
	FrameAt(t float64) (*image.RGBA, int)
	Close() error
}

// ProbeResult holds the dimensions and detected type of a media file
type ProbeResult struct {
	IsAnimated bool
	IsVideo    bool
	Width      int
	Height     int
	Duration   float64 // s (video)
}

var videoExts = map[string]bool{".mp4": true, ".mkv": true, ".mov": true, ".webm": true, ".avi": true, ".m4v": true, ".ts": true}

// IsVideo reports whether the path is a video based on its extension
func IsVideo(path string) bool { return videoExts[strings.ToLower(filepath.Ext(path))] }

// ProbeMedia determines the background dimensions
func ProbeMedia(filePath string) (*ProbeResult, error) {
	if IsVideo(filePath) {
		w, h, dur, err := probeVideo(filePath)
		if err != nil {
			return nil, err
		}
		return &ProbeResult{IsVideo: true, Width: w / 2 * 2, Height: h / 2 * 2, Duration: dur}, nil
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("'%s' is not recognized as an image (PNG, JPEG, WebP, BMP, GIF): %w", filePath, err)
	}
	return &ProbeResult{IsAnimated: format == "gif", Width: cfg.Width / 2 * 2, Height: cfg.Height / 2 * 2}, nil
}

// Fit scales src to (w, h) as a background is placed (cover/contain/stretch), over black: a
// depth map lines up with its image this way
func Fit(src image.Image, w, h int, mode string) *image.RGBA { return fit(src, w, h, mode) }

// fit scales src to (w, h) according to the cover/contain/stretch mode, over black
func fit(src image.Image, w, h int, mode string) *image.RGBA {
	return fitOn(src, w, h, mode, image.Black)
}

// fitOn is fit over a given backdrop (image.Transparent keeps the transparency)
func fitOn(src image.Image, w, h int, mode string, backdrop image.Image) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), backdrop, image.Point{}, draw.Src)
	ow, oh := src.Bounds().Dx(), src.Bounds().Dy()
	dst := out.Bounds()
	if mode != "stretch" {
		sx, sy := float64(w)/float64(ow), float64(h)/float64(oh)
		s := sx
		if (mode == "contain") == (sy < sx) {
			s = sy
		}
		dw, dh := int(float64(ow)*s+0.5), int(float64(oh)*s+0.5)
		ox, oy := (w-dw)/2, (h-dh)/2
		dst = image.Rect(ox, oy, ox+dw, oy+dh)
	}
	xdraw.CatmullRom.Scale(out, dst, src, src.Bounds(), xdraw.Over, nil)
	return out
}

type staticReader struct {
	frame *image.RGBA
}

func (r *staticReader) Width() int                         { return r.frame.Bounds().Dx() }
func (r *staticReader) Height() int                        { return r.frame.Bounds().Dy() }
func (r *staticReader) FrameAt(float64) (*image.RGBA, int) { return r.frame, 0 }
func (r *staticReader) Close() error                       { return nil }

type gifReader struct {
	frames []*image.RGBA
	ends   []float64 // time (s) at which each frame ends
	total  float64
	loop   bool
}

func (r *gifReader) Width() int   { return r.frames[0].Bounds().Dx() }
func (r *gifReader) Height() int  { return r.frames[0].Bounds().Dy() }
func (r *gifReader) Close() error { return nil }

func (r *gifReader) FrameAt(t float64) (*image.RGBA, int) {
	if r.loop && r.total > 0 {
		for t >= r.total {
			t -= r.total
		}
	}
	for i, e := range r.ends {
		if t < e {
			return r.frames[i], i
		}
	}
	last := len(r.frames) - 1
	return r.frames[last], last
}

// NewBackgroundReader loads the image or animated GIF already scaled to the output
func NewBackgroundReader(filePath string, w, h int, fitMode string, loop bool) (BackgroundReader, error) {
	if IsVideo(filePath) {
		return newVideoReader(filePath, w, h, fitMode, loop)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("could not open background '%s': %w", filePath, err)
	}
	defer f.Close()

	if _, format, err := image.DecodeConfig(f); err == nil && format == "gif" {
		f.Seek(0, 0)
		g, err := gif.DecodeAll(f)
		if err != nil {
			return nil, err
		}
		if len(g.Image) > 1 {
			return newGIFReader(g, w, h, fitMode, loop, image.Black), nil
		}
	}
	f.Seek(0, 0)
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("error decoding image '%s': %w", filePath, err)
	}
	return &staticReader{frame: fit(img, w, h, fitMode)}, nil
}

func newGIFReader(g *gif.GIF, w, h int, fitMode string, loop bool, backdrop image.Image) *gifReader {
	canvas := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	r := &gifReader{loop: loop}
	for i, fr := range g.Image {
		prev := image.NewRGBA(canvas.Bounds())
		copy(prev.Pix, canvas.Pix)
		draw.Draw(canvas, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
		r.frames = append(r.frames, fitOn(canvas, w, h, fitMode, backdrop))
		d := 0.1
		if i < len(g.Delay) && g.Delay[i] > 1 {
			d = float64(g.Delay[i]) / 100
		}
		r.total += d
		r.ends = append(r.ends, r.total)
		if i < len(g.Disposal) {
			switch g.Disposal[i] {
			case gif.DisposalBackground:
				draw.Draw(canvas, fr.Bounds(), image.Transparent, image.Point{}, draw.Src)
			case gif.DisposalPrevious:
				copy(canvas.Pix, prev.Pix)
			}
		}
	}
	return r
}

// NewLayerReader loads an overlay file (image, GIF or video) at its own size, but no larger than
// maxW×maxH (aspect ratio kept), with its transparency (videos are opaque). Videos and GIFs start
// at t = 0; loop repeats them.
func NewLayerReader(filePath string, maxW, maxH int, loop bool) (BackgroundReader, error) {
	pr, err := ProbeMedia(filePath)
	if err != nil {
		return nil, err
	}
	w, h := max(pr.Width, 2), max(pr.Height, 2)
	if s := min(1, float64(maxW)/float64(w), float64(maxH)/float64(h)); s < 1 {
		w, h = max(2, int(float64(w)*s+0.5)), max(2, int(float64(h)*s+0.5))
	}
	if pr.IsVideo {
		return newVideoReader(filePath, w, h, "stretch", loop)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if pr.IsAnimated {
		g, err := gif.DecodeAll(f)
		if err != nil {
			return nil, err
		}
		if len(g.Image) > 1 {
			return newGIFReader(g, w, h, "stretch", loop, image.Transparent), nil
		}
		f.Seek(0, 0)
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("error decoding image '%s': %w", filePath, err)
	}
	return &staticReader{frame: fitOn(img, w, h, "stretch", image.Transparent)}, nil
}
