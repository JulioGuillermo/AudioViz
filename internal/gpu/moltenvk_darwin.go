//go:build darwin && !nomoltenvk

// macOS: MoltenVK (Vulkan on top of Metal) linked statically, so the same compute shader runs on
// Apple GPUs with nothing installed. scripts/build-moltenvk.sh puts libMoltenVK.a and the Vulkan
// headers in third_party/build-darwin-<arch>. It is linked by default (so a plain
// "go install ./cmd/audioviz" works); build.sh adds the "nomoltenvk" tag only when the library is
// missing, and then the renderer looks for a MoltenVK / Vulkan SDK dylib at runtime or uses the CPU.
package gpu

/*
#cgo CFLAGS: -DAUDIOVIZ_MOLTENVK_STATIC
#cgo arm64 LDFLAGS: ${SRCDIR}/../../third_party/build-darwin-arm64/lib/libMoltenVK.a
#cgo amd64 LDFLAGS: ${SRCDIR}/../../third_party/build-darwin-amd64/lib/libMoltenVK.a
#cgo LDFLAGS: -framework Metal -framework IOSurface -framework QuartzCore -framework CoreGraphics -framework IOKit -framework Foundation -framework AppKit -lc++
*/
import "C"
