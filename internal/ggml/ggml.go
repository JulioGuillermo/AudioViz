// Package ggml holds what the ggml-based libraries (whisper.cpp for the lyrics, stable-diffusion.cpp
// for the images) share at link time: the Vulkan loader bridge (vkshim.c). Both are built against a
// single ggml (scripts/ml). Import it for its side effect.
package ggml

/*
#cgo windows CFLAGS: -I${SRCDIR}/../../third_party/vulkan-headers
*/
import "C"
