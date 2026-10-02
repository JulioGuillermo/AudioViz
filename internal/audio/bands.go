package audio

import "math"

// Band returns the value (0..1) of the given reactivity source:
// bass, mid, treble, overall, transient or beat. def is used if name is empty.
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
