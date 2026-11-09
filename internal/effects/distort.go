package effects

import (
	"image"
	"math"
	"math/rand"
	"runtime"
	"sync"
)

// parallelFor splits rows 0 to height across runtime.NumCPU() goroutines
func parallelFor(height int, fn func(startY, endY int)) {
	numCPU := runtime.NumCPU()
	if numCPU > height {
		numCPU = height
	}
	if numCPU <= 1 {
		fn(0, height)
		return
	}

	var wg sync.WaitGroup
	chunk := height / numCPU

	for i := 0; i < numCPU; i++ {
		startY := i * chunk
		endY := startY + chunk
		if i == numCPU-1 {
			endY = height
		}
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			fn(s, e)
		}(startY, endY)
	}
	wg.Wait()
}

// ApplyBassZoom applies a concentric zoom from the image center in parallel
func ApplyBassZoom(src *image.RGBA, zoom, shiftX, shiftY float64) *image.RGBA {
	if zoom <= 1.001 && shiftX == 0 && shiftY == 0 {
		return src
	}

	w := src.Bounds().Dx()
	h := src.Bounds().Dy()
	dst := image.NewRGBA(src.Bounds())

	centerX := float64(w) / 2.0
	centerY := float64(h) / 2.0
	invZoom := 1.0 / zoom

	parallelFor(h, func(startY, endY int) {
		for y := startY; y < endY; y++ {
			srcY := centerY + (float64(y)-centerY)*invZoom - shiftY
			iy0 := int(math.Floor(srcY))
			iy1 := iy0 + 1
			fy := srcY - float64(iy0)

			if iy0 < 0 {
				iy0 = 0
			}
			if iy0 > h-1 {
				iy0 = h - 1
			}
			if iy1 < 0 {
				iy1 = 0
			}
			if iy1 >= h {
				iy1 = h - 1
			}

			row0 := iy0 * src.Stride
			row1 := iy1 * src.Stride
			dstRow := y * dst.Stride

			for x := 0; x < w; x++ {
				srcX := centerX + (float64(x)-centerX)*invZoom - shiftX
				ix0 := int(math.Floor(srcX))
				ix1 := ix0 + 1
				fx := srcX - float64(ix0)

				if ix0 < 0 {
					ix0 = 0
				}
				if ix0 > w-1 {
					ix0 = w - 1
				}
				if ix1 < 0 {
					ix1 = 0
				}
				if ix1 >= w {
					ix1 = w - 1
				}

				p00 := row0 + ix0*4
				p10 := row0 + ix1*4
				p01 := row1 + ix0*4
				p11 := row1 + ix1*4

				dIdx := dstRow + x*4
				for c := 0; c < 4; c++ {
					top := float64(src.Pix[p00+c])*(1-fx) + float64(src.Pix[p10+c])*fx
					bot := float64(src.Pix[p01+c])*(1-fx) + float64(src.Pix[p11+c])*fx
					dst.Pix[dIdx+c] = uint8(math.Round(top*(1-fy) + bot*fy))
				}
			}
		}
	})

	return dst
}

// ApplyChromaticAberration separates the Red and Blue channels horizontally or radially in parallel
func ApplyChromaticAberration(src *image.RGBA, shift float64, mode string) *image.RGBA {
	if shift <= 0.5 {
		return src
	}

	w := src.Bounds().Dx()
	h := src.Bounds().Dy()
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)

	if mode == "radial" {
		centerX := float64(w) / 2.0
		centerY := float64(h) / 2.0
		maxDist := math.Hypot(centerX, centerY)

		parallelFor(h, func(startY, endY int) {
			for y := startY; y < endY; y++ {
				dy := (float64(y) - centerY) / maxDist
				dstRow := y * dst.Stride
				for x := 0; x < w; x++ {
					dx := (float64(x) - centerX) / maxDist

					rx := int(float64(x) + dx*shift)
					ry := int(float64(y) + dy*shift)
					if rx >= 0 && rx < w && ry >= 0 && ry < h {
						dst.Pix[dstRow+x*4+0] = src.Pix[ry*src.Stride+rx*4+0]
					}

					bx := int(float64(x) - dx*shift)
					by := int(float64(y) - dy*shift)
					if bx >= 0 && bx < w && by >= 0 && by < h {
						dst.Pix[dstRow+x*4+2] = src.Pix[by*src.Stride+bx*4+2]
					}
				}
			}
		})
	} else {
		s := int(math.Round(shift))
		parallelFor(h, func(startY, endY int) {
			for y := startY; y < endY; y++ {
				row := y * src.Stride
				for x := 0; x < w; x++ {
					rx := x - s
					if rx >= 0 && rx < w {
						dst.Pix[row+x*4+0] = src.Pix[row+rx*4+0]
					}
					bx := x + s
					if bx >= 0 && bx < w {
						dst.Pix[row+x*4+2] = src.Pix[row+bx*4+2]
					}
				}
			}
		})
	}

	return dst
}

