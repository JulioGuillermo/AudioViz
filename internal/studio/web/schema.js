// UI schema: how each config block is edited. Labels are English and translated at render time.
// Types: num, int, bool, enum, color, gradient, file, text, group, layers, optnum
// showIf(cfg) hides fields that do not apply. def is the value shown when the config leaves it empty.

export const BANDS = [
  ['bass', 'Bass'], ['mid', 'Mids'], ['treble', 'Treble'],
  ['overall', 'Volume'], ['transient', 'Transients'], ['beat', 'Beats'],
];
const react = (def = 'bass') => ({ key: 'react_to', label: 'Reacts to', type: 'enum', options: BANDS, def,
  help: 'Frequency band that drives the effect' });

const glow = { key: 'glow', label: 'Glow', type: 'group', fields: [
  { key: 'enabled', label: 'Enabled', type: 'bool' },
  { key: 'radius', label: 'Radius', type: 'num', min: 0, max: 40, step: 0.5 },
  { key: 'intensity', label: 'Intensity', type: 'num', min: 0, max: 2, step: 0.05 },
] };

const ANIMS = [['fade', 'Fade'], ['slide_up', 'Slide up'], ['slide_down', 'Slide down'],
  ['slide_left', 'Slide left'], ['slide_right', 'Slide right'], ['slide_side', 'From its side'],
  ['pop', 'Pop'], ['zoom', 'Zoom'], ['none', 'No animation']];

