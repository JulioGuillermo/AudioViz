package video

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// BackgroundReader es la interfaz común para alimentar cuadros de fondo (imagen o video)
type BackgroundReader interface {
	Width() int
	Height() int
	GetFrame(frameIndex int) (*image.RGBA, error)
	Close() error
}

// ProbeResult contiene dimensiones y tipo detectado de un medio
type ProbeResult struct {
	IsVideo  bool
	Width    int
	Height   int
	Duration float64
}

// ProbeMedia analiza un archivo para determinar si es imagen o video y sus dimensiones
func ProbeMedia(filePath string) (*ProbeResult, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	isImgExt := ext == ".png" || ext == ".jpg" || ext == ".jpeg" || ext == ".webp" || ext == ".bmp"

	// Probar si se puede abrir como imagen estándar de Go
	if isImgExt || ext == "" {
		f, err := os.Open(filePath)
		if err == nil {
			defer f.Close()
			cfg, _, err := image.DecodeConfig(f)
			if err == nil && cfg.Width > 0 && cfg.Height > 0 {
				w := (cfg.Width / 2) * 2
				h := (cfg.Height / 2) * 2
				return &ProbeResult{
					IsVideo: false,
					Width:   w,
					Height:  h,
				}, nil
			}
		}
	}

	// Usar ffprobe para inspeccionar videos o imágenes no estándar
	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,duration,codec_type",
		"-of", "json",
		filePath,
	)

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error analizando medio con ffprobe: %w", err)
	}

	var data struct {
		Streams []struct {
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			CodecType string `json:"codec_type"`
		} `json:"streams"`
	}

	if err := json.Unmarshal(out, &data); err != nil || len(data.Streams) == 0 {
		return nil, fmt.Errorf("no se encontró flujo de video o imagen en '%s'", filePath)
	}

	st := data.Streams[0]
	w := (st.Width / 2) * 2
	h := (st.Height / 2) * 2

	// Comprobar si tiene más de 1 cuadro para saber si es video
	isVideo := !isImgExt

	return &ProbeResult{
		IsVideo: isVideo,
		Width:   w,
		Height:  h,
	}, nil
}

// ----------------------------------------------------
// LECTOR DE IMAGEN ESTÁTICA
// ----------------------------------------------------

type ImageBackgroundReader struct {
	width     int
	height    int
	baseFrame *image.RGBA
}

func NewImageBackgroundReader(filePath string, targetWidth, targetHeight int, fitMode string) (*ImageBackgroundReader, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir la imagen '%s': %w", filePath, err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("error decodificando imagen '%s': %w", filePath, err)
	}

	origW := img.Bounds().Dx()
	origH := img.Bounds().Dy()

	if targetWidth <= 0 || targetHeight <= 0 {
		targetWidth = (origW / 2) * 2
		targetHeight = (origH / 2) * 2
	}

	out := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))

	switch fitMode {
	case "stretch":
		xdraw.ApproxBiLinear.Scale(out, out.Bounds(), img, img.Bounds(), xdraw.Over, nil)
	case "contain":
		scale := mathMin(float64(targetWidth)/float64(origW), float64(targetHeight)/float64(origH))
		destW := int(float64(origW) * scale)
		destH := int(float64(origH) * scale)
		offsetX := (targetWidth - destW) / 2
		offsetY := (targetHeight - destH) / 2
		destRect := image.Rect(offsetX, offsetY, offsetX+destW, offsetY+destH)
		xdraw.ApproxBiLinear.Scale(out, destRect, img, img.Bounds(), xdraw.Over, nil)
	case "cover":
		fallthrough
	default:
		scale := mathMax(float64(targetWidth)/float64(origW), float64(targetHeight)/float64(origH))
		destW := int(float64(origW) * scale)
		destH := int(float64(origH) * scale)
		offsetX := (targetWidth - destW) / 2
		offsetY := (targetHeight - destH) / 2
		destRect := image.Rect(offsetX, offsetY, offsetX+destW, offsetY+destH)
		xdraw.ApproxBiLinear.Scale(out, destRect, img, img.Bounds(), xdraw.Over, nil)
	}

	return &ImageBackgroundReader{
		width:     targetWidth,
		height:    targetHeight,
		baseFrame: out,
	}, nil
}

