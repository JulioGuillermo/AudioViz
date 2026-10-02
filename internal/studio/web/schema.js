// Esquema de la interfaz: cómo editar cada bloque de la config.
// Tipos: num, int, bool, enum, color, gradient, file, text, group, layers, optnum
// showIf(cfg) oculta campos que no aplican.

export const BANDS = [
  ['bass', 'Graves'], ['mid', 'Medios'], ['treble', 'Agudos'],
  ['overall', 'Volumen'], ['transient', 'Ataques'], ['beat', 'Golpes'],
];
const react = (def = 'bass') => ({ key: 'react_to', label: 'Reacciona a', type: 'enum', options: BANDS, def,
  help: 'Banda de frecuencias que controla el efecto' });

const glow = { key: 'glow', label: 'Glow', type: 'group', fields: [
  { key: 'enabled', label: 'Activo', type: 'bool' },
  { key: 'radius', label: 'Radio', type: 'num', min: 0, max: 40, step: 0.5 },
  { key: 'intensity', label: 'Intensidad', type: 'num', min: 0, max: 2, step: 0.05 },
] };

const ANIMS = [['fade', 'Fundido'], ['slide_up', 'Desliza arriba'], ['slide_down', 'Desliza abajo'],
  ['slide_left', 'Desliza izquierda'], ['slide_right', 'Desliza derecha'], ['slide_side', 'Desde su lado'],
  ['pop', 'Pop'], ['zoom', 'Zoom'], ['none', 'Ninguna']];

