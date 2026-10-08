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
	case "beat":
		return f.BeatPulse
	// the separated song (StemFrame); before it is separated, the closest from the mix
	case "drums":
		if f.Stem != nil {
			return f.Stem.Drums
		}
		return math.Min(f.SpectralFlux*5.0, 1.5)
	case "kick":
		if f.Stem != nil {
			return f.Stem.Kick
		}
		return f.BeatPulse
	case "snare":
		if f.Stem != nil {
			return f.Stem.Snare
		}
		return math.Min(f.SpectralFlux*5.0, 1.5)
	case "hats", "hihat":
		if f.Stem != nil {
			return f.Stem.Hats
		}
		return f.TrebleEnergy
	case "bassline":
		if f.Stem != nil {
			return f.Stem.Bass
		}
		return f.BassEnergy
	case "vocals", "voice":
		if f.Stem != nil {
			return f.Stem.Vocals
		}
		return f.MidEnergy
	case "instruments", "other":
		if f.Stem != nil {
			return f.Stem.Other
		}
		return f.OverallRMS
	default:
		return f.BassEnergy
	}
}
