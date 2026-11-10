// UI schema: how each config block is edited. Labels are English and translated at render time.
// Types: num, int, bool, enum, color, gradient, file, text, group, layers, optnum
// showIf(cfg) hides fields that do not apply. def is the value shown when the config leaves it empty.

export const BANDS = [
  ['bass', 'Bass'], ['mid', 'Mids'], ['treble', 'Treble'],
  ['overall', 'Volume'], ['transient', 'Transients'], ['beat', 'Beats'],
  // the song separated into its instruments (Audio → Instruments); before that, the closest from the mix
  ['drums', 'Drums · separated'], ['kick', 'Kick (hits) · separated'], ['snare', 'Snare (hits) · separated'],
  ['hats', 'Hi-hats (hits) · separated'], ['bassline', 'Bass line · separated'], ['vocals', 'Vocals · separated'],
  ['instruments', 'The rest · separated'],
];
const react = (def = 'bass') => ({ key: 'react_to', label: 'Reacts to', type: 'enum', options: BANDS, def,
  help: 'What drives it: a band of the whole song, or one instrument of the separated song (Audio → Instruments; until it is separated, the closest from the mix)' });

// A section groups fields of the same object under a title (folded until opened)
const sec = (key, label, fields) => ({ key: 'sec_' + key, label, type: 'section', fields });

const glow = { key: 'glow', label: 'Glow', type: 'group', fields: [
  { key: 'enabled', label: 'Enabled', type: 'bool' },
  { key: 'radius', label: 'Radius', type: 'num', min: 0, max: 40, step: 0.5 },
  { key: 'intensity', label: 'Intensity', type: 'num', min: 0, max: 2, step: 0.05 },
] };

const ANIMS = [['fade', 'Fade'], ['slide_up', 'Slide up'], ['slide_down', 'Slide down'],
  ['slide_left', 'Slide left'], ['slide_right', 'Slide right'], ['slide_side', 'From its side'],
  ['pop', 'Pop'], ['zoom', 'Zoom'], ['none', 'No animation']];

// Zones of the track list and the timeline, top to bottom, each with its own tint (styles.css
// .z-<id>). Layers: overlay layers and emitters (to come).
export const ZONES = [
  { id: 'main', label: 'Main' },
  { id: 'layers', label: 'Layers' },
  { id: 'effects', label: 'Effects' },
];
// Zone of a track list entry (track id or "texts"); everything else is an effect
export const zoneOf = (id) => ({ background: 'main', lyrics: 'main', texts: 'main' })[id] || 'effects';

