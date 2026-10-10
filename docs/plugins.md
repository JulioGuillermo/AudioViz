# Plugins

Plugins add engines and abilities to AudioViz without changing the program: each one is a `.zip`
installed from *Project settings → Plugins → Install a plugin…*. AudioViz unpacks it into its own
folder (`~/.cache/audioviz/plugins/<id>/`, next to the downloaded models), so it works for every
project. A plugin is built for one system and one version of AudioViz; one that doesn't match is
refused with the reason.

## Intel GPU acceleration (SYCL)

On Intel GPUs (Arc, Lunar Lake / Xe2 and later), image generation runs on Intel's own runtime
(SYCL, oneDNN, oneMKL) instead of Vulkan: about 1.7× faster (Z-Image Turbo 1024×1024, 8 steps:
~94 s instead of ~129 s on Vulkan). The first image after AudioViz starts takes ~10 s more (the
GPU's kernels are compiled).

- **Image engine**: *Automatic* (the plugin when it is installed and loads, else Vulkan),
  *Vulkan* (built in) or *Intel GPU (SYCL plugin)*. Changing it loads the models again on their
  next use.
- **Building it** (Linux, with the oneAPI Base Toolkit): `scripts/build-sycl-plugin.sh` after
  `./build.sh`; the result is `third_party/obj-linux-amd64/sycl-plugin/audioviz-intel-sycl-linux-amd64.zip`
  (~130 MB; ~470 MB installed: the module and the oneAPI runtime libraries it needs).
- Installing a new version while the old one is in use takes effect when AudioViz restarts;
  the same for removing it.