export const TRACKS = [
  { id: 'background', group: 'Fondo', label: 'Imagen / vídeo', icon: 'image', fields: [
    { key: 'path', label: 'Archivo', type: 'file', kind: 'bg', help: 'Imagen, GIF o vídeo (mp4, mov, mkv, webm)' },
    { key: 'fit_mode', label: 'Ajuste', type: 'enum', options: [['cover', 'Rellenar (recorta)'], ['contain', 'Contener (bandas)'], ['stretch', 'Estirar']] },
    { key: 'opacity', label: 'Opacidad', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'darken', label: 'Oscurecer', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'vignette', label: 'Viñeta', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'loop_video', label: 'Repetir vídeo/GIF', type: 'bool' },
  ] },
  { id: 'bass_zoom', group: 'Efectos del fondo', label: 'Zoom', icon: 'zoom-in', fields: [
    { key: 'enabled', label: 'Activo', type: 'bool' },
    { key: 'max_zoom', label: 'Zoom máximo', type: 'num', min: 1, max: 1.4, step: 0.005 },
    { key: 'sensitivity', label: 'Sensibilidad', type: 'num', min: 0, max: 3, step: 0.05 },
    react('bass'),
  ] },
  { id: 'shake', group: 'Efectos del fondo', label: 'Temblor / movimiento', icon: 'move', fields: [
    { key: 'enabled', label: 'Activo', type: 'bool' },
    { key: 'amplitude', label: 'Amplitud (px)', type: 'num', min: 0, max: 60, step: 0.5 },
    { key: 'speed', label: 'Velocidad', type: 'num', min: 0.1, max: 5, step: 0.05 },
    react('bass'),
  ] },
  { id: 'chromatic_aberration', group: 'Efectos del fondo', label: 'Aberración cromática', icon: 'blend', fields: [
    { key: 'enabled', label: 'Activo', type: 'bool' },
    { key: 'max_shift', label: 'Desplazamiento (px)', type: 'num', min: 0, max: 40, step: 0.5 },
    { key: 'mode', label: 'Modo', type: 'enum', options: [['radial', 'Radial'], ['horizontal', 'Horizontal']] },
    react('bass'),
  ] },
  { id: 'ripple', group: 'Efectos del fondo', label: 'Ondulación', icon: 'waves', fields: [
    { key: 'enabled', label: 'Activo', type: 'bool' },
    { key: 'amplitude', label: 'Amplitud (px)', type: 'num', min: 0, max: 30, step: 0.5 },
    { key: 'frequency', label: 'Frecuencia', type: 'num', min: 0.002, max: 0.2, step: 0.001 },
    { key: 'speed', label: 'Velocidad', type: 'num', min: 0, max: 6, step: 0.05 },
    react('mid'),
  ] },
  { id: 'glitch', group: 'Efectos del fondo', label: 'Glitch', icon: 'scan-line', fields: [
    { key: 'enabled', label: 'Activo', type: 'bool' },
    { key: 'intensity', label: 'Intensidad', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'threshold', label: 'Umbral', type: 'num', min: 0, max: 1, step: 0.01, help: 'Por encima de este valor se dispara' },
    { key: 'react_to', label: 'Se dispara con', type: 'enum', options: [['', 'Golpes / ataques'], ...BANDS] },
  ] },
  { id: 'color_flash', group: 'Efectos del fondo', label: 'Flash de color', icon: 'sun', fields: [
    { key: 'enabled', label: 'Activo', type: 'bool' },
    { key: 'brightness_boost', label: 'Brillo extra', type: 'num', min: 0, max: 0.8, step: 0.01 },
    { key: 'contrast_boost', label: 'Contraste extra', type: 'num', min: 0, max: 0.8, step: 0.01 },
    react('bass'),
  ] },
  { id: 'visualizer', group: 'Visualizador', label: 'Espectro', icon: 'chart-column', fields: [
    { key: 'enabled', label: 'Activo', type: 'bool' },
    { key: 'type', label: 'Tipo', type: 'enum', options: [['circular', 'Circular'], ['horizontal', 'Horizontal'], ['vertical', 'Vertical (bordes)'], ['oscilloscope', 'Osciloscopio']] },
    { key: 'bars', label: 'Barras por canal', type: 'int', min: 4, max: 256, step: 1 },
    { key: 'min_height', label: 'Altura mínima', type: 'num', min: 0, max: 100, step: 0.5 },
    { key: 'max_height', label: 'Altura máxima', type: 'num', min: 10, max: 1200, step: 1 },
    { key: 'bar_width', label: 'Grosor', type: 'num', min: 0.5, max: 60, step: 0.5 },
    { key: 'bar_gap', label: 'Separación', type: 'num', min: 0, max: 40, step: 0.5, showIf: c => c.type === 'horizontal' },
    { key: 'center_gap', label: 'Hueco central (px)', type: 'optnum', min: 0, max: 200, step: 1, showIf: c => c.type === 'horizontal', help: 'Vacío = 2 × separación' },
    { key: 'roundness', label: 'Redondeo', type: 'num', min: 0, max: 30, step: 0.5, showIf: c => c.type === 'horizontal' || c.type === 'vertical' },
    { key: 'radius', label: 'Radio', type: 'num', min: 0, max: 1500, step: 1, showIf: c => c.type === 'circular' },
    { key: 'center_x', label: 'Centro X', type: 'num', min: 0, max: 1, step: 0.005, showIf: c => c.type === 'circular' },
    { key: 'center_y', label: 'Centro Y', type: 'num', min: 0, max: 1, step: 0.005, showIf: c => c.type === 'circular' },
    { key: 'position_y', label: 'Altura (0 arriba, 1 abajo)', type: 'num', min: 0, max: 1, step: 0.005, showIf: c => c.type === 'horizontal' || c.type === 'oscilloscope' },
    { key: 'mirror_bottom', label: 'Reflejo', type: 'bool', showIf: c => c.type === 'horizontal' },
    { key: 'smoothing', label: 'Velocidad de subida', type: 'num', min: 0.05, max: 0.98, step: 0.01 },
    { key: 'decay_rate', label: 'Velocidad de bajada', type: 'num', min: 0.01, max: 0.5, step: 0.005 },
    { key: 'color_mode', label: 'Color', type: 'enum', options: [['gradient', 'Gradiente'], ['solid', 'Sólido']] },
    { key: 'solid_color', label: 'Color sólido', type: 'color', showIf: c => c.color_mode === 'solid' },
    { key: 'gradient', label: 'Gradiente', type: 'gradient', showIf: c => c.color_mode !== 'solid' },
    { key: 'gradient_axis', label: 'Dirección', type: 'enum', options: [['index', 'Por barra (a lo ancho)'], ['length', 'A lo largo de cada barra']], showIf: c => c.color_mode !== 'solid' },
    { key: 'gradient_fit', label: 'Referencia', type: 'enum', options: [['bar', 'Se mueve con la barra'], ['max', 'Respecto al máximo']], showIf: c => c.color_mode !== 'solid' && c.gradient_axis === 'length' },
    glow,
  ] },
  { id: 'particles', group: 'Partículas', label: 'Partículas', icon: 'sparkles', fields: [
    { key: 'enabled', label: 'Activas', type: 'bool' },
    { key: 'layers', label: 'Capas', type: 'layers', fields: [
      { key: 'type', label: 'Tipo', type: 'enum', options: [['rising_sparks', 'Chispas (dirección)'], ['radial_burst', 'Explosión radial'], ['ambient_dust', 'Polvo ambiental']] },
      react('bass'),
      { key: 'count', label: 'Cantidad máxima', type: 'int', min: 1, max: 1024, step: 1 },
      { key: 'min_density', label: 'Densidad en silencio', type: 'optnum', min: 0, max: 1, step: 0.01, help: 'Vacío = siempre todas; 0.1 = casi ninguna si la banda calla' },
      { key: 'direction', label: 'Dirección (°)', type: 'optnum', min: 0, max: 360, step: 1, help: '0 derecha, 90 arriba, 180 izquierda, 270 abajo. Vacío = hacia arriba' },
      { key: 'spread', label: 'Apertura (°)', type: 'num', min: 0, max: 180, step: 1 },
      { key: 'min_size', label: 'Tamaño mín.', type: 'num', min: 0.2, max: 30, step: 0.1 },
      { key: 'max_size', label: 'Tamaño máx.', type: 'num', min: 0.2, max: 40, step: 0.1 },
      { key: 'color', label: 'Color al nacer', type: 'color' },
      { key: 'color_end', label: 'Color al morir', type: 'color', optional: true },
      { key: 'speed', label: 'Velocidad', type: 'num', min: 0, max: 10, step: 0.05 },
      { key: 'audio_boost', label: 'Aceleración con audio', type: 'num', min: 0, max: 10, step: 0.1 },
      { key: 'turbulence', label: 'Turbulencia', type: 'num', min: 0, max: 3, step: 0.05 },
      { key: 'min_life', label: 'Vida mín. (s)', type: 'num', min: 0.1, max: 15, step: 0.1 },
      { key: 'max_life', label: 'Vida máx. (s)', type: 'num', min: 0.1, max: 20, step: 0.1 },
      { key: 'glow', label: 'Halo', type: 'bool' },
    ] },
  ] },
  { id: 'lyrics', group: 'Letra', label: 'Letra', icon: 'mic-vocal', fields: [
    { key: 'enabled', label: 'Visible', type: 'bool' },
    { key: 'path', label: 'Archivo .lrc / .srt', type: 'file', kind: 'lyrics', firstOnly: true },
    { key: 'offset', label: 'Desfase (s)', type: 'num', min: -5, max: 5, step: 0.01, firstOnly: true },
    { key: 'font', label: 'Fuente', type: 'file', kind: 'font', help: 'Vacío = fuente integrada' },
    { key: 'size', label: 'Tamaño', type: 'num', min: 8, max: 300, step: 1 },
    { key: 'uppercase', label: 'Mayúsculas', type: 'bool' },
    { key: 'position', label: 'Posición', type: 'enum', options: [['bottom', 'Abajo'], ['top', 'Arriba'], ['center', 'Centro'], ['left', 'Izquierda'], ['right', 'Derecha'], ['side', 'Alterna izq./der.'], ['custom', 'Personalizada']] },
    { key: 'margin', label: 'Margen (px)', type: 'num', min: 0, max: 1000, step: 1 },
    { key: 'position_x', label: 'Posición X', type: 'num', min: 0, max: 1, step: 0.005, showIf: c => c.position === 'custom' },
    { key: 'position_y', label: 'Posición Y', type: 'num', min: 0, max: 1, step: 0.005, showIf: c => ['custom', 'left', 'right', 'side'].includes(c.position) },
    { key: 'max_width', label: 'Ancho máximo', type: 'num', min: 0.1, max: 1, step: 0.01 },
    { key: 'line_spacing', label: 'Interlineado', type: 'num', min: 0.5, max: 3, step: 0.05 },
    { key: 'word_spacing', label: 'Espacio entre palabras', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'align', label: 'Alineación', type: 'enum', options: [['center', 'Centro'], ['left', 'Izquierda'], ['right', 'Derecha']], showIf: c => ['top', 'bottom', 'center', 'custom'].includes(c.position) },
    { key: 'mode', label: 'Modo', type: 'enum', options: [['karaoke', 'Karaoke (se rellena)'], ['words', 'Palabra a palabra'], ['line', 'Línea completa']] },
    { key: 'word_pop', label: 'Latido de palabra', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'in_animation', label: 'Animación de entrada', type: 'enum', options: ANIMS },
    { key: 'out_animation', label: 'Animación de salida', type: 'enum', options: ANIMS },
    { key: 'in_duration', label: 'Duración entrada (s)', type: 'num', min: 0.01, max: 3, step: 0.01 },
    { key: 'out_duration', label: 'Duración salida (s)', type: 'num', min: 0.01, max: 3, step: 0.01 },
    { key: 'slide_distance', label: 'Distancia deslizamiento', type: 'num', min: 0, max: 600, step: 1 },
    { key: 'color', label: 'Color', type: 'color' },
    { key: 'gradient', label: 'Gradiente vertical', type: 'gradient', optional: true, help: 'Si tiene colores, sustituye al color' },
    { key: 'upcoming_color', label: 'Color pendiente (karaoke)', type: 'color' },
    { key: 'outline_color', label: 'Contorno', type: 'color' },
    { key: 'outline_width', label: 'Grosor contorno', type: 'num', min: 0, max: 15, step: 0.25 },
    { key: 'glow', label: 'Glow', type: 'group', fields: [
      { key: 'enabled', label: 'Activo', type: 'bool' },
      { key: 'color', label: 'Color', type: 'color', optional: true },
      { key: 'radius', label: 'Radio', type: 'num', min: 0, max: 24, step: 0.5 },
      { key: 'intensity', label: 'Intensidad', type: 'num', min: 0, max: 2, step: 0.05 },
    ] },
    { key: 'shadow', label: 'Sombra', type: 'group', fields: [
      { key: 'enabled', label: 'Activa', type: 'bool' },
      { key: 'color', label: 'Color', type: 'color' },
      { key: 'offset_x', label: 'Desplazamiento X', type: 'num', min: -40, max: 40, step: 0.5 },
      { key: 'offset_y', label: 'Desplazamiento Y', type: 'num', min: -40, max: 40, step: 0.5 },
      { key: 'opacity', label: 'Opacidad', type: 'num', min: 0, max: 1, step: 0.01 },
      { key: 'blur', label: 'Difuminado', type: 'num', min: 0, max: 20, step: 0.5 },
    ] },
    { key: 'react', label: 'Reacción al audio', type: 'group', fields: [
      react('bass'),
      { key: 'scale', label: 'Latido (escala)', type: 'num', min: 0, max: 0.5, step: 0.005 },
      { key: 'glow', label: 'Glow extra', type: 'num', min: 0, max: 2, step: 0.05 },
      { key: 'shake', label: 'Temblor (px)', type: 'num', min: 0, max: 30, step: 0.5 },
      { key: 'chromatic', label: 'Aberración (px)', type: 'num', min: 0, max: 20, step: 0.25 },
      { key: 'flicker', label: 'Parpadeo de llama', type: 'num', min: 0, max: 1, step: 0.01 },
      { key: 'flash', label: 'Flash de color', type: 'num', min: 0, max: 1, step: 0.01 },
      { key: 'flash_color', label: 'Color del flash', type: 'color', optional: true },
    ] },
  ] },
];