export const TRACKS = [
  { id: 'background', group: 'Background', label: 'Image / video', icon: 'image', fields: [
    { key: 'path', label: 'File', type: 'file', kind: 'bg', help: 'Image, GIF or video (mp4, mov, mkv, webm)' },
    { key: 'fit_mode', label: 'Fit', type: 'enum', def: 'cover', options: [['cover', 'Cover (crops)'], ['contain', 'Contain (bars)'], ['stretch', 'Stretch']] },
    { key: 'opacity', label: 'Opacity', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'darken', label: 'Darken', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'vignette', label: 'Vignette', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'loop_video', label: 'Loop video / GIF', type: 'bool' },
    { key: 'motion', label: 'Motion', type: 'group', fields: [
      { key: 'type', label: 'Type', type: 'enum', def: 'none', help: 'Slow camera motion over the image during this segment',
        options: [['none', 'Still'], ['zoom_in', 'Slow zoom in'], ['zoom_out', 'Slow zoom out'], ['pan_left', 'Pan left'],
          ['pan_right', 'Pan right'], ['pan_up', 'Pan up'], ['pan_down', 'Pan down'], ['ken_burns', 'Ken Burns (zoom + pan)'],
          ['drift', 'Drift (floating)'], ['sway', 'Sway (rotation)']] },
      { key: 'amount', label: 'Amount', type: 'num', min: 0.02, max: 0.6, step: 0.01, showIf: (c) => (c.type || 'none') !== 'none' },
      { key: 'easing', label: 'Easing', type: 'enum', def: 'linear', options: [['linear', 'Constant speed'], ['ease', 'Soft start and end']],
        showIf: (c) => !['none', 'drift', 'sway'].includes(c.type || 'none') },
      { key: 'speed', label: 'Speed', type: 'num', min: 0.1, max: 5, step: 0.1, help: 'Cycles every 10 s', showIf: (c) => ['drift', 'sway'].includes(c.type) },
    ] },
  ] },
  { id: 'bass_zoom', group: 'Background effects', label: 'Zoom', icon: 'zoom-in', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool' },
    { key: 'max_zoom', label: 'Max zoom', type: 'num', min: 1, max: 1.4, step: 0.005 },
    { key: 'sensitivity', label: 'Sensitivity', type: 'num', min: 0, max: 3, step: 0.05 },
    react('bass'),
  ] },
  { id: 'shake', group: 'Background effects', label: 'Camera shake', icon: 'move', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool' },
    { key: 'amplitude', label: 'Amplitude (px)', type: 'num', min: 0, max: 60, step: 0.5 },
    { key: 'speed', label: 'Speed', type: 'num', min: 0.1, max: 5, step: 0.05 },
    react('bass'),
  ] },
  { id: 'chromatic_aberration', group: 'Background effects', label: 'Chromatic aberration', icon: 'blend', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool' },
    { key: 'max_shift', label: 'Shift (px)', type: 'num', min: 0, max: 40, step: 0.5 },
    { key: 'mode', label: 'Mode', type: 'enum', def: 'radial', options: [['radial', 'Radial'], ['horizontal', 'Horizontal']] },
    react('bass'),
  ] },
  { id: 'ripple', group: 'Background effects', label: 'Ripple', icon: 'waves', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool' },
    { key: 'amplitude', label: 'Amplitude (px)', type: 'num', min: 0, max: 30, step: 0.5 },
    { key: 'frequency', label: 'Frequency', type: 'num', min: 0.002, max: 0.2, step: 0.001 },
    { key: 'speed', label: 'Speed', type: 'num', min: 0, max: 6, step: 0.05 },
    react('mid'),
  ] },
  { id: 'glitch', group: 'Background effects', label: 'Glitch', icon: 'scan-line', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool' },
    { key: 'intensity', label: 'Intensity', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'threshold', label: 'Threshold', type: 'num', min: 0, max: 1, step: 0.01, help: 'Triggers above this value' },
    { key: 'react_to', label: 'Triggered by', type: 'enum', def: '', options: [['', 'Beats / transients'], ...BANDS] },
  ] },
  { id: 'color_flash', group: 'Background effects', label: 'Color flash', icon: 'sun', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool' },
    { key: 'brightness_boost', label: 'Extra brightness', type: 'num', min: 0, max: 0.8, step: 0.01 },
    { key: 'contrast_boost', label: 'Extra contrast', type: 'num', min: 0, max: 0.8, step: 0.01 },
    react('bass'),
  ] },
  { id: 'visualizer', group: 'Visualizer', label: 'Spectrum', icon: 'chart-column', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool' },
    { key: 'type', label: 'Type', type: 'enum', def: 'circular', options: [['circular', 'Circular'], ['horizontal', 'Horizontal'], ['vertical', 'Vertical (edges)'], ['oscilloscope', 'Oscilloscope']] },
    { key: 'bars', label: 'Bars per channel', type: 'int', min: 4, max: 256, step: 1 },
    { key: 'min_height', label: 'Min height', type: 'num', min: 0, max: 100, step: 0.5 },
    { key: 'max_height', label: 'Max height', type: 'num', min: 10, max: 1200, step: 1 },
    { key: 'bar_width', label: 'Thickness', type: 'num', min: 0.5, max: 60, step: 0.5 },
    { key: 'bar_gap', label: 'Gap', type: 'num', min: 0, max: 40, step: 0.5, showIf: (c) => c.type === 'horizontal' },
    { key: 'center_gap', label: 'Center gap (px)', type: 'optnum', min: 0, max: 200, step: 1, showIf: (c) => c.type === 'horizontal', help: 'Off = 2 × gap' },
    { key: 'roundness', label: 'Roundness', type: 'num', min: 0, max: 30, step: 0.5, showIf: (c) => c.type === 'horizontal' || c.type === 'vertical' },
    { key: 'radius', label: 'Radius', type: 'num', min: 0, max: 1500, step: 1, showIf: (c) => c.type === 'circular' },
    { key: 'center_x', label: 'Center X', type: 'num', min: 0, max: 1, step: 0.005, showIf: (c) => c.type === 'circular' },
    { key: 'center_y', label: 'Center Y', type: 'num', min: 0, max: 1, step: 0.005, showIf: (c) => c.type === 'circular' },
    { key: 'position_y', label: 'Height (0 top, 1 bottom)', type: 'num', min: 0, max: 1, step: 0.005, showIf: (c) => c.type === 'horizontal' || c.type === 'oscilloscope' },
    { key: 'mirror_bottom', label: 'Reflection', type: 'bool', showIf: (c) => c.type === 'horizontal' },
    { key: 'smoothing', label: 'Rise speed', type: 'num', min: 0.05, max: 0.98, step: 0.01 },
    { key: 'decay_rate', label: 'Fall speed', type: 'num', min: 0.01, max: 0.5, step: 0.005 },
    { key: 'color_mode', label: 'Color', type: 'enum', def: 'gradient', options: [['gradient', 'Gradient'], ['solid', 'Solid']] },
    { key: 'solid_color', label: 'Solid color', type: 'color', showIf: (c) => c.color_mode === 'solid' },
    { key: 'gradient', label: 'Gradient', type: 'gradient', showIf: (c) => c.color_mode !== 'solid' },
    { key: 'gradient_axis', label: 'Direction', type: 'enum', def: 'index', options: [['index', 'Per bar (across)'], ['length', 'Along each bar']], showIf: (c) => c.color_mode !== 'solid' },
    { key: 'gradient_fit', label: 'Reference', type: 'enum', def: 'bar', options: [['bar', 'Moves with the bar'], ['max', 'Relative to max height']], showIf: (c) => c.color_mode !== 'solid' && c.gradient_axis === 'length' },
    glow,
  ] },
  { id: 'particles', group: 'Particles', label: 'Particles', icon: 'sparkles', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool' },
    { key: 'layers', label: 'Layers', type: 'layers', fields: [
      { key: 'type', label: 'Type', type: 'enum', def: 'radial_burst', options: [['rising_sparks', 'Sparks (directional)'], ['radial_burst', 'Radial burst'], ['ambient_dust', 'Ambient dust']] },
      react('bass'),
      { key: 'count', label: 'Max count', type: 'int', min: 1, max: 1024, step: 1 },
      { key: 'min_density', label: 'Density in silence', type: 'optnum', min: 0, max: 1, step: 0.01, help: 'Off = always all of them; 0.1 = almost none when the band is quiet' },
      { key: 'direction', label: 'Direction (°)', type: 'optnum', min: 0, max: 360, step: 1, help: '0 right, 90 up, 180 left, 270 down. Off = upwards' },
      { key: 'spread', label: 'Spread (°)', type: 'num', min: 0, max: 180, step: 1 },
      { key: 'min_size', label: 'Min size', type: 'num', min: 0.2, max: 30, step: 0.1 },
      { key: 'max_size', label: 'Max size', type: 'num', min: 0.2, max: 40, step: 0.1 },
      { key: 'color', label: 'Birth color', type: 'color' },
      { key: 'color_end', label: 'Death color', type: 'color', optional: true },
      { key: 'speed', label: 'Speed', type: 'num', min: 0, max: 10, step: 0.05 },
      { key: 'audio_boost', label: 'Audio acceleration', type: 'num', min: 0, max: 10, step: 0.1 },
      { key: 'turbulence', label: 'Turbulence', type: 'num', min: 0, max: 3, step: 0.05 },
      { key: 'min_life', label: 'Min life (s)', type: 'num', min: 0.1, max: 15, step: 0.1 },
      { key: 'max_life', label: 'Max life (s)', type: 'num', min: 0.1, max: 20, step: 0.1 },
      { key: 'glow', label: 'Halo', type: 'bool' },
    ] },
  ] },
  { id: 'lyrics', group: 'Lyrics', label: 'Lyrics', icon: 'mic-vocal', fields: [
    { key: 'enabled', label: 'Visible', type: 'bool' },
    { key: 'path', label: '.lrc / .srt file', type: 'file', kind: 'lyrics', firstOnly: true },
    { key: 'offset', label: 'Offset (s)', type: 'num', min: -5, max: 5, step: 0.01, firstOnly: true },
    { key: 'font', label: 'Font', type: 'file', kind: 'font', help: 'Empty = built-in font' },
    { key: 'size', label: 'Size', type: 'num', min: 8, max: 300, step: 1 },
    { key: 'uppercase', label: 'Uppercase', type: 'bool' },
    { key: 'position', label: 'Position', type: 'enum', def: 'bottom', options: [['bottom', 'Bottom'], ['top', 'Top'], ['center', 'Center'], ['left', 'Left'], ['right', 'Right'], ['side', 'Alternate left/right'], ['custom', 'Custom']] },
    { key: 'margin', label: 'Margin (px)', type: 'num', min: 0, max: 1000, step: 1 },
    { key: 'position_x', label: 'Position X', type: 'num', min: 0, max: 1, step: 0.005, showIf: (c) => c.position === 'custom' },
    { key: 'position_y', label: 'Position Y', type: 'num', min: 0, max: 1, step: 0.005, showIf: (c) => ['custom', 'left', 'right', 'side'].includes(c.position) },
    { key: 'max_width', label: 'Max width', type: 'num', min: 0.1, max: 1, step: 0.01 },
    { key: 'line_spacing', label: 'Line spacing', type: 'num', min: 0.5, max: 3, step: 0.05 },
    { key: 'word_spacing', label: 'Word spacing', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'align', label: 'Alignment', type: 'enum', def: 'center', options: [['center', 'Center'], ['left', 'Left'], ['right', 'Right']], showIf: (c) => ['top', 'bottom', 'center', 'custom'].includes(c.position) },
    { key: 'mode', label: 'Mode', type: 'enum', def: 'karaoke', options: [['karaoke', 'Karaoke (fills in)'], ['words', 'Word by word'], ['line', 'Whole line']] },
    { key: 'word_pop', label: 'Word pop', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'in_animation', label: 'In animation', type: 'enum', def: 'fade', options: ANIMS },
    { key: 'out_animation', label: 'Out animation', type: 'enum', def: 'fade', options: ANIMS },
    { key: 'in_duration', label: 'In duration (s)', type: 'num', min: 0.01, max: 3, step: 0.01 },
    { key: 'out_duration', label: 'Out duration (s)', type: 'num', min: 0.01, max: 3, step: 0.01 },
    { key: 'slide_distance', label: 'Slide distance', type: 'num', min: 0, max: 600, step: 1 },
    { key: 'color', label: 'Color', type: 'color' },
    { key: 'gradient', label: 'Vertical gradient', type: 'gradient', optional: true, help: 'If it has colors, it replaces the color' },
    { key: 'upcoming_color', label: 'Upcoming color (karaoke)', type: 'color' },
    { key: 'outline_color', label: 'Outline', type: 'color' },
    { key: 'outline_width', label: 'Outline width', type: 'num', min: 0, max: 15, step: 0.25 },
    { key: 'glow', label: 'Glow', type: 'group', fields: [
      { key: 'enabled', label: 'Enabled', type: 'bool' },
      { key: 'color', label: 'Color', type: 'color', optional: true },
      { key: 'radius', label: 'Radius', type: 'num', min: 0, max: 24, step: 0.5 },
      { key: 'intensity', label: 'Intensity', type: 'num', min: 0, max: 2, step: 0.05 },
    ] },
    { key: 'shadow', label: 'Shadow', type: 'group', fields: [
      { key: 'enabled', label: 'Enabled', type: 'bool' },
      { key: 'color', label: 'Color', type: 'color' },
      { key: 'offset_x', label: 'Offset X', type: 'num', min: -40, max: 40, step: 0.5 },
      { key: 'offset_y', label: 'Offset Y', type: 'num', min: -40, max: 40, step: 0.5 },
      { key: 'opacity', label: 'Opacity', type: 'num', min: 0, max: 1, step: 0.01 },
      { key: 'blur', label: 'Blur', type: 'num', min: 0, max: 20, step: 0.5 },
    ] },
    { key: 'react', label: 'Audio reaction', type: 'group', fields: [
      react('bass'),
      { key: 'scale', label: 'Pulse (scale)', type: 'num', min: 0, max: 0.5, step: 0.005 },
      { key: 'glow', label: 'Extra glow', type: 'num', min: 0, max: 2, step: 0.05 },
      { key: 'shake', label: 'Shake (px)', type: 'num', min: 0, max: 30, step: 0.5 },
      { key: 'chromatic', label: 'Aberration (px)', type: 'num', min: 0, max: 20, step: 0.25 },
      { key: 'flicker', label: 'Flame flicker', type: 'num', min: 0, max: 1, step: 0.01 },
      { key: 'flash', label: 'Color flash', type: 'num', min: 0, max: 1, step: 0.01 },
      { key: 'flash_color', label: 'Flash color', type: 'color', optional: true },
    ] },
  ] },
];

