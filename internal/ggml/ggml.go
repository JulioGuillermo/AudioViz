// Package ggml holds what the ggml-based libraries (whisper.cpp for the lyrics, stable-diffusion.cpp
// for the images) share at link time: the Vulkan loader bridge (vkshim.c). Both are built against a
// single ggml (scripts/ml). Import it for its side effect.
package ggml

/*
#cgo windows CFLAGS: -I${SRCDIR}/../../third_party/vulkan-headers
*/
import "C"

import "os"

func init() {
	// ggml-vulkan sends up to 100 operations to the GPU at once. With large images one such
	// submission can take longer than the driver allows a compute job (5 s on Intel's xe driver),
	// which then resets the GPU engine and the generation hangs. Small submissions cost nothing
	// measurable (26.6 vs 26.7 s on a Z-Image test) and stay far below the limit.
	if os.Getenv("GGML_VK_MAX_NODES_PER_SUBMIT") == "" {
		os.Setenv("GGML_VK_MAX_NODES_PER_SUBMIT", "8")
	}
}