func (r *ImageBackgroundReader) Width() int  { return r.width }
func (r *ImageBackgroundReader) Height() int { return r.height }

func (r *ImageBackgroundReader) GetFrame(frameIndex int) (*image.RGBA, error) {
	// Copiar el buffer para que cada worker pueda transformarlo de forma aislada
	copyImg := image.NewRGBA(r.baseFrame.Bounds())
	copy(copyImg.Pix, r.baseFrame.Pix)
	return copyImg, nil
}

func (r *ImageBackgroundReader) Close() error {
	return nil
}

// ----------------------------------------------------
// LECTOR DE VIDEO EN STREAMING (PIPE FFMPEG)
// ----------------------------------------------------

type VideoBackgroundReader struct {
	filePath  string
	width     int
	height    int
	fps       int
	loopVideo bool
	cmd       *exec.Cmd
	pipe      io.ReadCloser
	frameSize int
	rawBuf    []byte
}

func NewVideoBackgroundReader(filePath string, targetWidth, targetHeight, fps int, loopVideo bool) (*VideoBackgroundReader, error) {
	vr := &VideoBackgroundReader{
		filePath:  filePath,
		width:     targetWidth,
		height:    targetHeight,
		fps:       fps,
		loopVideo: loopVideo,
		frameSize: targetWidth * targetHeight * 4,
		rawBuf:    make([]byte, targetWidth*targetHeight*4),
	}

	if err := vr.startProcess(); err != nil {
		return nil, err
	}

	return vr, nil
}

func (vr *VideoBackgroundReader) startProcess() error {
	cmd := exec.Command("ffmpeg",
		"-v", "error",
		"-i", vr.filePath,
		"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=increase,crop=%d:%d,fps=%d",
			vr.width, vr.height, vr.width, vr.height, vr.fps),
		"-f", "rawvideo",
		"-pix_fmt", "rgba",
		"-",
	)

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error creando stdout pipe para ffmpeg video reader: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error iniciando ffmpeg video reader: %w", err)
	}

	vr.cmd = cmd
	vr.pipe = pipe
	return nil
}

func (vr *VideoBackgroundReader) Width() int  { return vr.width }
func (vr *VideoBackgroundReader) Height() int { return vr.height }

func (vr *VideoBackgroundReader) GetFrame(frameIndex int) (*image.RGBA, error) {
	_, err := io.ReadFull(vr.pipe, vr.rawBuf)
	if err == io.EOF || err == io.ErrUnexpectedEOF {
		if vr.loopVideo {
			// Reiniciar proceso de lectura de video
			_ = vr.Close()
			if err := vr.startProcess(); err != nil {
				return nil, fmt.Errorf("error reiniciando video en loop: %w", err)
			}
			_, err = io.ReadFull(vr.pipe, vr.rawBuf)
			if err != nil {
				return nil, fmt.Errorf("error leyendo tras reiniciar video: %w", err)
			}
		} else {
			return nil, io.EOF
		}
	} else if err != nil {
		return nil, err
	}

	frame := image.NewRGBA(image.Rect(0, 0, vr.width, vr.height))
	copy(frame.Pix, vr.rawBuf)
	return frame, nil
}

func (vr *VideoBackgroundReader) Close() error {
	if vr.pipe != nil {
		_ = vr.pipe.Close()
	}
	if vr.cmd != nil && vr.cmd.Process != nil {
		_ = vr.cmd.Process.Kill()
		_ = vr.cmd.Wait()
	}
	return nil
}

func mathMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func mathMax(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
