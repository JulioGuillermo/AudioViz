package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os/exec"
)

const TargetSampleRate = 48000

// DecodedAudio contiene las muestras de audio PCM estéreo decodificadas
type DecodedAudio struct {
	SampleRate int
	Channels   int
	// Samples contiene las muestras normalizadas [-1.0, 1.0] entrelazadas: [L0, R0, L1, R1, ...]
	Samples    []float32
	DurationSec float64
}

// LeftSample retorna la muestra en el canal izquierdo para el índice de frame de audio i
func (d *DecodedAudio) LeftSample(i int) float32 {
	idx := i * 2
	if idx < 0 || idx >= len(d.Samples) {
		return 0
	}
	return d.Samples[idx]
}

// RightSample retorna la muestra en el canal derecho para el índice de frame de audio i
func (d *DecodedAudio) RightSample(i int) float32 {
	idx := i*2 + 1
	if idx < 0 || idx >= len(d.Samples) {
		return 0
	}
	return d.Samples[idx]
}

// TotalFrames retorna la cantidad de frames de audio (pares estéreo)
func (d *DecodedAudio) TotalFrames() int {
	return len(d.Samples) / 2
}

// DecodeAudio usa un pipe de FFmpeg para extraer PCM float32 estéreo a 48000Hz sin importar el formato de entrada
func DecodeAudio(filePath string, volume float64) (*DecodedAudio, error) {
	cmd := exec.Command(
		"ffmpeg",
		"-v", "error",
		"-i", filePath,
		"-f", "f32le",
		"-ac", "2",
		"-ar", fmt.Sprintf("%d", TargetSampleRate),
		"-",
	)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("error decodificando audio con ffmpeg: %v, stderr: %s", err, stderr.String())
	}

	rawBytes := stdout.Bytes()
	totalFloats := len(rawBytes) / 4
	if totalFloats == 0 {
		return nil, fmt.Errorf("el archivo de audio decodificado está vacío")
	}

	samples := make([]float32, totalFloats)
	reader := bytes.NewReader(rawBytes)
	if err := binary.Read(reader, binary.LittleEndian, &samples); err != nil && err != io.EOF {
		return nil, fmt.Errorf("error leyendo muestras float32: %w", err)
	}

	// Aplicar volumen y limitar para evitar clipping extremo
	if volume != 1.0 {
		vol := float32(volume)
		for i := range samples {
			val := samples[i] * vol
			if val > 1.0 {
				val = 1.0
			} else if val < -1.0 {
				val = -1.0
			}
			samples[i] = val
		}
	}

	totalStereoFrames := totalFloats / 2
	duration := float64(totalStereoFrames) / float64(TargetSampleRate)

	return &DecodedAudio{
		SampleRate:  TargetSampleRate,
		Channels:    2,
		Samples:     samples,
		DurationSec: duration,
	}, nil
}

// ExtractChannelSamples extrae un slice de muestras consecutivas para un canal (0 = L, 1 = R)
// con centro en centerSampleIndex y longitud windowSize. Si excede los límites, se rellena con 0.
func (d *DecodedAudio) ExtractChannelSamples(channel int, centerSampleIndex int, windowSize int, dest []float32) {
	halfWin := windowSize / 2
	start := centerSampleIndex - halfWin
	for i := 0; i < windowSize; i++ {
		sampleIdx := start + i
		if sampleIdx < 0 || sampleIdx >= d.TotalFrames() {
			dest[i] = 0
		} else {
			dest[i] = d.Samples[sampleIdx*2+channel]
		}
	}
}

// ComputeRMS calcula el valor cuadrático medio en una ventana de muestras
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