export const AUDIO_FIELDS = [
  { key: 'path', label: 'Archivo de audio', type: 'file', kind: 'audio' },
  { key: 'volume', label: 'Volumen de análisis', type: 'num', min: 0.1, max: 4, step: 0.05 },
  { key: 'sensitivity', label: 'Sensibilidad de barras', type: 'num', min: 0.1, max: 4, step: 0.05 },
  { key: 'normalize', label: 'Normalización adaptativa', type: 'bool', help: 'Cada efecto y barra se reescala a su rango local' },
  { key: 'normalize_window', label: 'Ventana (s)', type: 'num', min: 0.5, max: 20, step: 0.5 },
];

export const OUTPUT_FIELDS = [
  { key: 'path', label: 'Archivo de salida', type: 'text' },
  { key: 'width', label: 'Ancho (0 = el del fondo)', type: 'int', min: 0, max: 7680, step: 2 },
  { key: 'height', label: 'Alto (0 = el del fondo)', type: 'int', min: 0, max: 7680, step: 2 },
  { key: 'fps', label: 'FPS', type: 'enum', options: [[24, '24'], [25, '25'], [30, '30'], [50, '50'], [60, '60']] },
  { key: 'renderer', label: 'Render', type: 'enum', options: [['auto', 'Auto (GPU si hay)'], ['gpu', 'GPU'], ['cpu', 'CPU']] },
  { key: 'preset', label: 'Preset x264', type: 'enum', options: ['ultrafast', 'superfast', 'veryfast', 'faster', 'fast', 'medium', 'slow', 'slower', 'veryslow'].map(x => [x, x]) },
  { key: 'crf', label: 'Calidad (CRF, menor = mejor)', type: 'int', min: 10, max: 35, step: 1 },
];

export const trackById = Object.fromEntries(TRACKS.map(t => [t.id, t]));