export const AUDIO_FIELDS = [
  { key: 'path', label: 'Audio file', type: 'file', kind: 'audio' },
  { key: 'volume', label: 'Analysis volume', type: 'num', min: 0.1, max: 4, step: 0.05 },
  { key: 'sensitivity', label: 'Bar sensitivity', type: 'num', min: 0.1, max: 4, step: 0.05 },
  { key: 'normalize', label: 'Adaptive normalization', type: 'bool', help: 'Every effect and bar is rescaled to its local range' },
  { key: 'normalize_window', label: 'Window (s)', type: 'num', min: 0.5, max: 20, step: 0.5 },
];

export const OUTPUT_FIELDS = [
  { key: 'path', label: 'Output file', type: 'text' },
  { key: 'width', label: 'Width (0 = background)', type: 'int', min: 0, max: 7680, step: 2 },
  { key: 'height', label: 'Height (0 = background)', type: 'int', min: 0, max: 7680, step: 2 },
  { key: 'fps', label: 'FPS', type: 'enum', def: 60, options: [[24, '24'], [25, '25'], [30, '30'], [50, '50'], [60, '60']] },
  { key: 'renderer', label: 'Renderer', type: 'enum', def: 'auto', options: [['auto', 'Auto (GPU if available)'], ['gpu', 'GPU'], ['cpu', 'CPU']] },
  { key: 'preset', label: 'x264 preset', type: 'enum', def: 'fast', options: ['ultrafast', 'superfast', 'veryfast', 'faster', 'fast', 'medium', 'slow', 'slower', 'veryslow'].map((x) => [x, x]) },
  { key: 'crf', label: 'Quality (CRF, lower = better)', type: 'int', min: 10, max: 35, step: 1 },
];

export const trackById = Object.fromEntries(TRACKS.map((t) => [t.id, t]));

// Transitions between segments. The background has visual transitions; other tracks blend.
export const TRANSITIONS = [['cut', 'Cut'], ['fade', 'Fade'], ['dissolve', 'Dissolve'], ['dip_black', 'Dip to black'],
  ['flash', 'Flash (white)'], ['wipe', 'Wipe'], ['slide', 'Slide'], ['push', 'Push'], ['zoom_in', 'Zoom in'],
  ['zoom_out', 'Zoom out'], ['circle', 'Circle (iris)'], ['clock', 'Clock wipe'], ['glitch', 'Glitch'], ['pixelate', 'Pixelate']];
export const SIMPLE_TRANSITIONS = [['cut', 'Cut'], ['fade', 'Fade']];
export const DIRECTED = ['wipe', 'slide', 'push'];
export const DIRECTIONS = [['left', 'Left'], ['right', 'Right'], ['up', 'Up'], ['down', 'Down']];