// ApplyRipple applies a reactive sinusoidal water / wave distortion in parallel
func ApplyRipple(src *image.RGBA, amplitude float64, frequency float64, timeVal float64) *image.RGBA {
	if amplitude <= 0.2 {
		return src
	}

	w := src.Bounds().Dx()
	h := src.Bounds().Dy()
	dst := image.NewRGBA(src.Bounds())

	parallelFor(h, func(startY, endY int) {
		for y := startY; y < endY; y++ {
			offsetY := math.Cos(float64(y)*frequency+timeVal) * amplitude
			sy := int(float64(y) + offsetY)
			if sy < 0 {
				sy = 0
			} else if sy >= h {
				sy = h - 1
			}

			dstRow := y * dst.Stride
			srcRow := sy * src.Stride

			for x := 0; x < w; x++ {
				offsetX := math.Sin(float64(y)*frequency+timeVal) * amplitude
				sx := int(float64(x) + offsetX)
				if sx < 0 {
					sx = 0
				} else if sx >= w {
					sx = w - 1
				}

				sIdx := srcRow + sx*4
				dIdx := dstRow + x*4
				dst.Pix[dIdx+0] = src.Pix[sIdx+0]
				dst.Pix[dIdx+1] = src.Pix[sIdx+1]
				dst.Pix[dIdx+2] = src.Pix[sIdx+2]
				dst.Pix[dIdx+3] = src.Pix[sIdx+3]
			}
		}
	})

	return dst
}

// GlitchSlice is a horizontally shifted strip
type GlitchSlice struct {
	Y0, Y1, ShiftX int
}

// GlitchSlices generates the deterministic strips for a seed (shared CPU/GPU)
func GlitchSlices(w, h int, intensity float64, seed int64) []GlitchSlice {
	rng := rand.New(rand.NewSource(seed))
	numSlices := int(float64(h) * 0.04 * intensity)
	if numSlices < 2 {
		numSlices = 2
	}
	maxSliceH := int(float64(h) * 0.08)
	if maxSliceH < 4 {
		maxSliceH = 4
	}
	out := make([]GlitchSlice, numSlices)
	for i := range out {
		y := rng.Intn(h)
		sh := rng.Intn(maxSliceH) + 2
		shiftX := int((rng.Float64()*2.0 - 1.0) * float64(w) * 0.06 * intensity)
		out[i] = GlitchSlice{Y0: y, Y1: y + sh, ShiftX: shiftX}
	}
	return out
}

// ApplyGlitch applies strip displacement
func ApplyGlitch(src *image.RGBA, intensity float64, seed int64) *image.RGBA {
	if intensity <= 0.05 {
		return src
	}
	w := src.Bounds().Dx()
	h := src.Bounds().Dy()
	dst := image.NewRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	origRow := make([]byte, w*4)
	for _, sl := range GlitchSlices(w, h, intensity, seed) {
		for y := sl.Y0; y < sl.Y1 && y < h; y++ {
			row := y * dst.Stride
			copy(origRow, dst.Pix[row:row+w*4])
			for x := 0; x < w; x++ {
				srcX := x - sl.ShiftX
				if srcX >= 0 && srcX < w {
					copy(dst.Pix[row+x*4:row+x*4+3], origRow[srcX*4:srcX*4+3])
				}
			}
		}
	}
	return dst
}

// ParallelRange runs fn over [0, n) split among the CPUs
func ParallelRange(n int, fn func(a, b int)) {
	if n <= 0 {
		return
	}
	parallelFor(n, fn)
}
