package video

import (
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"strings"
)

type VideoWriter struct {
	cmd       *exec.Cmd
	pipe      io.WriteCloser
	width     int
	height    int
	fps       int
	frameSize int
	CodecUsed string
}

func DetectBestCodec(requested string) string {
	req := strings.ToLower(requested)
	if req != "" && req != "auto" {
		return req
	}

	// 1. Probar VAAPI (Intel / AMD GPU en Linux)
	if _, err := os.Stat("/dev/dri/renderD128"); err == nil {
		testCmd := exec.Command("ffmpeg",
			"-vaapi_device", "/dev/dri/renderD128",
			"-f", "lavfi", "-i", "testsrc=duration=0.1:size=64x64:rate=30",
			"-vf", "format=nv12,hwupload",
			"-c:v", "h264_vaapi",
			"-f", "null", "-",
		)
		if testCmd.Run() == nil {
			return "h264_vaapi"
		}
	}

	// 2. Probar NVENC (NVIDIA GPU)
	testNvenc := exec.Command("ffmpeg",
		"-f", "lavfi", "-i", "testsrc=duration=0.1:size=64x64:rate=30",
		"-c:v", "h264_nvenc",
		"-f", "null", "-",
	)
	if testNvenc.Run() == nil {
		return "h264_nvenc"
	}

	// Fallback estándar por CPU
	return "libx264"
}

func NewVideoWriter(audioPath, outputPath string, width, height, fps int, codec, preset string, crf int, audioBitrate string, maxDurationSec float64) (*VideoWriter, error) {
	effectiveCodec := DetectBestCodec(codec)

	if preset == "" {
		preset = "fast"
	}
	if crf <= 0 {
		crf = 18
	}
	if audioBitrate == "" {
		audioBitrate = "320k"
	}

	var args []string
	args = append(args, "-y")

	// Si usamos aceleración por hardware VAAPI en GPU
	if effectiveCodec == "h264_vaapi" {
		args = append(args, "-vaapi_device", "/dev/dri/renderD128")
	}

	args = append(args,
		"-f", "rawvideo",
		"-pix_fmt", "rgba",
		"-s", fmt.Sprintf("%dx%d", width, height),
		"-r", fmt.Sprintf("%d", fps),
		"-i", "-", // Entrada de video desde stdin
	)

	if maxDurationSec > 0 {
		args = append(args, "-t", fmt.Sprintf("%.2f", maxDurationSec))
	}

	args = append(args, "-i", audioPath) // Entrada de audio original

	switch effectiveCodec {
	case "h264_vaapi":
		// Hardware upload directo a la memoria de la GPU
		args = append(args,
			"-vf", "format=nv12,hwupload",
			"-c:v", "h264_vaapi",
			"-qp", fmt.Sprintf("%d", crf+2), // En VAAPI qp suele equiparar a crf
		)
	case "h264_nvenc":
		args = append(args,
			"-c:v", "h264_nvenc",
			"-pix_fmt", "yuv420p",
			"-preset", "p4",
			"-cq", fmt.Sprintf("%d", crf),
		)
	case "h264_vulkan":
		args = append(args,
			"-init_hw_device", "vulkan=vk:0",
			"-filter_hw_device", "vk",
			"-vf", "format=nv12,hwupload",
			"-c:v", "h264_vulkan",
		)
	default: // libx264 software
		args = append(args,
			"-c:v", "libx264",
			"-pix_fmt", "yuv420p",
			"-preset", preset,
			"-crf", fmt.Sprintf("%d", crf),
		)
	}

	args = append(args,
		"-c:a", "aac",
		"-b:a", audioBitrate,
		"-shortest",
		outputPath,
	)

	cmd := exec.Command("ffmpeg", args...)
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("error creando stdin pipe para ffmpeg writer: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("error iniciando ffmpeg writer: %w", err)
	}

	return &VideoWriter{
		cmd:       cmd,
		pipe:      pipe,
		width:     width,
		height:    height,
		fps:       fps,
		frameSize: width * height * 4,
		CodecUsed: effectiveCodec,
	}, nil
}

func (vw *VideoWriter) WriteFrame(img *image.RGBA) error {
	_, err := vw.pipe.Write(img.Pix)
	return err
}

func (vw *VideoWriter) Close() error {
	if vw.pipe != nil {
		_ = vw.pipe.Close()
	}
	if vw.cmd != nil {
		return vw.cmd.Wait()
	}
	return nil
}
