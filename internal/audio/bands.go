package audio

import "math"

// Band devuelve el valor (0..1) de la fuente de reactividad indicada:
// bass, mid, treble, overall, transient o beat. def se usa si name está vacío.
func (f *FrameFeatures) Band(name, def string) float64 {
	if name == "" {
		name = def
	}
	switch name {
	case "mid", "mids", "medios":
		return f.MidEnergy
	case "treble", "high", "highs", "agudos":
		return f.TrebleEnergy
	case "overall", "rms", "volume":
		return f.OverallRMS
	case "transient", "flux":
		return math.Min(f.SpectralFlux*5.0, 1.5)
	case "beat", "kick":
		return f.BeatPulse
	default:
		return f.BassEnergy
	}
}
