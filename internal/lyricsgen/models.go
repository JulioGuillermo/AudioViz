package lyricsgen

import "audioviz/internal/aimodel"

// Model is a weights file downloaded once to the local cache
type Model = aimodel.Model

var (
	// Quantized Whisper large-v3-turbo: nearly the accuracy of large-v3, much faster
	WhisperModel = Model{Name: "whisper", Label: "Speech recognition (Whisper large-v3-turbo)", File: "ggml-large-v3-turbo-q5_0.bin",
		URL: "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3-turbo-q5_0.bin", Size: 574041195}
	DemucsModel = Model{Name: "demucs", Label: "Vocal separation (Demucs htdemucs)", File: "ggml-model-htdemucs-4s-f16.bin",
		URL: "https://huggingface.co/datasets/Retrobear/demucs.cpp/resolve/main/ggml-model-htdemucs-4s-f16.bin", Size: 83994361}
)

// aheadsLargeV3Turbo = WHISPER_AHEADS_LARGE_V3_TURBO in whisper.h
const aheadsLargeV3Turbo = 14

// CacheDir is the folder for models and caches (~/.cache/audioviz)
func CacheDir() string { return aimodel.CacheDir() }

func init() {
	aimodel.Register("Lyrics · speech recognition", WhisperModel)
	aimodel.Register("Lyrics and stems · voice separation", DemucsModel)
	aimodel.Register("Lyrics · alignment (Sylla)", SyllaModel, SyllaVocab)
}