// a background effect only on a depth range (with a depth map); not in a clip's own effects
const RANGE_KEYS = ['depth_from', 'depth_to', 'depth_soft'];
const DEPTH_RANGE = [
  { key: 'depth_only', label: 'Only on some depths', type: 'bool', anim: false, trackOnly: true,
    help: 'With a depth map: the effect only on part of the scene (near or far)' },
  { key: 'depth_from', label: 'From (near)', type: 'num', min: 0, max: 1, step: 0.005, def: 1, trackOnly: true, showIf: (c) => c.depth_only,
    adjust: 'range', rangeKeys: RANGE_KEYS, help: '1 = the nearest (white in the depth map)' },
  { key: 'depth_to', label: 'Up to (far)', type: 'num', min: 0, max: 1, step: 0.005, def: 0, trackOnly: true, showIf: (c) => c.depth_only,
    adjust: 'range', rangeKeys: RANGE_KEYS, help: '0 = the farthest (black in the depth map)' },
  { key: 'depth_soft', label: 'Soft edge', type: 'num', min: 0, max: 0.5, step: 0.005, def: 0.05, trackOnly: true, showIf: (c) => c.depth_only,
    adjust: 'range', rangeKeys: RANGE_KEYS, help: 'While you adjust these three, the preview shows the depth map with what it leaves out in black' },
];
// a clip's, a text's or the lyrics' depth in the scene (config.ClipDepth)
const CLIP_DEPTH = { key: 'depth', label: 'Depth', type: 'group', fields: [
  { key: 'mode', label: 'Its depth', type: 'enum', def: 'off', anim: false, options: [['off', 'None'],
    ['fixed', 'Fixed'], ['auto', 'Auto: the scene\'s, where it is']], help: 'Needs a background with a depth map (Image / video → Depth)' },
  { key: 'z', label: 'Depth (0 far, 1 near)', type: 'num', min: 0, max: 1, step: 0.005, showIf: (c) => c.mode === 'fixed', adjust: 'objdepth',
    help: 'While you adjust it the preview shows the depth map, with what is behind it in red' },
  { key: 'z', label: 'Nearer / farther than the scene', type: 'num', min: -0.5, max: 0.5, step: 0.005, showIf: (c) => c.mode === 'auto', adjust: 'objdepth',
    help: 'While you adjust it the preview shows the depth map, with what is behind it in red' },
  { key: 'behind', label: 'Behind what is nearer', type: 'bool', anim: false, showIf: (c) => c.mode === 'fixed' || c.mode === 'auto',
    help: 'Hidden where the scene is nearer than it (it goes behind things)' },
  { key: 'soft', label: 'Soft edge', type: 'num', min: 0, max: 0.3, step: 0.005, showIf: (c) => c.behind && c.mode && c.mode !== 'off', adjust: 'objdepth' },
] };
export const TRACKS = [
  { id: 'background', group: 'Background', label: 'Image / video', icon: 'image', fields: [
    { key: 'path', label: 'File', type: 'file', kind: 'bg', help: 'Image, GIF or video (mp4, mov, mkv, webm)' },
    { key: 'fit_mode', label: 'Fit', type: 'enum', def: 'cover', options: [['cover', 'Cover (crops)'], ['contain', 'Contain (bars)'], ['stretch', 'Stretch']] },
    sec('look', 'Look', [
      { key: 'opacity', label: 'Opacity', type: 'num', min: 0, max: 1, step: 0.01 },
      { key: 'darken', label: 'Darken', type: 'num', min: 0, max: 1, step: 0.01 },
      { key: 'vignette', label: 'Vignette', type: 'num', min: 0, max: 1, step: 0.01 },
    ]),
    sec('video', 'Video / GIF', [
      { key: 'loop_video', label: 'Loop video / GIF', type: 'bool' },
    ]),
    { key: 'motion', label: 'Motion', type: 'group', fields: [
      { key: 'type', label: 'Type', type: 'enum', def: 'none', help: 'Slow camera motion over the image during this segment',
        options: [['none', 'Still'], ['zoom_in', 'Slow zoom in'], ['zoom_out', 'Slow zoom out'], ['pan_left', 'Pan left'],
          ['pan_right', 'Pan right'], ['pan_up', 'Pan up'], ['pan_down', 'Pan down'], ['ken_burns', 'Ken Burns (zoom + pan)'],
          ['drift', 'Drift (floating)'], ['sway', 'Sway (rotation)']] },
      { key: 'amount', label: 'Amount', type: 'num', min: 0.02, max: 0.6, step: 0.01, showIf: (c) => (c.type || 'none') !== 'none' },
      { key: 'easing', label: 'Easing', type: 'enum', def: 'linear', options: [['linear', 'Constant speed'], ['ease', 'Soft start and end']],
        showIf: (c) => !['none', 'drift', 'sway'].includes(c.type || 'none') },
      { key: 'direction', label: 'Towards', type: 'enum', def: 'auto', help: 'Corner the camera moves to while zooming in',
        options: [['auto', 'Auto (varies per segment)'], ['top_left', 'Top left'], ['top_right', 'Top right'],
          ['bottom_left', 'Bottom left'], ['bottom_right', 'Bottom right']], showIf: (c) => c.type === 'ken_burns' },
      { key: 'speed', label: 'Speed', type: 'num', min: 0.1, max: 5, step: 0.1, help: 'Cycles every 10 s', showIf: (c) => ['drift', 'sway'].includes(c.type) },
    ] },
  ] },
  { id: 'bass_zoom', group: 'Background effects', label: 'Zoom', icon: 'zoom-in', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool' },
    { key: 'max_zoom', label: 'Max zoom', type: 'num', min: 1, max: 1.4, step: 0.005 },
    { key: 'sensitivity', label: 'Sensitivity', type: 'num', min: 0, max: 3, step: 0.05 },
    { key: 'depth', label: '2.5D (camera moving in)', type: 'num', min: 0, max: 1, step: 0.05, def: 0, trackOnly: true,
      help: 'With a depth map: 0 = a flat zoom; 1 = the camera moves in (what is near grows more than what is far)' },
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
    ...DEPTH_RANGE,
  ] },
  { id: 'ripple', group: 'Background effects', label: 'Ripple', icon: 'waves', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool' },
    { key: 'amplitude', label: 'Amplitude (px)', type: 'num', min: 0, max: 30, step: 0.5 },
    { key: 'frequency', label: 'Frequency', type: 'num', min: 0.002, max: 0.2, step: 0.001 },
    { key: 'speed', label: 'Speed', type: 'num', min: 0, max: 6, step: 0.05 },
    react('mid'),
    ...DEPTH_RANGE,
  ] },
  { id: 'glitch', group: 'Background effects', label: 'Glitch', icon: 'scan-line', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool' },
    { key: 'intensity', label: 'Intensity', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'threshold', label: 'Threshold', type: 'num', min: 0, max: 1, step: 0.01, help: 'Triggers above this value' },
    { key: 'react_to', label: 'Triggered by', type: 'enum', def: '', options: [['', 'Beats / transients'], ...BANDS] },
    ...DEPTH_RANGE,
  ] },
  { id: 'color_flash', group: 'Background effects', label: 'Color flash', icon: 'sun', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool' },
    { key: 'brightness_boost', label: 'Extra brightness', type: 'num', min: 0, max: 0.8, step: 0.01 },
    { key: 'contrast_boost', label: 'Extra contrast', type: 'num', min: 0, max: 0.8, step: 0.01 },
    react('bass'),
    ...DEPTH_RANGE,
  ] },
  { id: 'parallax', group: 'Background effects', label: 'Parallax 2.5D', icon: 'focus', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool',
      help: 'A camera that moves through the image: what is near moves more than what is far. Needs a background with a depth map (Image / video → Depth)' },
    { key: 'motion', label: 'Camera', type: 'enum', def: 'sway', anim: false, options: [['sway', 'Sway (a slow drift)'],
      ['beat', 'With the music (pushes and comes back)'], ['still', 'Still (move it with segments or keys)']] },
    { key: 'amount', label: 'Movement (px)', type: 'num', min: 0, max: 200, step: 1, help: 'How far the nearest things move against the focus' },
    { key: 'x', label: 'Camera X', type: 'num', min: -1, max: 1, step: 0.01, help: 'Where it looks from: -1 left … 1 right (swaying: the middle of its drift)' },
    { key: 'y', label: 'Camera Y', type: 'num', min: -1, max: 1, step: 0.01, help: '-1 up … 1 down' },
    { key: 'dolly', label: 'Moving in', type: 'num', min: -0.3, max: 0.3, step: 0.005, help: 'What is near grows more than what is far (negative: moving out)' },
    { key: 'focus', label: 'Focus', type: 'num', min: 0, max: 1, step: 0.01, def: 0.5,
      help: 'The depth that stays still (0 far … 1 near): nearer things move one way, farther ones the other' },
    { key: 'speed', label: 'Speed', type: 'num', min: 0.05, max: 5, step: 0.05, showIf: (c) => c.motion !== 'still' && c.motion !== 'beat' },
    { key: 'react', label: 'Audio reaction', type: 'num', min: 0, max: 1, step: 0.01, def: 0, showIf: (c) => c.motion !== 'beat',
      help: 'How much the music pushes the camera further (its movement and its moving in): 1 = up to twice on the hits' },
    { ...react('bass'), showIf: (c) => c.motion === 'beat' || c.react > 0 },
  ] },
  { id: 'depth_of_field', group: 'Background effects', label: 'Depth of field', icon: 'aperture', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool',
      help: 'With a depth map: only a range of depths in focus, the rest blurred the farther it is, as a lens' },
    { key: 'from', label: 'In focus from (near)', type: 'num', min: 0, max: 1, step: 0.005, def: 1, adjust: 'range', rangeKeys: ['from', 'to', 'soft'],
      help: '1 = the nearest. While you adjust it the preview shows the depth map with what is out of focus in black' },
    { key: 'to', label: 'In focus up to (far)', type: 'num', min: 0, max: 1, step: 0.005, def: 0.5, adjust: 'range', rangeKeys: ['from', 'to', 'soft'],
      help: '0 = the farthest' },
    { key: 'soft', label: 'Blurs over (depth)', type: 'num', min: 0.01, max: 1, step: 0.005, def: 0.2,
      help: 'How far past the focus it reaches its full blur' },
    { key: 'blur', label: 'Blur (px)', type: 'num', min: 0, max: 40, step: 0.5, def: 8, help: 'Clips and emitters with a depth blur with it too' },
    { key: 'react', label: 'Blur with the music', type: 'num', min: 0, max: 1, step: 0.01, def: 0,
      help: '0 = always all of it; 1 = all of it only at full band' },
    { key: 'focus_move', label: 'Focus moves with the music', type: 'num', min: -0.5, max: 0.5, step: 0.005, def: 0,
      help: 'How far the focus moves at full band (+ nearer, - farther)' },
    react('bass'),
  ] },
  { id: 'visualizer', group: 'Visualizer', label: 'Spectrum', icon: 'chart-column', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool' },
    { key: 'type', label: 'Type', type: 'enum', def: 'circular', options: [['circular', 'Circular'], ['horizontal', 'Horizontal'], ['vertical', 'Vertical (edges)'], ['oscilloscope', 'Oscilloscope']] },
    sec('bars', 'Bars', [
      { key: 'bars', label: 'Bars per channel', type: 'int', min: 4, max: 256, step: 1 },
      { key: 'min_height', label: 'Min height', type: 'num', min: 0, max: 100, step: 0.5 },
      { key: 'max_height', label: 'Max height', type: 'num', min: 10, max: 1200, step: 1 },
      { key: 'bar_width', label: 'Thickness', type: 'num', min: 0.5, max: 60, step: 0.5 },
      { key: 'bar_gap', label: 'Gap', type: 'num', min: 0, max: 40, step: 0.5, showIf: (c) => c.type === 'horizontal' },
      { key: 'center_gap', label: 'Center gap (px)', type: 'optnum', min: 0, max: 200, step: 1, showIf: (c) => c.type === 'horizontal', help: 'Off = 2 × gap' },
      { key: 'roundness', label: 'Roundness', type: 'num', min: 0, max: 30, step: 0.5, showIf: (c) => c.type === 'horizontal' || c.type === 'vertical' },
    ]),
    sec('place', 'Position', [
      { key: 'radius', label: 'Radius', type: 'num', min: 0, max: 1500, step: 1, showIf: (c) => c.type === 'circular' },
      { key: 'center_x', label: 'Center X', type: 'num', min: 0, max: 1, step: 0.005, showIf: (c) => c.type === 'circular' },
      { key: 'center_y', label: 'Center Y', type: 'num', min: 0, max: 1, step: 0.005, showIf: (c) => c.type === 'circular' },
      { key: 'position_y', label: 'Height (0 top, 1 bottom)', type: 'num', min: 0, max: 1, step: 0.005, showIf: (c) => c.type === 'horizontal' || c.type === 'oscilloscope' },
      { key: 'mirror_bottom', label: 'Reflection', type: 'bool', showIf: (c) => c.type === 'horizontal' },
    ]),
    sec('motion', 'Movement', [
      { key: 'smoothing', label: 'Rise speed', type: 'num', min: 0.05, max: 0.98, step: 0.01 },
      { key: 'decay_rate', label: 'Fall speed', type: 'num', min: 0.01, max: 0.5, step: 0.005 },
    ]),
    sec('color', 'Color', [
      { key: 'color_mode', label: 'Color', type: 'enum', def: 'gradient', options: [['gradient', 'Gradient'], ['solid', 'Solid']] },
      { key: 'solid_color', label: 'Solid color', type: 'color', showIf: (c) => c.color_mode === 'solid' },
      { key: 'gradient', label: 'Gradient', type: 'gradient', showIf: (c) => c.color_mode !== 'solid' },
      { key: 'gradient_axis', label: 'Direction', type: 'enum', def: 'index', options: [['index', 'Per bar (across)'], ['length', 'Along each bar']], showIf: (c) => c.color_mode !== 'solid' },
      { key: 'gradient_fit', label: 'Reference', type: 'enum', def: 'bar', options: [['bar', 'Moves with the bar'], ['max', 'Relative to max height']], showIf: (c) => c.color_mode !== 'solid' && c.gradient_axis === 'length' },
    ]),
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
    sec('font', 'Font', [
      { key: 'font', label: 'Font', type: 'file', kind: 'font', help: 'Empty = built-in font' },
      { key: 'size', label: 'Size', type: 'num', min: 8, max: 300, step: 1 },
      { key: 'uppercase', label: 'Uppercase', type: 'bool' },
    ]),
    sec('layout', 'Position and layout', [
      { key: 'position', label: 'Position', type: 'enum', def: 'bottom', options: [['bottom', 'Bottom'], ['top', 'Top'], ['center', 'Center'], ['left', 'Left'], ['right', 'Right'], ['side', 'Alternate left/right'], ['custom', 'Custom']] },
      { key: 'margin', label: 'Margin (px)', type: 'num', min: 0, max: 1000, step: 1 },
      { key: 'position_x', label: 'Position X', type: 'num', min: 0, max: 1, step: 0.005, showIf: (c) => c.position === 'custom' },
      { key: 'position_y', label: 'Position Y', type: 'num', min: 0, max: 1, step: 0.005, showIf: (c) => ['custom', 'left', 'right', 'side'].includes(c.position) },
      { key: 'max_width', label: 'Max width', type: 'num', min: 0.1, max: 1, step: 0.01 },
      { key: 'line_spacing', label: 'Line spacing', type: 'num', min: 0.5, max: 3, step: 0.05 },
      { key: 'word_spacing', label: 'Word spacing', type: 'num', min: 0, max: 1, step: 0.01 },
      { key: 'align', label: 'Alignment', type: 'enum', def: 'center', options: [['center', 'Center'], ['left', 'Left'], ['right', 'Right']], showIf: (c) => ['top', 'bottom', 'center', 'custom'].includes(c.position) },
    ]),
    sec('anim', 'Animation', [
      { key: 'mode', label: 'Mode', type: 'enum', def: 'karaoke', options: [['karaoke', 'Karaoke (fills in)'], ['words', 'Word by word'], ['line', 'Whole line']] },
      { key: 'word_pop', label: 'Word pop', type: 'num', min: 0, max: 1, step: 0.01 },
      { key: 'in_animation', label: 'In animation', type: 'enum', def: 'fade', options: ANIMS },
      { key: 'out_animation', label: 'Out animation', type: 'enum', def: 'fade', options: ANIMS },
      { key: 'in_duration', label: 'In duration (s)', type: 'num', min: 0.01, max: 3, step: 0.01 },
      { key: 'out_duration', label: 'Out duration (s)', type: 'num', min: 0.01, max: 3, step: 0.01 },
      { key: 'slide_distance', label: 'Slide distance', type: 'num', min: 0, max: 600, step: 1 },
    ]),
    sec('colors', 'Colors and outline', [
      { key: 'color', label: 'Color', type: 'color' },
      { key: 'gradient', label: 'Vertical gradient', type: 'gradient', optional: true, help: 'If it has colors, it replaces the color' },
      { key: 'upcoming_color', label: 'Upcoming color (karaoke)', type: 'color' },
      { key: 'outline_color', label: 'Outline', type: 'color' },
      { key: 'outline_width', label: 'Outline width', type: 'num', min: 0, max: 15, step: 0.25 },
    ]),
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
    CLIP_DEPTH,
    { key: 'light', label: 'Light on the scene', type: 'group', fields: [
      { key: 'enabled', label: 'Lights what is behind', type: 'bool', help: 'Its letters light the background and the layers below it' },
      { key: 'intensity', label: 'Strength', type: 'num', min: 0, max: 4, step: 0.01, def: 0.8, showIf: (c) => c.enabled },
      { key: 'reach', label: 'Reach (px)', type: 'num', min: 20, max: 3000, step: 5, def: 300, showIf: (c) => c.enabled,
        help: 'Where it is still strong' },
      { key: 'end', label: 'Fades out at (px)', type: 'num', min: 0, max: 8000, step: 5, showIf: (c) => c.enabled, help: '0 = 2.5 × the reach' },
      { key: 'color', label: 'Color', type: 'color', optional: true, showIf: (c) => c.enabled, help: 'Empty: the text\'s own (its glow\'s if it glows)' },
      { key: 'mode', label: 'How it lights', type: 'enum', def: 'soft', anim: false, showIf: (c) => c.enabled,
        options: [['soft', 'Soft (lights and tints what is there)'], ['add', 'Add (brighter, like neon)']] },
      { key: 'react', label: 'More with the music', type: 'num', min: 0, max: 4, step: 0.05, def: 0, showIf: (c) => c.enabled,
        help: 'How much the band (Audio reaction → Reacts to) adds to its strength' },
      { key: 'quiet', label: 'Weaker when quiet', type: 'num', min: 0, max: 1, step: 0.01, def: 0, showIf: (c) => c.enabled,
        help: 'How much it dims where the band (Audio reaction → Reacts to) is low (1 = dark in silence, full strength at full band)' },
      { key: 'depth', label: 'Fades with depth', type: 'num', min: 0, max: 10, step: 0.1, def: 0, showIf: (c) => c.enabled,
        help: 'With a background that has depth: how fast it fades away from the text\'s depth (0 = flat, as in 2D)' },
      { key: 'shade', label: 'Surfaces shading', type: 'num', min: 0, max: 1, step: 0.01, def: 0, showIf: (c) => c.enabled,
        help: 'With depth: the side of the surfaces that faces it gets more light' },
      { key: 'gloss', label: 'Shine', type: 'num', min: 0, max: 1, step: 0.01, def: 0, showIf: (c) => c.enabled,
        help: 'With depth: highlights on the surfaces that face it (wet skin, metal); more = brighter and sharper' },
      { key: 'shadows', label: 'Shadows', type: 'num', min: 0, max: 1, step: 0.01, def: 0, showIf: (c) => c.enabled,
        help: 'With depth: what is nearer and stands between the light and a surface casts a shadow on it' },
      { key: 'from', label: 'Lights from (near)', type: 'num', min: 0, max: 1, step: 0.005, def: 1, showIf: (c) => c.enabled,
        adjust: 'range', rangeKeys: ['light.from', 'light.to', 'light.range_soft'],
        help: 'With depth: what is nearer than this is not lit (1 = white in the depth map, the nearest); above 1 = in front of it)' },
      { key: 'to', label: 'Lights up to (far)', type: 'num', min: 0, max: 1, step: 0.005, def: 0, showIf: (c) => c.enabled,
        adjust: 'range', rangeKeys: ['light.from', 'light.to', 'light.range_soft'],
        help: 'What is farther than this is too far and is not lit (0 = black in the depth map, the farthest; below 0 = behind it, so even the farthest gets all the light with a soft edge)' },
      { key: 'range_soft', label: 'Soft edge of that', type: 'num', min: 0, max: 0.5, step: 0.005, def: 0.05, showIf: (c) => c.enabled,
        adjust: 'range', rangeKeys: ['light.from', 'light.to', 'light.range_soft'],
        help: 'While you adjust these three, the preview shows the depth map with what is not lit in black' },
      { key: 'rim', label: 'Only the edges, far away', type: 'bool', anim: false, showIf: (c) => c.enabled,
        help: 'With depth: farther than a circle it no longer lights the surfaces, only the edges of things that face it (rim light)' },
      { key: 'rim_from', label: 'Edges from (px)', type: 'num', min: 0, max: 6000, step: 5, def: 500, showIf: (c) => c.enabled && c.rim,
        help: 'Farther than this only the edges (or drag the handle of its circle; 0 = edges everywhere). Up to where the light fades out' },
      { key: 'rim_soft', label: 'Change over (px)', type: 'num', min: 0, max: 2000, step: 5, def: 120, showIf: (c) => c.enabled && c.rim },
      { key: 'rim_width', label: 'Edge width (px)', type: 'num', min: 1, max: 80, step: 0.5, def: 10, showIf: (c) => c.enabled && c.rim },
      { key: 'rim_gain', label: 'Edge strength', type: 'num', min: 0, max: 10, step: 0.05, def: 2, showIf: (c) => c.enabled && c.rim },
      { key: 'rim_edge', label: 'What counts as an edge', type: 'num', min: 0.005, max: 0.4, step: 0.005, def: 0.04, showIf: (c) => c.enabled && c.rim,
        help: 'How much the surface must turn away from you to be an edge: less = more edges (even small bumps)' },
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

// Drawing order among the overlay layers (config z_index): for the visualizer, the particles,
// the lyrics and the texts
const ZFIELD = { key: 'z_index', label: 'Drawing order', type: 'zindex', anim: false,
  help: 'Where it is drawn among the overlay layers' };
for (const id of ['visualizer', 'particles', 'lyrics']) trackById[id].fields.splice(1, 0, ZFIELD);

// Keyframes (keys.js): numbers, colors and switches can be animated, except the ones that set
// up something built once (the lyrics layout, the number of bars or particles...)
const STILL = {
  background: ['loop_video'],
  visualizer: ['bars'],
  particles: ['enabled', 'count'],
  lyrics: ['enabled', 'offset', 'size', 'uppercase', 'margin', 'position_x', 'position_y', 'max_width', 'line_spacing',
    'word_spacing', 'word_pop', 'in_duration', 'out_duration', 'slide_distance'],
};
const markStill = (fields, keys) => fields.map((f) => (keys.includes(f.key) ? { ...f, anim: false }
  : f.fields ? { ...f, fields: markStill(f.fields, keys) } : f));
for (const tr of TRACKS) if (STILL[tr.id]) tr.fields = markStill(tr.fields, STILL[tr.id]);

// Transitions between segments. The background has visual transitions; other tracks blend.
export const TRANSITIONS = [['cut', 'Cut'], ['fade', 'Fade'], ['dissolve', 'Dissolve'], ['dip_black', 'Dip to black'],
  ['flash', 'Flash (white)'], ['wipe', 'Wipe'], ['slide', 'Slide'], ['push', 'Push'], ['zoom_in', 'Zoom in'],
  ['zoom_out', 'Zoom out'], ['circle', 'Circle (iris)'], ['clock', 'Clock wipe'], ['glitch', 'Glitch'], ['pixelate', 'Pixelate'], ['burn', 'Burn (paper)'], ['fluid', 'Fluid'], ['liquid', 'Liquid']];
export const SIMPLE_TRANSITIONS = [['cut', 'Cut'], ['fade', 'Fade']];
export const DIRECTED = ['wipe', 'slide', 'push'];
export const DIRECTIONS = [['left', 'Left'], ['right', 'Right'], ['up', 'Up'], ['down', 'Down']];

// It follows a motion path (its position comes from it)
const onPath = (c) => !!(c.motion && c.motion.path && (c.motion.path.points || []).length >= 2);

// Fixed texts (titles): the lyrics style options, positioned by hand
const TEXT_SKIP = new Set(['enabled', 'path', 'offset', 'position', 'margin', 'mode', 'word_pop', 'upcoming_color']);
const textField = (f) => {
  if (f.type === 'section') return { ...f, fields: f.fields.flatMap(textField) };
  if (TEXT_SKIP.has(f.key)) return [];
  // A fixed text can move, scale and turn over time (keyframes)
  if (f.key === 'position_x' || f.key === 'position_y') return [{ ...f, anim: true, showIf: (c) => !onPath(c), help: f.key === 'position_x' ? 'Or drag the text on the preview' : undefined }];
  if (f.key === 'size') return [{ ...f, anim: true }];
  if (f.key === 'color') return [f, { key: 'opacity', label: 'Opacity', type: 'num', min: 0, max: 1, step: 0.01, def: 1 }];
  if (f.key === 'align') return [{ ...f, showIf: undefined, help: 'Left / right: the text starts / ends at Position X' }];
  if (f.key === 'max_width') return [f, { key: 'rotation', label: 'Rotation (°)', type: 'num', min: -360, max: 360, step: 1, def: 0, anim: true,
    help: 'Clockwise, around its position' }];
  return [f];
};
export const TEXT_FIELDS = [
  { key: 'enabled', label: 'Visible', type: 'bool', anim: false },
  { key: 'text', label: 'Text', type: 'textarea', help: 'Enter = new row' },
  ...trackById.lyrics.fields.flatMap(textField),
];

// Overlay layers: the clip inspector (config.ClipConfig). Every number and color can have keys.
const EFFECT_MODES = [['inherit', 'Like the background'], ['own', 'Own'], ['off', 'Off']];
const layerEffect = (key, label) => ({ key, label, type: 'group', fields: [
  { key: 'mode', label: 'Mode', type: 'enum', def: 'inherit', options: EFFECT_MODES, anim: false },
  { key: 'own', label: 'Own settings', type: 'group', showIf: (c) => c.mode === 'own',
    fields: trackById[key].fields.filter((f) => !f.trackOnly) },
] });
export const BLENDS = [['normal', 'Normal'], ['add', 'Add (light)'], ['multiply', 'Multiply (darken)'], ['screen', 'Screen'],
  ['overlay', 'Overlay'], ['lighten', 'Lighten'], ['darken', 'Darken']];
export const CLIP_FIELDS = [
  { key: 'path', label: 'File', type: 'file', kind: 'bg', help: 'Image (PNG / WebP with transparency), GIF or video' },
  { key: 'fit', label: 'Base size', type: 'enum', def: 'contain', anim: false, options: [['contain', 'Inside the frame'],
    ['cover', 'Covering the frame'], ['stretch', 'Exactly the frame'], ['none', 'Its own pixels']] },
  sec('place', 'Placement', [
    { key: 'x', label: 'Position X', type: 'num', min: -0.5, max: 1.5, step: 0.001, help: 'Or drag it on the preview', showIf: (c) => !onPath(c) },
    { key: 'y', label: 'Position Y', type: 'num', min: -0.5, max: 1.5, step: 0.001, showIf: (c) => !onPath(c) },
    { key: 'scale', label: 'Scale', type: 'num', min: 0.01, max: 4, step: 0.005, help: 'Or drag a corner on the preview' },
    { key: 'rotation', label: 'Rotation (°)', type: 'num', min: -360, max: 360, step: 0.5, help: 'Or drag the round handle on the preview' },
    { key: 'flip_x', label: 'Flip horizontally', type: 'bool' },
    { key: 'flip_y', label: 'Flip vertically', type: 'bool' },
  ]),
  sec('show', 'Opacity and blend', [
    { key: 'opacity', label: 'Opacity', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'fade_in', label: 'Fade in (s)', type: 'num', min: 0, max: 10, step: 0.05, anim: false },
    { key: 'fade_out', label: 'Fade out (s)', type: 'num', min: 0, max: 10, step: 0.05, anim: false },
    { key: 'blend', label: 'Blend', type: 'enum', def: 'normal', options: BLENDS },
  ]),
  CLIP_DEPTH,
  { key: 'filters', label: 'Filters', type: 'section', fields: [
    { key: 'brightness', label: 'Brightness', type: 'num', min: -1, max: 1, step: 0.01 },
    { key: 'contrast', label: 'Contrast', type: 'num', min: -1, max: 1, step: 0.01 },
    { key: 'saturation', label: 'Saturation', type: 'num', min: -1, max: 1, step: 0.01, help: '-1 = black and white' },
    { key: 'hue', label: 'Hue (°)', type: 'num', min: -180, max: 180, step: 1 },
    { key: 'blur', label: 'Blur (px)', type: 'num', min: 0, max: 40, step: 0.5 },
  ] },
  { key: 'chroma', label: 'Chroma key (green screen)', type: 'group', fields: [
    { key: 'enabled', label: 'Enabled', type: 'bool' },
    { key: 'color', label: 'Key color', type: 'color' },
    { key: 'tolerance', label: 'Tolerance', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'softness', label: 'Soft edge', type: 'num', min: 0, max: 1, step: 0.01 },
  ] },
  { key: 'react', label: 'Audio reaction', type: 'group', fields: [
    react('bass'),
    { key: 'opacity', label: 'Opacity pulse', type: 'num', min: 0, max: 1, step: 0.01, help: '1 = only visible when the band sounds' },
    { key: 'scale', label: 'Scale pulse', type: 'num', min: 0, max: 1, step: 0.01 },
  ] },
  { key: 'video', label: 'Video / GIF', type: 'group', fields: [
    { key: 'trim', label: 'Start at (s)', type: 'num', min: 0, max: 600, step: 0.05, anim: false, help: 'Seconds of the file skipped' },
    { key: 'speed', label: 'Speed', type: 'num', min: 0.1, max: 4, step: 0.05, anim: false },
    { key: 'loop', label: 'Loop', type: 'bool', anim: false },
  ] },
  { key: 'effects', label: 'Background effects on this clip', type: 'group', fields: [
    layerEffect('bass_zoom', 'Zoom'), layerEffect('shake', 'Camera shake'), layerEffect('chromatic_aberration', 'Chromatic aberration'),
    layerEffect('ripple', 'Ripple'), layerEffect('glitch', 'Glitch'), layerEffect('color_flash', 'Color flash'),
  ] },
];

// Emitter clips (config.EmitterConfig): the clip places the emitter, the emitter block says what
// it throws. Every number and color can have keys.
// grouped by what they make (fire, smoke, fog, clouds, water…), in this order everywhere
export const EMITTER_GROUPS = [
  ['Fire', [['fire', 'Fire', 'flame'], ['fire_real', 'Realistic fire', 'flame'], ['fire_gas', 'Simulated fire', 'flame'],
    ['fire_gas_haze', 'Simulated fire with heat haze', 'flame'], ['heat_haze', 'Heat haze (invisible)', 'waves'],
    ['explosion', 'Explosion', 'zap'], ['sparks', 'Sparks', 'sparkles']]],
  ['Smoke', [['smoke', 'Smoke', 'cloud'], ['smoke_real', 'Simulated smoke', 'cloud'], ['steam', 'Steam', 'cloud']]],
  ['Fog', [['fog_gas', 'Ground fog', 'cloud-fog']]],
  ['Clouds', [['clouds_gas', 'Storm clouds', 'cloud']]],
  ['Water', [['water', 'Water', 'droplet'], ['water_clear', 'Clear water (lens)', 'droplet'], ['splash', 'Water splash', 'waves'],
    ['rain', 'Rain', 'cloud-rain'], ['water_sim', 'Simulated water', 'droplet'], ['water_sim_clear', 'Simulated clear water (lens)', 'droplet'],
    ['water_3d', 'Water on the scene (3D)', 'droplet'], ['water_3d_clear', 'Clear water on the scene (3D, lens)', 'droplet'],
    ['splash_sim', 'Simulated splash', 'waves']]],
  ['Other', [['dust', 'Dust', 'focus'], ['light', 'Lights (bokeh)', 'sun'], ['snow', 'Snow', 'snowflake'],
    ['lamp', 'Light source (invisible)', 'sun']]],
];
export const EMITTER_PRESETS = EMITTER_GROUPS.flatMap((g) => g[1]);
const EMIT_SHAPES = [['point', 'Point'], ['circle', 'Circle (filled)'], ['ring', 'Ring'], ['line', 'Line'], ['box', 'Rectangle'],
  ['text', 'The letters of a text']];
// an emitter light's reaction to the music (in Audio reaction)
const LIGHT_MUSIC = [
  { key: 'react_to', label: 'Its light reacts to', type: 'enum', def: '', anim: false, showIf: (c) => c.enabled,
    options: [['', 'As the particles (Audio reaction)'], ...BANDS],
    help: 'The band or instrument that makes its light stronger or weaker (the two settings below)' },
  { key: 'react', label: 'Light stronger with the music', type: 'num', min: 0, max: 4, step: 0.05, def: 0, showIf: (c) => c.enabled,
    help: 'How much the band adds to its strength (1 = twice as strong at full band)' },
  { key: 'quiet', label: 'Light weaker when quiet', type: 'num', min: 0, max: 1, step: 0.01, def: 0, showIf: (c) => c.enabled,
    help: 'How much it dims where the band is low (1 = dark in silence, full strength at full band)' },
];
const EMITTER_BLOCK = { key: 'emitter', label: 'Particles', type: 'group', fields: [
  { key: 'amount', label: 'Amount', type: 'section', fields: [
    { key: 'rate', label: 'Per second', type: 'num', min: 0, max: 3000, step: 1 },
    { key: 'burst', label: 'Burst at the start', type: 'int', min: 0, max: 20000, step: 1, help: 'Particles thrown at once when the clip starts' },
    { key: 'max', label: 'Most alive at once', type: 'int', min: 10, max: 200000, step: 10, anim: false },
  ] },
  { key: 'origin', label: 'Where they are born', type: 'section', fields: [
    { key: 'shape', label: 'Shape', type: 'enum', def: 'point', options: EMIT_SHAPES, anim: false },
    { key: 'width', label: 'Width / diameter (px)', type: 'num', min: 0, max: 4000, step: 1, showIf: (c) => c.shape && c.shape !== 'point' && c.shape !== 'text' },
    { key: 'height', label: 'Height (px)', type: 'num', min: 0, max: 4000, step: 1, showIf: (c) => c.shape === 'box' },
    { key: 'text', label: 'Text', type: 'textpick', anim: false, showIf: (c) => c.shape === 'text',
      help: 'Born on its letters, following it as it moves, turns and grows (it can be hidden: then only the particles show it)' },
    { key: 'text_part', label: 'On its', type: 'enum', def: 'outline', anim: false, showIf: (c) => c.shape === 'text',
      options: [['outline', 'Outline'], ['fill', 'Whole letters']] },
    { key: 'follow_path', label: 'They travel along the motion path', type: 'bool', anim: false,
      help: 'With a motion path (Motion path section): they go along it at their speed, from where the emitter is on it; the shape\'s width spreads them across it' },
  ] },
  { key: 'launch', label: 'How they leave', type: 'section', fields: [
    { key: 'direction', label: 'Direction (°)', type: 'num', min: -180, max: 180, step: 1, help: '0 = up, 90 = right (plus the clip\'s rotation)' },
    { key: 'spread', label: 'Spread (°)', type: 'num', min: 0, max: 360, step: 1, help: '360 = every way' },
    { key: 'speed', label: 'Speed (px/s)', type: 'num', min: 0, max: 4000, step: 1 },
    { key: 'speed_var', label: 'Speed variation', type: 'num', min: 0, max: 1, step: 0.01 },
  ] },
  { key: 'forces', label: 'Forces', type: 'section', fields: [
    { key: 'gravity', label: 'Gravity (px/s²)', type: 'num', min: -3000, max: 3000, step: 1, help: 'Negative: they rise (fire, smoke)' },
    { key: 'wind', label: 'Wind (px/s²)', type: 'num', min: -3000, max: 3000, step: 1, help: 'Positive: to the right' },
    { key: 'drag', label: 'Drag', type: 'num', min: 0, max: 10, step: 0.05, help: 'How fast they slow down' },
    { key: 'turbulence', label: 'Turbulence (px)', type: 'num', min: 0, max: 400, step: 1 },
    { key: 'turb_freq', label: 'Turbulence speed (Hz)', type: 'num', min: 0, max: 8, step: 0.05 },
  ] },
  { key: 'floorsec', label: 'Floor and the scene', type: 'section', fields: [
    { key: 'floor', label: 'Floor below it (px)', type: 'num', min: 0, max: 3000, step: 1, def: 0,
      help: '0 = none. With gravity they fall onto it and bounce' },
    { key: 'floor_depth', label: 'The scene\'s floor', type: 'bool', anim: false,
      help: 'With a depth map: where the depth below the emitter comes up to its own (plus the px above)' },
    { key: 'bounce', label: 'Bounce', type: 'num', min: 0, max: 0.95, step: 0.01, def: 0.5, showIf: (c) => c.floor > 0 || c.floor_depth,
      help: 'How much of their speed they keep at each bounce' },
    { key: 'friction', label: 'Sideways speed kept', type: 'num', min: 0, max: 1, step: 0.01, def: 0.7, showIf: (c) => c.floor > 0 || c.floor_depth },
    { key: 'collide', label: 'They go round the scene', type: 'bool', anim: false, adjust: 'solid',
      help: 'With a depth map: what is in the scene at their depth is solid: they slide off it (what is nearer hides them, what is farther is behind). With the gas look, the gas flows round it too' },
    { key: 'collide_band', label: 'Solid within (depth)', type: 'num', min: 0.005, max: 0.5, step: 0.005, def: 0.06, showIf: (c) => c.collide, adjust: 'solid',
      help: 'How far in depth from the emitter something counts as solid: more = thicker things. While you adjust it the preview shows what is solid in cyan' },
    { key: 'collide_bounce', label: 'Bounce off it', type: 'num', min: 0, max: 0.95, step: 0.01, def: 0, showIf: (c) => c.collide,
      help: '0 = they slide along it (flames lick it); more: they bounce off walls and ceilings (sparks), keeping that much speed. On what faces up they still slide' },
  ] },
  { key: 'lifesize', label: 'Life and size', type: 'section', fields: [
    { key: 'life', label: 'Life (s)', type: 'num', min: 0.05, max: 30, step: 0.05 },
    { key: 'life_var', label: 'Life variation', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'size', label: 'Size at birth (px)', type: 'num', min: 0.5, max: 600, step: 0.5 },
    { key: 'size_end', label: 'Size at the end (px)', type: 'num', min: 0, max: 600, step: 0.5 },
    { key: 'size_var', label: 'Size variation', type: 'num', min: 0, max: 1, step: 0.01 },
  ] },
  { key: 'look', label: 'Colors', type: 'section', fields: [
    { key: 'color_start', label: 'Color at birth', type: 'color' },
    { key: 'color_mid', label: 'Color halfway', type: 'color' },
    { key: 'color_end', label: 'Color at the end', type: 'color', help: 'The opacity of each color fades them over their life' },
    { key: 'fade_in', label: 'Fade in (part of the life)', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'fade_out', label: 'Fade out (part of the life)', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'mode', label: 'They add up as', type: 'enum', def: 'add', anim: false,
      options: [['add', 'Light (fire, sparks, glows)'], ['soft', 'Matter (smoke, dust, snow)']] },
    { key: 'streak', label: 'Streak (s of motion)', type: 'num', min: 0, max: 0.3, step: 0.001, help: 'Stretched along their motion, like sparks' },
    { key: 'softness', label: 'Soft edge', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'seed', label: 'Random seed', type: 'int', min: 0, max: 99999, step: 1, anim: false, help: 'Another number, another pattern' },
    { key: 'hidden', label: 'Only its light (and glow)', type: 'bool', anim: false,
      help: 'The particles are not drawn: it only lights the scene (Light) and glows, if it has a glow. A light source' },
  ] },
  { key: 'lookgrp', label: 'Look (particles or flame)', type: 'section', fields: [
    { key: 'look', label: 'Look', type: 'enum', def: 'plain', anim: false,
      options: [['plain', 'Particles (as they are)'], ['flame', 'Flame (they melt into fire)'], ['smoke', 'Smoke (torn into wisps)'], ['water', 'Water (they melt into a liquid)'],
        ['gas', 'Gas (simulated smoke and fire)'], ['liquid', 'Liquid (simulated water)']],
      help: 'Flame: the color comes from the heat that piles up, and rising noise cuts its edges into tongues. Gas: the particles are not drawn, they feed a simulated gas (smoke, heat, fuel) that then moves by itself' },
    { key: 'sim', label: 'Simulated on', type: 'enum', def: '', anim: false, showIf: (c) => c.look === 'gas' || c.look === 'liquid',
      options: [['', 'Auto (gas: GPU; liquid: CPU)'], ['gpu', 'GPU'], ['cpu', 'CPU']],
      help: 'Where the gas or the liquid is simulated. Auto: the gas on the GPU (on the CPU when rendering on the CPU), the liquid on the CPU, which works only where the liquid is and is usually as fast or faster. Both give the same kind of motion, but not exactly the same: changing it simulates it again' },
    { key: 'walls', label: 'Closed frame edges', type: 'bool', anim: false, showIf: (c) => c.look === 'gas' || c.look === 'liquid',
      help: 'The edges of the frame are walls (sides, floor and ceiling): the gas or the liquid stays in, as in a fish tank, instead of leaving' },
    { key: 'gas', label: 'Gas', type: 'group', showIf: (c) => c.look === 'gas', fields: [
      { key: 'detail', label: 'Detail (cell px)', type: 'num', min: 2, max: 32, step: 0.5, def: 7, anim: false,
        help: 'The size of the simulation\'s cells: smaller is finer but slower. The particles\' fine wisps are added on top' },
      { key: 'speed', label: 'Speed', type: 'num', min: 0.05, max: 8, step: 0.05, def: 1,
        help: 'How fast the gas lives: 2 = twice as fast, 0.5 = slow motion (its particles keep their pace). Faster costs more steps' },
      { key: 'smoke', label: 'Smoke per particle', type: 'num', min: 0, max: 5, step: 0.005, def: 0.2, help: 'Smoke each particle gives off per second (its color: the particles\')' },
      { key: 'heat', label: 'Heat per particle', type: 'num', min: 0, max: 5, step: 0.005, def: 0.4, help: 'Heat each particle gives off per second: hot gas rises (and glows when very hot)' },
      { key: 'fuel', label: 'Fuel per particle', type: 'num', min: 0, max: 5, step: 0.005, def: 0, help: 'Fuel each particle gives off per second: it burns as it rises, into flames (heat) and soot' },
      { key: 'push', label: 'Push', type: 'num', min: 0, max: 1, step: 0.01, def: 0.1, help: 'How much the particles drag the gas their way (their launch)' },
      { key: 'rise', label: 'Rise (px/s²)', type: 'num', min: -2000, max: 3000, step: 1, def: 250, help: 'How fast hot gas rises (per unit of heat)' },
      { key: 'weight', label: 'Weight (px/s²)', type: 'num', min: -2000, max: 2000, step: 1, def: 0, help: 'Smoke sinks (per unit of smoke): heavy, cold smoke; negative floats' },
      { key: 'swirl', label: 'Swirls', type: 'num', min: 0, max: 6, step: 0.01, def: 2.5, help: 'Keeps its curls alive: more, more rolling whirls' },
      { key: 'wind', label: 'Wind (px/s)', type: 'num', min: -2000, max: 2000, step: 1, def: 0, help: 'The air\'s flow, positive to the right' },
      { key: 'turbulence', label: 'Turbulence (px/s)', type: 'num', min: 0, max: 600, step: 1, def: 120, help: 'How fast the air\'s gusts are: each whirl lives about as long as it takes to turn (its size / this speed)' },
      { key: 'turb_scale', label: 'Turbulence size (px)', type: 'num', min: 10, max: 2000, step: 1, def: 140 },
      { key: 'fade', label: 'Smoke vanishes (1/s)', type: 'num', min: 0, max: 5, step: 0.01, def: 0.12 },
      { key: 'cool', label: 'Heat lost (1/s)', type: 'num', min: 0, max: 20, step: 0.01, def: 3, help: 'More: the flames are shorter' },
      { key: 'burn', label: 'Burns (1/s)', type: 'num', min: 0, max: 20, step: 0.05, def: 2.5, help: 'How fast the fuel burns: more, the fire is nearer the source' },
      { key: 'soot', label: 'Soot', type: 'num', min: 0, max: 4, step: 0.01, def: 0.6, help: 'Smoke made by the fuel that burns' },
      { key: 'soot_color', label: 'Soot color', type: 'color', def: '#2a2623' },
      { key: 'expand', label: 'Takes room', type: 'num', min: 0, max: 10, step: 0.05, def: 0.8,
        help: 'How much room the new gas takes: it pushes out from its source. Under a ceiling it fills down until it spills out of the openings. 0: none' },
      { key: 'spread', label: 'Spreads out (px²/s)', type: 'num', min: 0, max: 3000, step: 5, def: 300, help: 'How much the smoke spreads out by itself: fuller and softer; 0 = thin, sharp wisps' },
      { key: 'opacity', label: 'Thickness', type: 'num', min: 0, max: 10, step: 0.05, def: 1.6, help: 'How opaque the smoke looks' },
      { key: 'wisps', label: 'Wisps', type: 'num', min: 0, max: 1, step: 0.01, def: 0.4, help: 'Fine detail carried by the flow that tears its edges' },
      { key: 'wisp_scale', label: 'Wisp size (px)', type: 'num', min: 4, max: 400, step: 1, def: 60 },
      { key: 'light_angle', label: 'Light from (°)', type: 'num', min: -180, max: 180, step: 1, def: 0, help: '0 = from above, 90 = from the right: the far side of the smoke is in its own shadow' },
      { key: 'light_color', label: 'Light color', type: 'color', def: '#fff2e0' },
      { key: 'shadow_color', label: 'Shadow color', type: 'color', def: '#66707e', help: 'The light from around (the sky) that its shadows get' },
      { key: 'shadow', label: 'Own shadow', type: 'num', min: 0, max: 3, step: 0.01, def: 0.6, help: 'How dark the smoke gets where the light has gone through a lot of it' },
      { key: 'haze', label: 'Heat haze (px)', type: 'num', min: 0, max: 60, step: 0.5, def: 0,
        help: 'Where the air is warm, what is behind it shimmers, moved up to this many px (0: none). It works even where there is no smoke' },
      { key: 'haze_scale', label: 'Haze size (px)', type: 'num', min: 4, max: 200, step: 1, def: 30, showIf: (c) => c.haze > 0,
        help: 'The size of its ripples: smaller, a finer shimmer' },
      { key: 'fire_light', label: 'Fire lights it', type: 'num', min: 0, max: 4, step: 0.01, def: 1, help: 'How much its flames light the smoke around them (the flame\'s body color)' },
    ] },
    { key: 'flame', label: 'Flame', type: 'group', showIf: (c) => c.look === 'flame' || c.look === 'gas', fields: [
      { key: 'color_cool', label: 'Color of its edges', type: 'color' },
      { key: 'color_mid', label: 'Color of its body', type: 'color' },
      { key: 'color_hot', label: 'Color of its core', type: 'color' },
      { key: 'heat', label: 'Heat', type: 'num', min: 0.05, max: 6, step: 0.05, help: 'More: it gets white-hot sooner' },
      { key: 'noise', label: 'Tongues (px)', type: 'num', min: 0, max: 120, step: 0.5, help: 'How far the rising noise moves it' },
      { key: 'noise_scale', label: 'Whirl size (px)', type: 'num', min: 4, max: 400, step: 1 },
      { key: 'noise_speed', label: 'Rising speed (px/s)', type: 'num', min: 0, max: 1500, step: 5 },
      { key: 'edge', label: 'Eaten edges', type: 'num', min: 0, max: 1, step: 0.01 },
    ] },
    { key: 'smoke', label: 'Smoke', type: 'group', showIf: (c) => c.look === 'smoke', fields: [
      { key: 'wisp', label: 'Wisps', type: 'num', min: 0, max: 1, step: 0.01, def: 0.7, help: 'How much the noise tears it' },
      { key: 'scale', label: 'Wisp size (px)', type: 'num', min: 4, max: 600, step: 1, def: 70 },
      { key: 'speed', label: 'Rising speed (px/s)', type: 'num', min: 0, max: 600, step: 1, def: 30 },
      { key: 'warp', label: 'Swirl (px)', type: 'num', min: 0, max: 200, step: 0.5, def: 25, help: 'How far the noise moves it' },
    ] },
    { key: 'liquid', label: 'Liquid', type: 'group', showIf: (c) => c.look === 'liquid', fields: [
      { key: 'detail', label: 'Detail (cell px)', type: 'num', min: 2, max: 32, step: 0.5, def: 6, anim: false,
        help: 'The size of the simulation\'s cells: smaller is finer but slower' },
      { key: 'splash', label: 'Splash', type: 'num', min: 0, max: 1, step: 0.01, def: 0.95,
        help: 'How lively it is: 1 splashes and keeps every ripple, 0 calm and thick, like honey' },
      { key: 'speed', label: 'Speed', type: 'num', min: 0.05, max: 8, step: 0.05, def: 1,
        help: 'How fast it lives: 2 = twice as fast, 0.5 = slow motion' },
      { key: 'gravity_x', label: 'Gravity sideways (px/s²)', type: 'num', min: -5000, max: 5000, step: 1, def: 0,
        help: 'Added to the emitter\'s gravity: positive pulls it to the right (with the gravity down, it falls at an angle)' },
      { key: 'd3', label: '3D (with the scene\'s depth)', type: 'bool', anim: false,
        help: 'With a depth map: the drops move in the scene\'s depth. They fall onto what they meet and roll, slide and drip off it in 3D (forwards, backwards, to the sides), as its surface slopes' },
      { key: 'gravity_z', label: 'Gravity in depth (px/s²)', type: 'num', min: -5000, max: 5000, step: 1, def: 0, showIf: (c) => c.d3,
        help: 'Positive pulls it into the scene (away from you), negative towards you. The scene\'s whole depth is about 800 px' },
      { key: 'thickness', label: 'Things\' thickness', type: 'num', min: 0.01, max: 1, step: 0.01, def: 1, showIf: (c) => c.d3,
        help: 'How deep things are behind the surface you see (part of the scene\'s depth): 1 = solid all the way back. Less: the drops can pass behind them' },
      { key: 'back_slope', label: 'Their back\'s slope', type: 'num', min: 0, max: 1, step: 0.01, def: 1, showIf: (c) => c.d3,
        help: 'How the hidden back of things slopes: 1 = a vertical wall (what passes behind falls freely), 0 = a ledge it rolls on' },
      { key: 'bounce', label: 'Bounce', type: 'num', min: 0, max: 1, step: 0.01, def: 0.1, showIf: (c) => c.d3,
        help: 'How much of its speed into a surface it keeps bouncing off it' },
      { key: 'friction', label: 'Friction', type: 'num', min: 0, max: 1, step: 0.01, def: 0.15, showIf: (c) => c.d3,
        help: 'How much speed it loses sliding along a surface' },
    ] },
    { key: 'water', label: 'Water', type: 'group', showIf: (c) => c.look === 'water' || c.look === 'liquid', fields: [
      { key: 'threshold', label: 'Where it becomes liquid', type: 'num', min: 0.05, max: 2, step: 0.01, def: 0.5,
        help: 'How much of the particles must pile up: less, fatter water that joins sooner; more, thinner and in drops' },
      { key: 'clear', label: 'See-through', type: 'num', min: 0, max: 1, step: 0.01, def: 0.7, help: 'How much you see through its body (its edges stay)' },
      { key: 'shine', label: 'Shine', type: 'num', min: 0, max: 3, step: 0.01, def: 1, help: 'Its highlights' },
      { key: 'bump', label: 'Rounded', type: 'num', min: 0, max: 3, step: 0.01, def: 1, help: 'How round and deep its surface looks' },
      { key: 'ripple', label: 'Ripples (px)', type: 'num', min: 0, max: 40, step: 0.1, def: 3, help: 'How far the ripples move it' },
      { key: 'ripple_scale', label: 'Ripple size (px)', type: 'num', min: 4, max: 400, step: 1, def: 40 },
      { key: 'ripple_speed', label: 'Ripple speed (px/s)', type: 'num', min: 0, max: 600, step: 1, def: 50 },
      { key: 'lens', label: 'Lens (px)', type: 'num', min: 0, max: 200, step: 0.5, def: 0,
        help: 'What is behind it is seen through it, bent like through glass: the drops magnify it (and turn it upside down when strong). 0 = none' },
    ] },
  ] },
  { key: 'light', label: 'Light', type: 'group', fields: [
    { key: 'enabled', label: 'Lights what is behind', type: 'bool', help: 'The background and the layers below it, from where its particles are' },
    { key: 'intensity', label: 'Strength', type: 'num', min: 0, max: 4, step: 0.01, showIf: (c) => c.enabled },
    { key: 'reach', label: 'Reach (px)', type: 'num', min: 20, max: 3000, step: 5, showIf: (c) => c.enabled, help: 'Where it is still strong (or drag the handle of its circle)' },
    { key: 'end', label: 'Fades out at (px)', type: 'num', min: 0, max: 8000, step: 5, showIf: (c) => c.enabled, help: '0 = 2.5 × the reach (or drag the handle of the outer circle)' },
    { key: 'color', label: 'Color', type: 'color', optional: true, showIf: (c) => c.enabled, help: 'Empty: the particles\' own (the flame\'s body)' },
    { key: 'mode', label: 'How it lights', type: 'enum', def: 'soft', anim: false, showIf: (c) => c.enabled,
      options: [['soft', 'Soft (lights and tints what is there)'], ['add', 'Add (brighter, like neon)']] },
    { key: 'flicker', label: 'Flicker', type: 'num', min: 0, max: 1, step: 0.01, showIf: (c) => c.enabled },
    { key: 'depth', label: 'Fades with depth', type: 'num', min: 0, max: 10, step: 0.1, showIf: (c) => c.enabled,
      help: 'With a background that has depth: how fast it fades away from its own depth (0 = flat, as in 2D)' },
    { key: 'shade', label: 'Surfaces shading', type: 'num', min: 0, max: 1, step: 0.01, showIf: (c) => c.enabled,
      help: 'With depth: the side of the surfaces that faces it gets more light' },
    { key: 'gloss', label: 'Shine', type: 'num', min: 0, max: 1, step: 0.01, def: 0, showIf: (c) => c.enabled,
      help: 'With depth: highlights on the surfaces that face it (wet skin, metal); more = brighter and sharper' },
    { key: 'shadows', label: 'Shadows', type: 'num', min: 0, max: 1, step: 0.01, def: 0, showIf: (c) => c.enabled,
      help: 'With depth: what is nearer and stands between the light and a surface casts a shadow on it' },
    { key: 'from', label: 'Lights from (near)', type: 'num', min: 0, max: 1, step: 0.005, def: 1, showIf: (c) => c.enabled, adjust: 'lightrange',
      help: 'With depth: what is nearer than this is in front of it and is not lit (1 = white in the depth map, the nearest); above 1 = in front of it)' },
    { key: 'to', label: 'Lights up to (far)', type: 'num', min: 0, max: 1, step: 0.005, def: 0, showIf: (c) => c.enabled, adjust: 'lightrange',
      help: 'What is farther than this is too far and is not lit (0 = black in the depth map, the farthest; below 0 = behind it, so even the farthest gets all the light with a soft edge)' },
    { key: 'range_soft', label: 'Soft edge of that', type: 'num', min: 0, max: 0.5, step: 0.005, def: 0.05, showIf: (c) => c.enabled, adjust: 'lightrange',
      help: 'While you adjust these three, the preview shows the depth map with what is not lit in black' },
    { key: 'rim', label: 'Only the edges, far away', type: 'bool', anim: false, showIf: (c) => c.enabled,
      help: 'With depth: farther than a circle it no longer lights the surfaces, only the edges of things that face it (rim light)' },
    { key: 'rim_from', label: 'Edges from (px)', type: 'num', min: 0, max: 6000, step: 5, def: 500, showIf: (c) => c.enabled && c.rim,
      help: 'Farther than this only the edges (or drag the handle of its circle; 0 = edges everywhere). Up to where the light fades out' },
    { key: 'rim_soft', label: 'Change over (px)', type: 'num', min: 0, max: 2000, step: 5, def: 120, showIf: (c) => c.enabled && c.rim },
    { key: 'rim_width', label: 'Edge width (px)', type: 'num', min: 1, max: 80, step: 0.5, def: 10, showIf: (c) => c.enabled && c.rim },
    { key: 'rim_gain', label: 'Edge strength', type: 'num', min: 0, max: 10, step: 0.05, def: 2, showIf: (c) => c.enabled && c.rim },
    { key: 'rim_edge', label: 'What counts as an edge', type: 'num', min: 0.005, max: 0.4, step: 0.005, def: 0.04, showIf: (c) => c.enabled && c.rim,
      help: 'How much the surface must turn away from you to be an edge: less = more edges (even small bumps)' },
  ] },
  { key: 'glow', label: 'Glow', type: 'group', fields: [
    { key: 'intensity', label: 'Strength', type: 'num', min: 0, max: 4, step: 0.01, help: '0 = no glow' },
    { key: 'radius', label: 'Reach (px)', type: 'num', min: 2, max: 400, step: 1 },
    { key: 'color', label: 'Color', type: 'color', optional: true, help: 'Empty: the particles\' own color' },
  ] },
  // (edits the emitter itself: its react block, and its light's reaction)
  { key: 'react', label: 'Audio reaction', type: 'group', whole: true, fields: [
    { ...react('bass'), key: 'react.react_to' },
    { key: 'react.rate', label: 'More particles', type: 'num', min: 0, max: 10, step: 0.05, help: '1 = twice as many at full band' },
    { key: 'react.speed', label: 'Faster', type: 'num', min: 0, max: 4, step: 0.05 },
    { key: 'react.size', label: 'Bigger', type: 'num', min: 0, max: 4, step: 0.05 },
    { key: 'react.burst', label: 'Burst on each beat', type: 'int', min: 0, max: 5000, step: 1, help: 'Particles thrown at once on every beat' },
    ...LIGHT_MUSIC.map((f) => ({ ...f, key: 'light.' + f.key, showIf: (c) => !!(c.light && c.light.enabled) })),
  ] },
] };
export const EMITTER_CLIP_FIELDS = [
  sec('place', 'Placement', [
    { key: 'x', label: 'Position X', type: 'num', min: -0.5, max: 1.5, step: 0.001, help: 'Or drag its point on the preview', showIf: (c) => !onPath(c) },
    { key: 'y', label: 'Position Y', type: 'num', min: -0.5, max: 1.5, step: 0.001, showIf: (c) => !onPath(c) },
    { key: 'rotation', label: 'Rotation (°)', type: 'num', min: -360, max: 360, step: 0.5, help: 'Turns its shape and its direction (or drag the arrow)' },
    { key: 'scale', label: 'Scale', type: 'num', min: 0.01, max: 8, step: 0.01, help: 'Sizes, speeds and shape together' },
  ]),
  sec('show', 'Opacity and blend', [
    { key: 'opacity', label: 'Opacity', type: 'num', min: 0, max: 1, step: 0.01 },
    { key: 'fade_in', label: 'Fade in (s)', type: 'num', min: 0, max: 10, step: 0.05, anim: false },
    { key: 'fade_out', label: 'Fade out (s)', type: 'num', min: 0, max: 10, step: 0.05, anim: false },
    { key: 'blend', label: 'Blend', type: 'enum', def: 'normal', options: BLENDS },
  ]),
  CLIP_DEPTH,
  // the emitter block, one folding group per part (each edits clip.emitter, or a block inside it)
  ...EMITTER_BLOCK.fields.flatMap((f) => {
    if (f.whole) return [{ ...f, key: 'emitter', id: 'em_' + f.key }]; // (its fields: paths in the emitter)
    if (f.type !== 'section') return [{ ...f, key: 'emitter.' + f.key, id: 'em_' + f.key }];
    const own = f.fields.filter((x) => x.type !== 'group');
    const subs = f.fields.filter((x) => x.type === 'group').map((x) => ({ ...x, key: 'emitter.' + x.key, id: 'em_' + x.key,
      showIf: x.showIf && ((c) => x.showIf(c.emitter || {})) }));
    return [{ key: 'emitter', id: 'em_' + f.key, label: f.label, type: 'group', fields: own }, ...subs];
  }),
  ...CLIP_FIELDS.filter((f) => f.key === 'filters' || f.key === 'effects'),
];
