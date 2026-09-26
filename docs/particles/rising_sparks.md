# rising_sparks

Nacen en el borde inferior repartidas a lo ancho y suben con algo de deriva lateral. Con `turbulence`
y `color_end` imitan chispas de fuego.

Ejemplo de fuego por bandas (graves = brasas rojas grandes, medios = chispas naranjas, agudos = chispitas amarillas):
```yaml
particles:
  enabled: true
  layers:
    - { type: rising_sparks, react_to: bass,   count: 90,  min_density: 0.15, min_size: 1.8, max_size: 4.0,
        color: "#ff3a0a", color_end: "#6e0a00", speed: 0.9, audio_boost: 1.5, turbulence: 0.6,
        min_life: 1.5, max_life: 3.5, glow: true }
    - { type: rising_sparks, react_to: mid,    count: 110, min_density: 0.15, min_size: 1.5, max_size: 4.0,
        color: "#ff8c1a", color_end: "#b82a00", speed: 1.3, audio_boost: 1.5, turbulence: 0.8,
        min_life: 1.2, max_life: 3.0, glow: true }
    - { type: rising_sparks, react_to: treble, count: 140, min_density: 0.1,  min_size: 0.8, max_size: 2.2,
        color: "#fff0a0", color_end: "#ff9500", speed: 1.8, audio_boost: 2.0, turbulence: 1.0,
        min_life: 0.8, max_life: 2.2, glow: true }
```
Opciones: ver [capas](README.md#opciones-de-cada-capa). `speed` controla la velocidad de subida.
