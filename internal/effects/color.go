package effects

import (
	"image"
	"math"
	"runtime"
	"sync"
)

// ApplyColorAndContrast aplica ajustes de brillo, contraste, oscurecimiento y viñeta in-place y en paralelo
func ApplyColorAndContrast(img *image.RGBA, brightnessBoost, contrastBoost, darken, opacity, vignette float64) {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()

	centerX := float64(w) / 2.0
	centerY := float64(h) / 2.0
	maxDistSq := centerX*centerX + centerY*centerY

	c := contrastBoost * 128.0
	contrastFactor := (259.0 * (c + 255.0)) / (255.0 * (259.0 - c))
	brightAdd := (-darken + brightnessBoost) * 255.0

	if math.Abs(brightAdd) < 0.5 && math.Abs(contrastBoost) < 0.01 && vignette <= 0.01 && opacity >= 0.999 {
		return
	}

	numCPU := runtime.NumCPU()
	if numCPU > h {
		numCPU = h
	}

	var wg sync.WaitGroup
	chunk := h / numCPU

	for i := 0; i < numCPU; i++ {
		startY := i * chunk
		endY := startY + chunk
		if i == numCPU-1 {
			endY = h
		}

		wg.Add(1)
		go func(sY, eY int) {
			defer wg.Done()
			for y := sY; y < eY; y++ {
				dy := float64(y) - centerY
				row := y * img.Stride

				for x := 0; x < w; x++ {
					pIdx := row + x*4

					var vigFactor float64 = 1.0
					if vignette > 0.01 {
						dx := float64(x) - centerX
						distSq := dx*dx + dy*dy
						normDist := distSq / maxDistSq
						vigFactor = 1.0 - (normDist * vignette)
						if vigFactor < 0 {
							vigFactor = 0
						}
					}

					for ch := 0; ch < 3; ch++ {
						val := float64(img.Pix[pIdx+ch])
						val = contrastFactor*(val-128.0) + 128.0
						val = (val + brightAdd) * vigFactor
						val = val * opacity

						if val < 0 {
							val = 0
						} else if val > 255 {
							val = 255
						}
						img.Pix[pIdx+ch] = uint8(val)
					}
				}
			}
		}(startY, endY)
	}
	wg.Wait()
}
