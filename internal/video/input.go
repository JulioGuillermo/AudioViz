// Package video carga el fondo (imagen fija o GIF animado) en Go puro, sin ffmpeg.
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

// BackgroundReader es la interfaz común para alimentar cuadros de fondo
type BackgroundReader interface {
	Width() int
	Height() int
	// FrameAt devuelve el cuadro de fondo para el instante t (segundos) y un índice
	// que cambia solo cuando cambia la imagen (para no re-subirla a la GPU)
	FrameAt(t float64) (*image.RGBA, int)
	Close() error
}

// ProbeResult contiene dimensiones y tipo detectado de un medio
type ProbeResult struct {
	IsAnimated bool
	Width      int
	Height     int
}

var videoExts = map[string]bool{".mp4": true, ".mkv": true, ".mov": true, ".webm": true, ".avi": true, ".m4v": true}

// ProbeMedia determina las dimensiones del fondo
func ProbeMedia(filePath string) (*ProbeResult, error) {
	if videoExts[strings.ToLower(filepath.Ext(filePath))] {
		return nil, fmt.Errorf("los fondos de video (%s) no están soportados sin ffmpeg; usa una imagen o un GIF animado", filepath.Ext(filePath))
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("no se reconoce '%s' como imagen (PNG, JPEG, WebP, BMP, GIF): %w", filePath, err)
	}
	return &ProbeResult{IsAnimated: format == "gif", Width: cfg.Width / 2 * 2, Height: cfg.Height / 2 * 2}, nil
}

// fit escala src a (w, h) según el modo cover/contain/stretch
func fit(src image.Image, w, h int, mode string) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(out, out.Bounds(), image.Black, image.Point{}, draw.Src)
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

func (r *staticReader) Width() int                             { return r.frame.Bounds().Dx() }
func (r *staticReader) Height() int                            { return r.frame.Bounds().Dy() }
func (r *staticReader) FrameAt(float64) (*image.RGBA, int)     { return r.frame, 0 }
func (r *staticReader) Close() error                           { return nil }

type gifReader struct {
	frames []*image.RGBA
	ends   []float64 // instante (s) en que termina cada cuadro
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

// NewBackgroundReader carga la imagen o el GIF animado ya escalado a la salida
func NewBackgroundReader(filePath string, w, h int, fitMode string, loop bool) (BackgroundReader, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir el fondo '%s': %w", filePath, err)
	}
	defer f.Close()

	if _, format, err := image.DecodeConfig(f); err == nil && format == "gif" {
		f.Seek(0, 0)
		g, err := gif.DecodeAll(f)
		if err != nil {
			return nil, err
		}
		if len(g.Image) > 1 {
			return newGIFReader(g, w, h, fitMode, loop), nil
		}
	}
	f.Seek(0, 0)
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("error decodificando imagen '%s': %w", filePath, err)
	}
	return &staticReader{frame: fit(img, w, h, fitMode)}, nil
}

func newGIFReader(g *gif.GIF, w, h int, fitMode string, loop bool) *gifReader {
	canvas := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	r := &gifReader{loop: loop}
	for i, fr := range g.Image {
		prev := image.NewRGBA(canvas.Bounds())
		copy(prev.Pix, canvas.Pix)
		draw.Draw(canvas, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
		r.frames = append(r.frames, fit(canvas, w, h, fitMode))
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
