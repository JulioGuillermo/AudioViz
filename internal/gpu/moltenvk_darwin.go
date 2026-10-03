//go:build darwin && moltenvk

// macOS: MoltenVK (Vulkan on top of Metal) linked statically, so the same compute shader runs on
// Apple GPUs with nothing installed. scripts/build-moltenvk.sh puts libMoltenVK.a and the Vulkan
// headers in third_party/build-darwin-<arch>; build.sh then adds the "moltenvk" tag. Without the
// tag the renderer looks for a MoltenVK / Vulkan SDK dylib at runtime and otherwise uses the CPU.
package gpu

/*
#cgo CFLAGS: -DAUDIOVIZ_MOLTENVK_STATIC
#cgo arm64 LDFLAGS: ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libMoltenVK.a
#cgo amd64 LDFLAGS: ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libMoltenVK.a
#cgo LDFLAGS: -framework Metal -framework IOSurface -framework QuartzCore -framework CoreGraphics -framework IOKit -framework Foundation -framework AppKit -lc++
*/
import "C"
