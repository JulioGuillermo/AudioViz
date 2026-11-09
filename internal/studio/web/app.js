import { html, render, useState, useEffect, useRef } from './vendor/preact-htm.js';
import { TRACKS, ZONES, zoneOf, AUDIO_FIELDS, OUTPUT_FIELDS, trackById, TRANSITIONS, SIMPLE_TRANSITIONS, DIRECTED, DIRECTIONS, TEXT_FIELDS } from './schema.js';
import { api, clone, fmtTime, parseTime, b64u8, basename, debounce, setClient } from './util.js';
import { Form, Select, Edit, Section, adjusting } from './fields.js';
import { Icon } from './icons.js';
import { Timeline, segSummary } from './timeline.js';
import { keyApi, valueAt, setIn, KEY_EPS } from './keys.js';
import { editKeysTimes } from './keymarks.js';
import { useFrameStream } from './frames.js';
import { Editor } from './editor.js';
import { maskShapes, setMaskShapes, MaskSection, segMaskOwner, editSegMasks } from './masks.js';
import { MotionSection, NodeSection, setPath, setRoute } from './motion.js';
import { HelpDialog, helpMenu, helpItem } from './help.js';
import { ExprDialog, exprLib, pathKeyNames, LibrarySection } from './exprs.js';
import { DepthSection } from './depth.js';
import { StemsSection } from './stems.js';
import { isEmitterLayer, newEmitterClip, emitterMenuItems, EmitterHandles } from './emitters.js';
import { ContextMenu, openMenu } from './menu.js';
import { LayerInspector, ClipHandles, TextHandles, textBoxOf, geoOf, frontFirst, layerName, newLayerId, setClipProps, duplicateClip } from './layers.js';
import { ImagesDialog } from './images.js';
import { FilePicker, NewProject, ExportDialog, GenLyricsDialog, LyricsTextDialog, LYRIC_LANGS, STEP_LABEL, ProjectsDialog, PackageDialog, ExportsDialog, WorkspaceGrid, uploadPackage } from './dialogs.js';
import { createStore, useStore, useThrottledStore } from './store.js';
import { t, setLang, langStore, LANGS } from './i18n.js';
import { createScrubber } from './scrub.js';
import { diff, applyOps, transformPair, deepEqual, me as whoAmI, saveMe } from './collab.js';
import { PeersBar, RemoteCursors, peerCursors, peerMarks } from './peers.js';
import { ChatPanel, chatStore, loadChat, chatEvent, unreadCount, notifyMsg } from './chat.js';
import { AgentChanges } from './review.js';
import { ComfySection, ComfyRunDialog, loadFlows, flowsStore } from './comfy.js';
import { PluginsSection } from './plugins.js';
import { BindSection, BIND_TRACKS } from './bind.js';
import { ModelsSection } from './models.js';

// ------------------------------------------------------------------ preview

// Fast forward / rewind while it is held down (pointer captured: it ends on release anywhere)
function HoldBtn({ dir, speed, start, stop }) {
  const down = (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    try { e.currentTarget.setPointerCapture(e.pointerId); } catch (err) { /* ignore */ }
    start(dir, speed);
  };
  const tip = dir > 0 ? t('Fast forward x{n} while held (L; Shift+L: x3)', { n: speed }) : t('Rewind x{n} while held (J; Shift+J: x3)', { n: speed });
  return html`<button class="btn icon shuttle" title=${tip} onPointerDown=${down} onPointerUp=${stop} onPointerCancel=${stop} onLostPointerCapture=${stop}
    onContextMenu=${(e) => e.preventDefault()}>
    <${Icon} name=${dir > 0 ? 'fast-forward' : 'rewind'} size=${14} /><small>${speed}</small></button>`;
}
function Preview({ meta, timeStore, scale, rev, playing, pin, onPin, points, onPoints, clip, clipRef, onClip, text, textIdx, onText, emitter, view, cursors = [], onPointer }) {
  const time = useStore(timeStore);
  const canvas = useRef();
  const stage = useRef();
  // Placing a fixed text: drag on the image (pin = its position 0..1, null when no text is selected)
  const [dragPin, setDragPin] = useState(null);
  // The image always fills the available area keeping the video's aspect ratio, whatever the
  // preview quality (a 1/4 preview is scaled up instead of staying small)
  const [box, setBox] = useState(null);
  useEffect(() => {
    const el = stage.current;
    if (!el) return;
    const fitBox = () => {
      const cs = getComputedStyle(el);
      const aw = el.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);
      const ah = el.clientHeight - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom);
      const ar = meta.width / meta.height;
      const w = Math.max(1, Math.floor(Math.min(aw, ah * ar)));
      setBox({ w, h: Math.max(1, Math.floor(w / ar)) });
    };
    fitBox();
    const ro = new ResizeObserver(fitBox);
    ro.observe(el);
    return () => ro.disconnect();
  }, [meta.width, meta.height]);
  const posFrom = (e) => {
    const r = canvas.current.getBoundingClientRect();
    const c = (v) => Math.max(0, Math.min(1, v));
    return { x: c((e.clientX - r.left) / r.width), y: c((e.clientY - r.top) / r.height) };
  };
  // Points (burn ignition points): click adds one (up to 8), drag moves the nearest, Shift+click removes it
  const [dragPts, setDragPts] = useState(null);
  const ptsDown = (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const r = canvas.current.getBoundingClientRect();
    let pts = points.map((q) => ({ ...q }));
    let p = posFrom(e);
    let k = -1, best = 14;
    pts.forEach((q, j) => { const dd = Math.hypot((q.x - p.x) * r.width, (q.y - p.y) * r.height); if (dd < best) { best = dd; k = j; } });
    if (e.shiftKey) { if (k >= 0) { pts.splice(k, 1); onPoints(pts); } return; }
    if (k < 0) { if (pts.length >= 8) return; pts.push(p); k = pts.length - 1; }
    setDragPts(pts);
    const mv = (ev) => { p = posFrom(ev); pts = pts.map((q, j) => (j === k ? p : q)); setDragPts(pts); };
    const up = () => {
      removeEventListener('mousemove', mv); removeEventListener('mouseup', up);
      setDragPts(null);
      onPoints(pts.map((q) => ({ x: +q.x.toFixed(4), y: +q.y.toFixed(4) })));
    };
    addEventListener('mousemove', mv); addEventListener('mouseup', up);
  };
  const pinDown = (e) => {
    if (points) { ptsDown(e); return; }
    if (!pin || e.button !== 0) return;
    e.preventDefault();
    let p = posFrom(e);
    setDragPin(p);
    const mv = (ev) => { p = posFrom(ev); setDragPin(p); };
    const up = () => {
      removeEventListener('mousemove', mv); removeEventListener('mouseup', up);
      setDragPin(null);
      onPin(+p.x.toFixed(4), +p.y.toFixed(4));
    };
    addEventListener('mousemove', mv); addEventListener('mouseup', up);
  };
  let ptsStyle = [];
  const shownPts = dragPts || points;
  if (shownPts && canvas.current && stage.current) {
    const cr = canvas.current.getBoundingClientRect(), sr = stage.current.getBoundingClientRect();
    ptsStyle = shownPts.map((q) => ({ left: cr.left - sr.left + q.x * cr.width + 'px', top: cr.top - sr.top + q.y * cr.height + 'px' }));
  }
  const info = useFrameStream(canvas, { meta, time, scale, rev, playing, view });
  // The selected text on screen: handles; otherwise (not shown now) the pin places it
  const tbox = text && textIdx != null ? textBoxOf(info.texts, textIdx) : null;
  if (tbox) pin = null;
  let pinStyle = null;
  const shownPin = dragPin || pin;
  if (shownPin && canvas.current && stage.current) {
    const cr = canvas.current.getBoundingClientRect(), sr = stage.current.getBoundingClientRect();
    pinStyle = { left: cr.left - sr.left + shownPin.x * cr.width + 'px', top: cr.top - sr.top + shownPin.y * cr.height + 'px' };
  }

  // my pointer on the frame (0..1), for the others; theirs drawn over it
  const pointer = (e) => {
    if (!onPointer || !canvas.current) return;
    const r = canvas.current.getBoundingClientRect();
    onPointer({ area: 'preview', x: +((e.clientX - r.left) / r.width).toFixed(4), y: +((e.clientY - r.top) / r.height).toFixed(4) });
  };
  const placeCursor = (c) => {
    if (!canvas.current || !stage.current) return null;
    const cr = canvas.current.getBoundingClientRect(), sr = stage.current.getBoundingClientRect();
    return { x: cr.left - sr.left + c.x * cr.width, y: cr.top - sr.top + c.y * cr.height };
  };
  return html`<div class="stage" ref=${stage} onMouseMove=${pointer} onMouseLeave=${() => onPointer && onPointer(null)}>
    <${RemoteCursors} list=${cursors} place=${placeCursor} />
    <canvas ref=${canvas} width=${Math.round(meta.width / scale)} height=${Math.round(meta.height / scale)}
      style=${{ width: box ? box.w + 'px' : '', height: box ? box.h + 'px' : '', cursor: pin || points ? 'crosshair' : '' }} onMouseDown=${pinDown}
      title=${points ? t('Click to add an ignition point, drag one to move it, Shift+click to remove it') : pin ? t('Drag to place the selected text') : ''}></canvas>
    ${pinStyle && html`<div class=${'pin' + (dragPin ? ' drag' : '')} style=${pinStyle}></div>`}
    ${clip && canvas.current && stage.current && (() => {
      // frame px → stage px: the canvas' place in the stage and its scale
      const cr = canvas.current.getBoundingClientRect(), sr = stage.current.getBoundingClientRect();
      const k = cr.width / meta.width;
      return html`<${emitter ? EmitterHandles : ClipHandles} clip=${clip} time=${time} meta=${meta} k=${k} ox=${cr.left - sr.left} oy=${cr.top - sr.top}
        geo=${clipRef && geoOf(info.layers, clipRef.id, clipRef.clip)}
        toFrame=${(cx, cy) => ({ x: (cx - cr.left) / k, y: (cy - cr.top) / k })} onChange=${onClip} />`;
    })()}
    ${tbox && canvas.current && stage.current && (() => {
      const cr = canvas.current.getBoundingClientRect(), sr = stage.current.getBoundingClientRect();
      const k = cr.width / meta.width;
      return html`<${TextHandles} text=${text} box=${tbox} time=${time} meta=${meta} k=${k} ox=${cr.left - sr.left} oy=${cr.top - sr.top}
        toFrame=${(cx, cy) => ({ x: (cx - cr.left) / k, y: (cy - cr.top) / k })} onChange=${onText} />`;
    })()}
    ${ptsStyle.map((st, j) => html`<div class="pin fire" style=${st}><span>${j + 1}</span></div>`)}
    <div class="hud">${meta.width}×${meta.height} · 1/${scale} · ${info.ms} ms${playing ? ' · ' + info.fps + ' fps' : ''}</div>
  </div>`;
}

function Clock({ timeStore, meta }) {
  const time = useStore(timeStore);
  return html`<span class="time">${fmtTime(time)} <small>/ ${fmtTime(meta.duration)} · ${t('frame')} ${Math.round(time * meta.fps)}</small></span>`;
}

// ------------------------------------------------------------------ getting started

const EXT = {
  audio: ['flac', 'wav', 'mp3', 'ogg'],
  bg: ['png', 'jpg', 'jpeg', 'webp', 'bmp', 'gif', 'mp4', 'mov', 'mkv', 'webm', 'avi', 'm4v'],
  lyrics: ['lrc', 'srt'],
  txt: ['txt'],
  font: ['ttf', 'otf'],
};
function kindOf(name) {
  const ext = String(name).split('.').pop().toLowerCase();
  for (const k in EXT) if (EXT[k].includes(ext)) return k;
  return null;
}

// Guide over the preview while the audio or the background is missing
function Setup({ proj, ctx, onHide }) {
  const lyr = proj.tracks.lyrics[0].cfg;
  const steps = [
    { done: !!proj.audio.path, title: 'Choose the song', text: 'FLAC, WAV, MP3 or OGG. It is analyzed right away: bass, mids, treble and beats.',
      btn: 'Choose audio…', act: () => ctx.pickFile('audio', ctx.setAudio) },
    { done: !!proj.tracks.background[0].cfg.path, title: 'Choose the background', text: 'Image, GIF or video. The video resolution will be the background’s (it can be changed in Output).',
      btn: 'Choose background…', act: () => ctx.pickFile('bg', ctx.setBackground) },
    { done: !!lyr.path, optional: true, title: 'Add the lyrics', text: 'An already synced .lrc / .srt, or generate it: from a .txt with the lyrics, or transcribed from the song.',
      btn: 'Choose .lrc / .srt…', act: () => ctx.pickFile('lyrics', ctx.setLyrics),
      btn2: 'Generate…', act2: () => ctx.dialog({ type: 'genlyrics' }) },
  ];
  return html`<div class="setup">
    <div class="setup-h"><span>${t('Getting started')}</span><span class="spacer"></span>
      <button class="btn sm icon ghost" title=${t('Hide')} onClick=${onHide}><${Icon} name="x" size=${14} /></button></div>
    ${steps.map((st, i) => html`<div class=${'step' + (st.done ? ' done' : '')}>
      <div class="num">${st.done ? html`<${Icon} name="check" size=${14} />` : i + 1}</div>
      <div class="grow">
        <div class="step-t">${t(st.title)}${st.optional && html`<span class="faint"> · ${t('optional')}</span>`}</div>
        <div class="step-d">${t(st.text)}</div>
        <div class="row" style="margin-top:8px">
          <button class=${'btn sm' + (!st.done && !st.optional ? ' primary' : '')} onClick=${st.act}><${Icon} name="folder-open" size=${14} /> ${st.done ? t('Change…') : t(st.btn)}</button>
          ${st.btn2 && html`<button class="btn sm" onClick=${st.act2}><${Icon} name="wand-sparkles" size=${14} /> ${t(st.btn2)}</button>`}
        </div>
      </div>
    </div>`)}
    <div class="setup-f"><${Icon} name="upload" size=${14} /><span>${t('You can also drop files onto the window: they are copied to assets/ and placed automatically.')}</span></div>
  </div>`;
}

// ------------------------------------------------------------------ inspector

function SegHeader({ segs, i, set, meta, track, ctx }) {
  const s = segs[i];
  const name = html`<div class="fld"><label>${t('Name')}</label><div class="ctl"><${Edit} type="text" class="grow" value=${s.label}
      placeholder=${i === 0 ? t('Optional') : t('e.g. Chorus')} onChange=${(e) => set({ ...s, label: e.target.value })} /></div></div>`;
  if (i === 0) return html`<div class="card">${name}<div class="card-note">${t('First segment: applies from the start until the next change.')}</div></div>`;
  return html`<div class="card">
    ${name}
    <div class="fld"><label>${t('Starts at')}</label><div class="ctl">
      <${Edit} type="text" class="mono" style="width:96px" value=${fmtTime(s.at)}
        onChange=${(e) => set({ ...s, at: Math.max(0.01, Math.min(meta.duration - 0.01, parseTime(e.target.value))) })} />
      <span class="faint" style="font-size:11.5px">min:sec</span></div></div>
    <div class="fld"><label>${t('Transition')}</label><div class="ctl">
      <${Select} value=${track === 'background' || s.transition.mode === 'cut' ? s.transition.mode : 'fade'}
        options=${track === 'background' ? TRANSITIONS : SIMPLE_TRANSITIONS}
        onChange=${(m) => set({ ...s, transition: { ...s.transition, mode: m, duration: s.transition.duration || 1 } })} />
      ${s.transition.mode !== 'cut' && html`<${Edit} type="number" step="0.1" min="0.05" value=${s.transition.duration} title=${t('Duration (s)')}
        onChange=${(e) => set({ ...s, transition: { ...s.transition, duration: Math.max(0.05, +e.target.value) } })} /><span class="faint">s</span>`}
    </div></div>
    ${track === 'background' && DIRECTED.includes(s.transition.mode) && html`<div class="fld"><label>${t('Direction')}</label><div class="ctl">
      <${Select} value=${s.transition.direction || 'left'} options=${DIRECTIONS}
        onChange=${(d) => set({ ...s, transition: { ...s.transition, direction: d } })} /></div></div>`}
    ${track === 'background' && s.transition.mode === 'burn' && html`<${BurnOptions} s=${s} set=${set} ctx=${ctx} />`}
    ${track === 'background' && (s.transition.mode === 'fluid' || s.transition.mode === 'liquid') && html`<${FluidOptions} s=${s} set=${set} ctx=${ctx} />`}
  </div>`;
}

// Options of the fluid transition (it is simulated once, the first time it is shown)
function FluidOptions({ s, set, ctx }) {
  const f = s.transition.fluid || {};
  const upd = (k, v) => set({ ...s, transition: { ...s.transition, fluid: { ...f, [k]: v } } });
  const slider = (k, label, def, title) => html`<div class="fld"><label title=${title || ''}>${t(label)}</label><div class="ctl">
    <input type="range" min="0.05" max="1" step="0.05" value=${f[k] || def} onInput=${(e) => upd(k, +e.target.value)} />
    <span class="mono faint" style="font-size:11px;width:30px;text-align:right">${(f[k] || def).toFixed(2)}</span></div></div>`;
  return html`<div class="burnopts" onContextMenu=${helpMenu(t('Transition'), 'The flow is simulated the first time the transition is shown (a few seconds); then it plays smoothly.')}>
    ${slider('color_weight', 'Color seeking', s.transition.mode === 'liquid' ? 0.3 : 0.6, t('Low: each part flows nearby. High: it travels to the most similar color of the new image'))}
    ${s.transition.mode === 'liquid' && slider('viscosity', 'Viscosity', 0.6, t('How much neighbors move together: high, thick streams; low, more like separate drops'))}
    ${slider('interaction', 'Interaction', 0.5, t('How solid the particles are: high, they push each other hard; low, they slip past each other'))}
    ${slider('stagger', 'Stagger', 0.5, t('How spread out in time the departures are'))}
    <div style="display:flex;flex-direction:column;gap:6px;margin-top:4px">
      <button class="btn sm" style="justify-content:flex-start" title=${t('Another flow with the same settings')} onClick=${() => upd('seed', 1 + Math.floor(Math.random() * 999999))}><${Icon} name="shuffle" size=${13} /> ${t('Other variation')}</button>
      <button class="btn sm" style="justify-content:flex-start" onClick=${() => ctx.seek(s.at + s.transition.duration * 0.5)}><${Icon} name="play" size=${13} /> ${t('Go to the middle of the transition')}</button>
    </div>
  </div>`;
}

// Options of the burn transition: where the fire starts and how the burning edge looks
function BurnOptions({ s, set, ctx }) {
  const b = s.transition.burn || {};
  const upd = (k, v) => set({ ...s, transition: { ...s.transition, burn: { ...b, [k]: v } } });
  const origins = b.origins || 'random';
  const pts = b.points || [];
  const slider = (k, label, def, title) => html`<div class="fld"><label title=${title || ''}>${t(label)}</label><div class="ctl">
    <input type="range" min="0.05" max="1" step="0.05" value=${b[k] || def} onInput=${(e) => upd(k, +e.target.value)} />
    <span class="mono faint" style="font-size:11px;width:30px;text-align:right">${(b[k] || def).toFixed(2)}</span></div></div>`;
  return html`<div class="burnopts" onContextMenu=${helpMenu(t('Ignition points'), 'Click on the preview to add a point (up to 8), drag one to move it, Shift+click to remove it.')}>
    <div class="fld"><label>${t('Fire starts at')}</label><div class="ctl">
      <${Select} value=${origins} options=${[['random', 'Random points'], ['points', 'Points I choose']]} onChange=${(v) => upd('origins', v)} /></div></div>
    ${origins === 'random' ? html`
      <div class="fld"><label>${t('Ignition points')}</label><div class="ctl">
        <input type="number" min="1" max="8" step="1" value=${b.count || 3} onInput=${(e) => upd('count', Math.max(1, Math.min(8, +e.target.value || 3)))} />
        <button class="btn sm icon" title=${t('Other random points')} onClick=${() => upd('seed', 1 + Math.floor(Math.random() * 999999))}><${Icon} name="shuffle" size=${13} /></button>
      </div></div>` : html`
      <div class="row" style="flex-wrap:wrap;gap:4px;margin-bottom:6px">
        ${pts.map((q, j) => html`<span class="chip">${j + 1}<span class="mono faint">${q[0].toFixed(2)}, ${q[1].toFixed(2)}</span>
          <button class="btn icon sm ghost" title=${t('Remove')} onClick=${() => upd('points', pts.filter((_, n) => n !== j))}><${Icon} name="x" size=${11} /></button></span>`)}
        ${pts.length === 0 ? html`<span class="faint" style="font-size:11.5px">${t('No points: the fire starts at the center.')}</span>`
          : html`<button class="btn sm ghost" onClick=${() => upd('points', [])}>${t('Clear')}</button>`}
      </div>`}
    ${slider('edge', 'Burnt edge', 0.5, t('Width of the charred edge and the ember'))}
    ${slider('roughness', 'Irregularity', 0.5, t('How irregular the fire front is'))}
    <div class="fld"><label>${t('Ember color')}</label><div class="ctl">
      <input type="color" value=${b.color || '#ff7a1f'} onInput=${(e) => upd('color', e.target.value)} /></div></div>
    <button class="btn sm" style="margin-top:4px" onClick=${() => ctx.seek(s.at + s.transition.duration * 0.45)}>
      <${Icon} name="play" size=${13} /> ${t('Go to the middle of the transition')}</button>
  </div>`;
}

// Fixed texts (titles): list, time range and the lyrics style options, placed by hand
function TextsInspector({ proj, sel, setSel, update, time, meta, ctx }) {
  const texts = proj.texts;
  const i = sel.i;
  const x = texts[i];
  const set = (nx) => update((p) => { p.texts[i] = nx; });
  const clampT = (v) => Math.max(0, Math.min(meta.duration, v));
  const name = (tx, k) => tx.label || (tx.text || '').split('\n')[0].slice(0, 22) || t('Text {n}', { n: k + 1 });
  return html`<div>
    <div class="insp-h">
      <div class="insp-title"><${Icon} name="type" size=${16} cls="muted" />${t('Texts')}<span class="sub">${t('Titles and fixed texts')}</span></div>
      ${texts.length > 0 && html`<div class="segtabs">${texts.map((tx, k) => html`<button class=${'segtab' + (k === i ? ' on' : '')} title=${tx.text}
          onClick=${() => ctx.selectText(k)}><span class="mono">${fmtTime(tx.start, false)}</span><span class="nm">${name(tx, k)}</span>
          <span class="sum mono">${fmtTime(tx.end - tx.start, false)}</span></button>`)}</div>`}
      <div class="row">
        <button class="btn sm" onClick=${() => ctx.addText(time)} title=${t('New text starting at the cursor')}><${Icon} name="plus" size=${14} /> ${t('Text at {t}', { t: fmtTime(time) })}</button>
        <span class="spacer"></span>
        <button class="btn sm ghost danger" disabled=${!x} title=${t('Delete this text (Del)')}
          onClick=${() => { update((p) => { p.texts.splice(i, 1); }); setSel({ kind: 'text', i: Math.min(i, texts.length - 2) }); }}><${Icon} name="trash-2" size=${14} /> ${t('Delete')}</button>
      </div>
    </div>
    <div class="insp-b">
      ${!x ? html`<p class="faint" style="font-size:12px">${t('No texts yet. Add one at the cursor, or double-click the Texts lane on the timeline.')}</p>` : html`
      <div class="card" onContextMenu=${helpMenu(t('Text'), 'Drag on the preview to place it. In and out animations happen inside this time.')}>
        <div class="fld"><label>${t('Name')}</label><div class="ctl"><${Edit} type="text" class="grow" value=${x.label || ''}
          placeholder=${t('Optional')} onChange=${(e) => set({ ...x, label: e.target.value })} /></div></div>
        <div class="fld"><label>${t('Starts at')}</label><div class="ctl">
          <${Edit} type="text" class="mono" style="width:96px" value=${fmtTime(x.start)}
            onChange=${(e) => { const v = clampT(parseTime(e.target.value)); set({ ...x, start: v, end: Math.max(x.end, v + 0.1) }); }} />
          <span class="faint" style="font-size:11.5px">min:sec</span></div></div>
        <div class="fld"><label>${t('Ends at')}</label><div class="ctl">
          <${Edit} type="text" class="mono" style="width:96px" value=${fmtTime(x.end)}
            onChange=${(e) => set({ ...x, end: Math.max(x.start + 0.1, clampT(parseTime(e.target.value))) })} />
          <span class="faint" style="font-size:11.5px">${t('{s} s', { s: (x.end - x.start).toFixed(2) })}</span></div></div>
      </div>
      <${Form} fields=${TEXT_FIELDS} value=${x} onChange=${set} ctx=${ctx} kf=${keyApi({ keys: x.keys, u: time - x.start, max: x.end - x.start, exprs: x.exprs, exprEach: x.expr_each, target: { kind: 'text', text: i }, t0: x.start, pathKeys: pathKeyNames(x.motion && x.motion.path),
        focus: ctx.kfFocus, setFocus: ctx.setKfFocus, seek: (u) => ctx.seek(x.start + u),
        edit: (fn) => update((p) => { const tx = p.texts[i]; fn(tx, tx); }) })} />
      <${MotionSection} obj=${x} what="text" ctx=${ctx} edit=${(fn) => update((p) => fn(p.texts[i]))} kf=${keyApi({ keys: x.keys, u: time - x.start, max: x.end - x.start, exprs: x.exprs, exprEach: x.expr_each, target: { kind: 'text', text: i }, t0: x.start, pathKeys: pathKeyNames(x.motion && x.motion.path),
        focus: ctx.kfFocus, setFocus: ctx.setKfFocus, seek: (u) => ctx.seek(x.start + u),
        edit: (fn) => update((p) => { const tx = p.texts[i]; fn(tx, tx); }) })} />
      ${ctx.pathNode && ctx.pathNode.what === 'motion' && html`<${NodeSection} obj=${x} what="motion" pi=${ctx.pathNode.pi}
        edit=${(fn) => update((p) => fn(p.texts[i]))} />`}
      <${MaskSection} clip=${x} screen=${true} kf=${keyApi({ keys: x.keys, u: time - x.start, max: x.end - x.start, exprs: x.exprs, exprEach: x.expr_each, target: { kind: 'text', text: i }, t0: x.start, pathKeys: pathKeyNames(x.motion && x.motion.path),
        focus: ctx.kfFocus, setFocus: ctx.setKfFocus, seek: (u) => ctx.seek(x.start + u),
        edit: (fn) => update((p) => { const tx = p.texts[i]; fn(tx, tx); }) })}
        selMask=${ctx.selMask} setSelMask=${ctx.setSelMask} ctx=${ctx} openEditor=${ctx.openEditor}
        editClip=${(fn) => update((p) => fn(p.texts[i]))} />`}
    </div>
  </div>`;
}

function TrackInspector({ proj, track, segIdx, time, meta, update, setSel, ctx, onGenLyrics, onEditText, hasLines, onImages }) {
  const def = trackById[track];
  const segs = proj.tracks[track];
  const i = Math.min(segIdx, segs.length - 1);
  const seg = segs[i];
  const setSeg = (ns) => update((p) => { p.tracks[track][i] = ns; });
  const end = i + 1 < segs.length ? segs[i + 1].at : meta.duration;
  const kf = keyApi({ keys: seg.keys, u: time - seg.at, max: end - seg.at, focus: ctx.kfFocus, setFocus: ctx.setKfFocus,
    exprs: seg.exprs, target: { kind: 'seg', track, seg: i }, t0: seg.at,
    seek: (u) => ctx.seek(seg.at + u), edit: (fn) => update((p) => { const s = p.tracks[track][i]; fn(s, s.cfg); }) });
  return html`<div>
    <div class="insp-h">
      <div class="insp-title"><${Icon} name=${def.icon} size=${16} cls="muted" />${t(def.label)}${def.group !== def.label && html`<span class="sub">${t(def.group)}</span>`}</div>
      <div class="segtabs">${segs.map((s, k) => html`<button class=${'segtab' + (k === i ? ' on' : '')} title=${s.label || ''}
          onClick=${() => { setSel({ kind: 'track', track, seg: k }); ctx.seek(s.at); }}>
          <span class="mono">${fmtTime(s.at, false)}</span><span class="nm">${s.label || t('Segment {n}', { n: k + 1 })}</span>
          <span class="sum">${segSummary(track, s.cfg)}</span></button>`)}</div>
      <div class="row">
        <button class="btn sm" onClick=${() => ctx.addSeg(track, time)} title=${t('New segment starting at the cursor (N)')}><${Icon} name="plus" size=${14} /> ${t('Segment at {t}', { t: fmtTime(time) })}</button>
        <span class="spacer"></span>
        <button class="btn sm ghost danger" disabled=${i === 0} title=${t('Delete this segment (Del)')}
          onClick=${() => { update((p) => { p.tracks[track].splice(i, 1); }); setSel({ kind: 'track', track, seg: i - 1 }); }}><${Icon} name="trash-2" size=${14} /> ${t('Delete')}</button>
      </div>
    </div>
    <div class="insp-b">
      <${SegHeader} segs=${segs} i=${i} set=${setSeg} meta=${meta} track=${track} ctx=${ctx} />
      <${Form} fields=${def.fields} value=${seg.cfg} onChange=${(cfg) => setSeg({ ...seg, cfg })} ctx=${ctx} isFirst=${i === 0} kf=${kf} />
      ${BIND_TRACKS.includes(track) && html`<${BindSection} proj=${proj} bind=${seg.bind} update=${update} what="effect"
        target=${(p) => p.tracks[track][i]} />`}
      ${track === 'background' && html`<${DepthSection} cfg=${seg.cfg} ctx=${ctx} at=${seg.at || 0} onChange=${(cfg) => setSeg({ ...seg, cfg })} />`}
      ${track === 'background' && html`<${Section} id="genimg" title=${t('Generate an image')} help=${'Create a background from a description, or edit an image, with AI on your GPU.'}>
        <button class="btn" onClick=${onImages}><${Icon} name="wand-sparkles" size=${15} /> ${t('Generate an image…')}</button></${Section}>`}
      ${track === 'lyrics' && html`<${MaskSection} clip=${segMaskOwner(seg)} screen=${true} kf=${kf}
        selMask=${ctx.selMask} setSelMask=${ctx.setSelMask} ctx=${ctx} openEditor=${ctx.openEditor}
        editClip=${(fn) => update((p) => editSegMasks(p.tracks.lyrics[i], fn))} />`}
      ${track === 'lyrics' && html`<${Section} id="genlyr" title=${t('Generate lyrics')} help=${'Creates an .lrc with the timing of every word, from the lyrics as text or transcribing the song.'}>
        <div class="row"><button class="btn" onClick=${onGenLyrics}><${Icon} name="wand-sparkles" size=${15} /> ${t('Generate synced lyrics…')}</button>
        ${hasLines && html`<button class="btn" title=${t('Correct the whole lyrics as text and align it again')} onClick=${onEditText}><${Icon} name="pencil" size=${14} /> ${t('Edit text')}</button>`}</div></${Section}>`}
    </div>
  </div>`;
}

function LineEditor({ lyr, lines, time, onChange, seek, saveState, onResync, resyncing, lyricLang, setLyricLang, aligner, setAligner, wordEdit, setWordEdit, onEditText }) {
  const idx = lines[0];
  const line = lyr.lines[idx];
  if (!line) return html`<div class="insp-b faint" style="padding-top:14px">${t('Select a line in the lyrics track.')}</div>`;
  const multi = lines.length > 1;
  const setWord = (w, k, v) => {
    const l = clone(line);
    l.words[w][k] = v;
    if (k === 'start' && w > 0 && l.words[w - 1].end > v) l.words[w - 1].end = v;
    l.start = l.words[0].start;
    l.end = l.words[l.words.length - 1].end;
    onChange((all) => { all[idx] = l; });
  };
  const shift = (dt) => onChange((all) => {
    for (const i of lines) {
      const l = all[i];
      l.words.forEach((w) => { w.start += dt; w.end += dt; });
      l.start += dt; l.end += dt;
    }
  });
  const fix = (l) => { l.start = l.words[0].start; l.end = l.words[l.words.length - 1].end; return l; };
  // Split the line before word k (the words keep their times); join it with the next line
  const split = (k) => onChange((all) => {
    const a = clone(line);
    const b = { ...clone(line), words: a.words.splice(k) };
    all.splice(idx, 1, fix(a), fix(b));
  });
  // The first word sung after the cursor starts the new line (cursor inside the line)
  const splitAt = time > line.start && time < line.end ? line.words.findIndex((w, k) => k > 0 && w.start >= time) : -1;
  const next = lyr.lines[idx + 1];
  const join = () => onChange((all) => {
    const a = clone(line);
    a.words = a.words.concat(clone(next.words));
    all.splice(idx, 2, fix(a));
  });
  const status = { saving: t('Saving…'), saved: t('Saved'), error: t('Save error') }[saveState] || '';
  return html`<div>
    <div class="insp-h">
      <div class="insp-title"><${Icon} name="text-cursor-input" size=${16} cls="muted" />${multi ? t('{n} lines', { n: lines.length }) : t('Line {n}', { n: idx + 1 })}
        <span class="sub">${status}</span></div>
      <div class="row" style="margin-top:10px">
        <button class="btn sm" title=${t('Go to the start of the line')} onClick=${() => seek(line.start - 0.5)}><${Icon} name="play" size=${13} /> ${t('Go')}</button>
        <div class="btngroup">
          <button class="btn sm" title=${t('Move earlier by 0.05 s')} onClick=${() => shift(-0.05)}>−0.05 s</button>
          <button class="btn sm" title=${t('Move later by 0.05 s')} onClick=${() => shift(0.05)}>+0.05 s</button>
        </div>
        ${!multi && html`<button class="btn sm icon" title=${t('Make the line start at the cursor')} onClick=${() => shift(time - line.start)}><${Icon} name="crosshair" size=${14} /></button>`}
        ${!multi && html`<button class=${'btn sm' + (wordEdit ? ' on' : '')} title=${t('Show the words on the timeline to adjust each one (Esc to finish)')}
          onClick=${() => setWordEdit(!wordEdit)}><${Icon} name=${wordEdit ? 'check' : 'pencil'} size=${13} /> ${t(wordEdit ? 'Done' : 'Edit words')}</button>`}
      </div>
      <div class="row" style="margin-top:8px">
        <button class="btn sm primary" disabled=${resyncing} title=${t('Recognize this part of the song again and re-align the words (around their current position)')}
          onClick=${onResync}><${Icon} name=${resyncing ? 'loader-circle' : 'wand-sparkles'} size=${14} cls=${resyncing ? 'spin' : ''} /> ${t(multi ? 'Resync lines' : 'Resync line')}</button>
        <${Select} cls="" value=${lyricLang} options=${LYRIC_LANGS} onChange=${setLyricLang} title=${t('Lyrics language')} />
        <${Select} cls="" value=${aligner} options=${[['sylla', 'Sylla'], ['whisper', 'Whisper']]} onChange=${setAligner} title=${t('Timing model: Sylla (made for sung lyrics, most precise) or Whisper (speech recognition)')} />
      </div>
      ${!multi && html`<div class="row" style="margin-top:8px">
        <button class="btn sm" disabled=${splitAt < 0} title=${t('Split the line before the first word after the cursor')} onClick=${() => split(splitAt)}>
          <${Icon} name="scissors" size=${13} /> ${t('Split at cursor')}</button>
        <button class="btn sm" disabled=${!next} title=${t('Join this line with the next one')} onClick=${join}><${Icon} name="merge" size=${13} /> ${t('Join next')}</button>
        <button class="btn sm" title=${t('Correct the whole lyrics as text and align it again')} onClick=${onEditText}><${Icon} name="file-text" size=${13} /> ${t('Edit text')}</button>
      </div>`}
    </div>
    ${multi ? html`<div class="insp-b"><p class="faint" style="font-size:12px">${t('Several lines selected (Shift+click). Drag them together on the timeline, shift them with the buttons, or resync them.')}</p></div>`
      : html`<div class="insp-b" onContextMenu=${helpMenu(t('Words'), 'Tip: with Edit words, drag the words (or their edges) on the timeline; or play, pause right when a word starts and press the crosshair button. If a line is far off, drag it roughly into place and press Resync.')}><div class="words">
      <div class="word head"><span>${t('Word')}</span><span>${t('Start')}</span><span>${t('End')}</span><span></span></div>
      ${line.words.map((w, k) => html`<div class=${'word' + (time >= w.start && time < w.end ? ' now' : '')}>
        <${Edit} type="text" value=${w.text} onChange=${(e) => setWord(k, 'text', e.target.value)} />
        <${Edit} type="number" step="0.01" value=${w.start.toFixed(2)} onChange=${(e) => setWord(k, 'start', +e.target.value)} />
        <${Edit} type="number" step="0.01" value=${w.end.toFixed(2)} onChange=${(e) => setWord(k, 'end', +e.target.value)} />
        <span class="row" style="gap:2px"><button class="btn icon" title=${t('Set the start at the cursor')} onClick=${() => setWord(k, 'start', +time.toFixed(2))}><${Icon} name="crosshair" size=${14} /></button>
        ${k > 0 && html`<button class="btn icon" title=${t('Start a new line at this word')} onClick=${() => split(k)}><${Icon} name="scissors" size=${14} /></button>`}</span>
      </div>`)}
    </div></div>`}
  </div>`;
}

// ------------------------------------------------------------------ app

function Welcome({ st, ctx }) {
  return html`<div class="welcome">
    <div class="hero">
      <div class="mark"><${Icon} name="audio-waveform" size=${24} /></div>
      <h1>AudioViz Studio</h1>
      <p class="muted" style="margin:0">${t('Audio-reactive music videos, rendered on the GPU')}</p>
    </div>
    <div class="cards">
      <div class="card2" onClick=${() => ctx.dialog({ type: 'new' })}><${Icon} name="plus" size=${20} />
        <h3>${t('New project')}</h3><p>${t('Creates a project folder; start by choosing the audio and the background.')}</p></div>
      <div class="card2" onClick=${() => document.getElementById('welcome-import').click()}><${Icon} name="upload" size=${20} />
        <h3>${t('Import project')}</h3><p>${t('Unpacks a project .zip into the workspace and opens it.')}</p></div>
      <div class="card2" onClick=${() => ctx.pickFile('project', (p) => ctx.openProject(p))}><${Icon} name="folder-open" size=${20} />
        <h3>${t('Open project')}</h3><p>${t('Opens an existing project.yaml, including hand-written ones.')}</p></div>
    </div>
    <input type="file" id="welcome-import" accept=".zip" style="display:none"
      onChange=${(e) => e.target.files[0] && ctx.importPackage(e.target.files[0])} />
    <div class="ws"><h2>${t('Your projects')}</h2><${WorkspaceGrid} onOpen=${(p) => ctx.openProject(p)} /></div>
    <div class="row"><${LangSelect} /></div>
    <p class="faint mono" style="font-size:11px">${(st.gpu || '').trim()}</p>
  </div>`;
}

function LangSelect() {
  const lang = useStore(langStore);
  return html`<span class="lang"><${Select} cls="" value=${lang} options=${LANGS} translate=${false} onChange=${setLang} title=${t('Interface language')} /></span>`;
}

const NO_LYRICS = { lines: [], path: '' };

function App() {
  useStore(langStore); // re-render everything when the language changes
  const [st, setSt] = useState(null);
  const [proj, setProj] = useState(null);
  useEffect(() => { exprLib.set((proj && proj.expressions) || {}); }, [proj && proj.expressions]);
  const [meta, setMeta] = useState(null);
  const [ana, setAna] = useState(null);
  // the song's lyrics: part of the project (its /lyrics: changed by parts like the rest, collab.js)
  const lyr = (proj && proj.lyrics) || NO_LYRICS;
  const [lyrSave, setLyrSave] = useState('');
  const [sel, setSel] = useState({ kind: 'output' }); // (the project's settings first)
  const [lineSel, setLineSel] = useState([]);
  const [wordEdit, setWordEdit] = useState(false); // word blocks of the selected line on the timeline
  // Keyframes: the property whose keys the timeline shows alone (set by its diamond), per selection
  const [kfFocus, setKfFocus] = useState(null);
  const [kfSel, setKfSel] = useState(null);
  const [depthView, setDepthView] = useState(false); // the preview shows the depth of the scene
  const adj = useStore(adjusting); // a field being adjusted that shows something on the preview // selected keys: { key: the target's id, ts: their times }
  const [layerDrag, setLayerDrag] = useState(null); // reordering layers: { from, to }
  const [editor, setEditor] = useState(false); // the full-screen editor
  const [editorTool, setEditorTool] = useState('select');
  const [pathWhat, setPathWhat] = useState('motion'); // the path tool edits the motion path or an emitter's route
  const [pathNodeI, setPathNodeI] = useState(null); // the node selected on it (the inspector's node section)
  const [selMask, setSelMask] = useState(null); // the mask of the selected clip being edited
  useEffect(() => setSelMask(null), [sel.kind, sel.id, sel.clip, sel.i, sel.track, sel.seg]);
  useEffect(() => { setKfFocus(null); setKfSel(null); setPathNodeI(null); }, [sel.kind, sel.track, sel.seg, sel.i, sel.id, sel.clip]);
  // the property filter goes away with a click anywhere but on its keys, on that property's field
  // (its name, value, diamond), on a menu or on the filter's chip
  useEffect(() => {
    if (!kfFocus) return;
    const down = (e) => {
      const el = e.target instanceof Element ? e.target : null;
      if (!el) return;
      const fld = el.closest('[data-kfpath]');
      if (fld && fld.dataset.kfpath === kfFocus) return;
      if (el.closest('.kf, .ed-key, .ctxmenu, .kf-chip')) return;
      setKfFocus(null);
    };
    addEventListener('pointerdown', down, true);
    return () => removeEventListener('pointerdown', down, true);
  }, [kfFocus]);
  const [playing, setPlaying] = useState(false);
  const [scale, setScale] = useState(2);
  const [rev, setRev] = useState(0);
  const [save, setSave] = useState({ kind: '', at: '' });
  const [job, setJob] = useState(null);
  // A job's end as a browser notification (optional, per browser; when the tab isn't looked at)
  const [notifyJobs, setNotifyJobsState] = useState(() => {
    try { return localStorage.getItem('audioviz.notifyJobs') === '1' && 'Notification' in window && Notification.permission === 'granted'; } catch (e) { return false; }
  });
  const notifyRef = useRef(notifyJobs);
  notifyRef.current = notifyJobs;
  const toggleNotifyJobs = async () => {
    if (notifyJobs) { try { localStorage.setItem('audioviz.notifyJobs', '0'); } catch (e) { /* none */ } setNotifyJobsState(false); return; }
    if (!('Notification' in window)) { alert(t('This browser has no notifications')); return; }
    const p = Notification.permission === 'granted' ? 'granted' : await Notification.requestPermission();
    if (p === 'granted') { try { localStorage.setItem('audioviz.notifyJobs', '1'); } catch (e) { /* none */ } setNotifyJobsState(true); }
  };
  const lastJob = useRef({ id: '', status: '' });
  const openJobRef = useRef(null);
  const jobEnded = (j) => {
    const was = lastJob.current;
    lastJob.current = { id: j.id, status: j.status };
    if (j.status === 'running' || (was.id === j.id && was.status === j.status)) return; // not an end, or one seen already
    if (!notifyRef.current || (!document.hidden && document.hasFocus())) return;
    const what = t({ export: 'Video export', image: 'Image', comfy: 'ComfyUI', depth: 'Depth', lyrics: 'Lyrics', stems: 'Stems' }[j.kind] || j.kind);
    const title = t(j.status === 'done' ? '{what}: done' : j.status === 'canceled' ? '{what}: cancelled' : '{what}: failed', { what });
    try {
      const n = new Notification(title, { body: (j.by ? j.by + ' · ' : '') + (j.message || ''), tag: 'av-job-' + j.id });
      n.onclick = () => { window.focus(); n.close(); if (openJobRef.current) openJobRef.current(j); };
    } catch (e) { /* none */ }
  };
  const [dlg, setDlg] = useState(null);
  // a job's dialog (a ComfyUI flow's: the Images dialog for an image's, its own otherwise)
  const openJob = (j) => {
    const fl = j.kind === 'comfy' && flowsStore.get().find((f) => f.id === j.ref);
    if (fl && fl.kind !== 'image' && fl.kind !== 'edit') { setDlg({ type: 'comfyrun', id: fl.id }); return; }
    const d = { export: 'export', lyrics: 'genlyrics', image: 'images', comfy: 'images' }[j.kind];
    if (d) setDlg({ type: d });
  };
  openJobRef.current = openJob;
  const [pick, setPick] = useState(null); // file browser: own layer, above any dialog
  const [toast, setToast] = useState('');
  // Layout: side panel widths and timeline height, adjustable with the dividers, remembered by the browser
  const layoutDef = () => ({ left: 220, right: 340, tl: Math.round(innerHeight * 0.36) });
  const [layout, setLayoutState] = useState(() => {
    try { return { ...layoutDef(), ...JSON.parse(localStorage.getItem('audioviz.layout') || '{}') }; } catch (e) { return layoutDef(); }
  });
  const setLayout = (k, v) => setLayoutState((l) => {
    const n = { ...l, [k]: v ?? layoutDef()[k] }; // null = default size
    try { localStorage.setItem('audioviz.layout', JSON.stringify(n)); } catch (e) { /* ignore */ }
    return n;
  });
  const tlH = Math.max(160, Math.min(innerHeight - 220, layout.tl));
  const setTlH = (v) => setLayout('tl', v);
  const colDrag = (e, side) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const x0 = e.clientX, w0 = layout[side], el = e.currentTarget;
    el.classList.add('on');
    document.body.classList.add('resizing-col');
    const lim = side === 'left' ? [160, 420] : [280, 640];
    const mv = (ev) => {
      const dx = ev.clientX - x0;
      setLayout(side, Math.round(Math.max(lim[0], Math.min(lim[1], side === 'left' ? w0 + dx : w0 - dx))));
    };
    const up = () => {
      removeEventListener('mousemove', mv); removeEventListener('mouseup', up);
      el.classList.remove('on');
      document.body.classList.remove('resizing-col');
    };
    addEventListener('mousemove', mv); addEventListener('mouseup', up);
  };
  const [hideSetup, setHideSetup] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [resyncing, setResyncing] = useState(false);
  // How lyrics get their timing (generate, align again, resync): shared, remembered with the last generation
  const [aligner, setAlignerState] = useState(() => { try { return (JSON.parse(localStorage.getItem('audioviz.lastGen')) || {}).aligner || 'sylla'; } catch (e) { return 'sylla'; } });
  const setAligner = (a) => {
    setAlignerState(a);
    try { localStorage.setItem('audioviz.lastGen', JSON.stringify({ ...(JSON.parse(localStorage.getItem('audioviz.lastGen')) || {}), aligner: a })); } catch (e) { /* ignore */ }
  };
  const [lyricLang, setLyricLangState] = useState(() => { try { return localStorage.getItem('audioviz.lyricLang') || 'en'; } catch (e) { return 'en'; } });
  const timeStore = useRef(createStore(0)).current;
  // Slow copy of the time for everything that doesn't need 60 updates per second
  const time = useThrottledStore(timeStore, 250, playing);
  const audio = useRef(new Audio());
  // Hear the audio while moving through time with the playback stopped (remembered per browser)
  const scrubber = useRef(null);
  if (!scrubber.current) scrubber.current = createScrubber();
  const [scrub, setScrubState] = useState(() => { try { return localStorage.getItem('audioviz.scrub') !== '0'; } catch (e) { return true; } });
  const scrubRef = useRef(scrub);
  scrubRef.current = scrub;
  const setScrub = (on) => {
    setScrubState(on);
    try { localStorage.setItem('audioviz.scrub', on ? '1' : '0'); } catch (e) { /* ignore */ }
    if (on) scrubber.current.preload(); else scrubber.current.stop();
  };
  const projRef = useRef(null);
  const lyrRef = useRef(lyr);
  projRef.current = proj;
  lyrRef.current = lyr;
  // Undo / redo: snapshots of {project, lyrics} at each committed change (drags count once, when
  // released; changes less than 0.6 s apart, like moving a slider, merge into one). 32 steps.
  const hist = useRef({ past: [], future: [], cur: null, at: 0 });
  const [, setHistTick] = useState(0);
  const histBase = () => { if (!hist.current.cur) hist.current.cur = { proj: projRef.current, lyr: null }; };
  const record = () => {
    const h = hist.current;
    const snap = { proj: projRef.current, lyr: null };
    if (!h.cur) { h.cur = snap; return; }
    if (snap.proj === h.cur.proj && snap.lyr === h.cur.lyr) return;
    // an entry: what changed (and back), and the lyrics before / after
    const fwd = diff(h.cur.proj, snap.proj), inv = diff(snap.proj, h.cur.proj);
    const lyr = snap.lyr !== h.cur.lyr ? { before: h.cur.lyr, after: snap.lyr } : null;
    h.cur = snap;
    if (!fwd.length && !lyr) return;
    const now = performance.now();
    const last = h.past[h.past.length - 1];
    if (now - h.at > 600 || !last) {
      h.past.push({ fwd, inv, lyr });
      if (h.past.length > 64) h.past.shift();
    } else {
      last.fwd = [...last.fwd, ...fwd];
      last.inv = [...inv, ...last.inv];
      if (lyr) last.lyr = { before: last.lyr ? last.lyr.before : lyr.before, after: lyr.after };
    }
    h.at = now;
    h.future = [];
    setHistTick((x) => x + 1);
  };

  const setLyricLang = (l) => { setLyricLangState(l); try { localStorage.setItem('audioviz.lyricLang', l); } catch (e) { /* ignore */ } };
  const showErr = (e) => { setToast(String(e.message || e)); setTimeout(() => setToast(''), 6000); };

  const loadAll = async () => {
    const s = await api('GET', '/api/state');
    setSt(s);
    if (s.job) setJob(s.job);
    if (!s.project) { setProj(null); return; }
    const d = await api('GET', '/api/project');
    sync.current = { conf: d.project, v: d.v || 0, epoch: d.epoch || '', shadow: d.project, inflight: null };
    metaRef.current = d.meta;
    const p0 = localView(d.project);
    projRef.current = p0;
    setProj(p0);
    hist.current = { past: [], future: [], cur: null, at: 0 }; // a (re)opened project starts a new history
    setHistTick((x) => x + 1);
    setMeta(d.meta);
    loadAudio(d.project);
    loadAna();
    loadLyrics();
    loadChat();
    setRev((r) => r + 1);
  };
  const loadAna = async () => {
    try {
      const a = await api('GET', '/api/analysis');
      const st = a.stems && { ...a.stems, drums: b64u8(a.stems.drums), bassline: b64u8(a.stems.bassline),
        vocals: b64u8(a.stems.vocals), other: b64u8(a.stems.other) };
      setAna({ ...a, bass: b64u8(a.bass), mid: b64u8(a.mid), treble: b64u8(a.treble), rms: b64u8(a.rms), wave: b64u8(a.wave), stems: st });
    } catch (e) { showErr(e); }
  };
  // (the lyrics come with the project: another file only clears the lines selected)
  const loadLyrics = () => { setLineSel([]); };
  const loadAudio = (p) => {
    const a = audio.current;
    const src = p.audio.path ? '/api/media?path=' + encodeURIComponent(p.audio.path) : '';
    if (a.dataset.src !== src) { a.src = src; a.dataset.src = src; a.load(); }
    scrubber.current.setSource(src);
    if (src) scrubber.current.preload(); // (decoded for the playback too)
  };

  // --- collaboration: who I am, who else is here, their cursors (collab.js, peers.js)
  const meRef = useRef(null);
  if (!meRef.current) { meRef.current = whoAmI(); setClient(meRef.current.id); }
  const [peers, setPeers] = useState([]);
  const [cursors, setCursors] = useState({});
  const [showCursors, setShowCursorsState] = useState(() => { try { return localStorage.getItem('audioviz.cursors') !== '0'; } catch (e) { return true; } });
  const setShowCursors = (on) => { setShowCursorsState(on); try { localStorage.setItem('audioviz.cursors', on ? '1' : '0'); } catch (e) { /* ignore */ } };
  const presence = useRef({ cursor: null, sel: null, timer: 0 });
  const sendPresence = (patch) => {
    Object.assign(presence.current, patch);
    if (presence.current.timer) return;
    presence.current.timer = setTimeout(() => {
      presence.current.timer = 0;
      const m = meRef.current;
      fetch('/api/presence', { method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ client: m.id, name: m.name, color: m.color, cursor: presence.current.cursor, sel: presence.current.sel }) }).catch(() => {});
    }, 70);
  };
  const renameMe = () => {
    const n = prompt(t('Your name (the others see it):'), meRef.current.name || '');
    if (n == null) return;
    meRef.current = { ...meRef.current, name: n.trim().slice(0, 40) };
    saveMe(meRef.current);
    sendPresence({});
  };

  useEffect(() => { sendPresence({ sel }); }, [sel]);

  const [agentRev, setAgentRev] = useState(0); // the agents' changes to review changed

  // --- the project's chat (chat.js)
  const [chatOpen, setChatOpen] = useState(false);
  const chatState = useStore(chatStore);
  // a link to what is selected, at this moment (av://…)
  const linkNow = () => {
    const p = projRef.current, tt = timeStore.get();
    const at = `[${fmtTime(tt)}](av://t/${tt.toFixed(2)})`;
    if (!p) return at + ' ';
    let el = '';
    if (sel.kind === 'text' && p.texts[sel.i]) el = `[${(p.texts[sel.i].label || p.texts[sel.i].text || t('Text')).split('\n')[0].slice(0, 30)}](av://text/${sel.i})`;
    else if (sel.kind === 'track' && p.tracks[sel.track]) el = `[${t(trackById[sel.track] ? trackById[sel.track].label : sel.track)}${sel.seg ? ' #' + (sel.seg + 1) : ''}](av://track/${sel.track}/${sel.seg || 0})`;
    else if (sel.kind === 'layer') { const l = p.layers.find((x) => x.id === sel.id); if (l) el = `[${l.name || t('Layer')}${sel.clip != null ? ' #' + (sel.clip + 1) : ''}](av://layer/${l.id}/${sel.clip ?? ''})`; }
    return (el ? el + ' · ' : '') + at + ' ';
  };
  // a link in a message: go to its time, or select its element
  const onAvLink = (url) => {
    const ps = url.replace('av://', '').split('/');
    if (ps[0] === 't') seek(+ps[1]);
    else if (ps[0] === 'text') setSel({ kind: 'text', i: +ps[1] });
    else if (ps[0] === 'track') setSel({ kind: 'track', track: ps[1], seg: +ps[2] || 0 });
    else if (ps[0] === 'layer') setSel({ kind: 'layer', id: ps[1], clip: ps[2] === '' || ps[2] == null ? null : +ps[2] });
  };

  // --- the project's sync: conf = as the server has it (version v); inflight = my batch on its
  // way (one at a time); shadow = conf + inflight; what is on screen (projRef) may have more of
  // mine not sent yet
  const sync = useRef({ conf: null, v: 0, epoch: '', shadow: null, inflight: null });
  const viewKey = () => 'audioviz.view:' + ((metaRef.current && metaRef.current.path) || '');
  const metaRef = useRef(null);
  metaRef.current = meta;
  const localView = (p) => {
    try { const v = JSON.parse(localStorage.getItem(viewKey())); if (v) return { ...p, view: v }; } catch (e) { /* none */ }
    return p;
  };
  // (the paths the song and the lyrics come from: when they change, they are loaded again)
  const watchPaths = (before, after) => {
    if (!before || !after) return;
    if (after.audio.path !== before.audio.path) { loadAna(); loadAudio(after); }
    if (after.tracks.lyrics[0].cfg.path !== before.tracks.lyrics[0].cfg.path) loadLyrics();
  };
  // a batch applied by the server (mine acknowledged, or someone else's)
  const receive = (b) => {
    const S = sync.current;
    if (!S.conf || !projRef.current) return;
    const mine = S.inflight && b.client === meRef.current.id && b.id === S.inflight.id;
    if (b.v <= S.v) {
      if (mine && !(b.ops || []).length) { S.inflight = null; S.shadow = S.conf; flushRef.current(); }
      return;
    }
    if (b.v > S.v + 1) { catchUp(); return; }
    const unsent = diff(S.shadow, projRef.current);
    const before = S.conf;
    S.conf = applyOps(S.conf, b.ops);
    S.v = b.v;
    let inflight = S.inflight ? S.inflight.ops : [];
    let un = unsent, remote = null;
    if (mine) { S.inflight = null; inflight = []; }
    else {
      remote = b.ops;
      if (inflight.length) { [inflight, remote] = transformPair(inflight, remote); S.inflight.ops = inflight; }
      if (un.length) { let r2; [un, r2] = transformPair(un, remote); remote = r2; }
    }
    S.shadow = applyOps(S.conf, inflight);
    const disp = applyOps(S.shadow, un);
    disp.view = projRef.current.view;
    for (const k of ['clipDefaults', 'textDefaults', 'emitterPresets']) disp[k] = projRef.current[k];
    if (!deepEqual(disp, projRef.current)) { projRef.current = disp; setProj(disp); setRev((x) => x + 1); }
    if (remote && remote.length) { histRemote(remote); selRemote(remote); }
    watchPaths(before, S.conf);
    if (!S.inflight) flushRef.current();
  };
  // what I have selected moves with others' inserts and removals (a text, a segment, a clip)
  const selRemote = (ops) => {
    const mv = (path) => { const r = transformPair([{ op: 'set', path }], ops)[0][0]; return r ? r.path.split('/') : null; };
    setSel((s) => {
      if (s.kind === 'text' && s.i != null) { const p = mv('/texts/' + s.i); return p ? { ...s, i: +p[2] } : { ...s, i: 0 }; }
      if (s.kind === 'track' && s.seg != null) { const p = mv('/tracks/' + s.track + '/' + s.seg); return p ? { ...s, seg: +p[3] } : { ...s, seg: 0 }; }
      if (s.kind === 'layer' && s.clip != null) {
        const li = projRef.current.layers.findIndex((l) => l.id === s.id);
        if (li < 0) return s;
        const p = mv('/layers/' + li + '/clips/' + s.clip);
        return p ? { ...s, clip: +p[4] } : { ...s, clip: null };
      }
      return s;
    });
  };
  // missed batches (an event lost): fetched; too many: the project taken again
  const catchUp = async () => {
    const S = sync.current;
    try {
      const r = await api('GET', '/api/project/ops?v=' + S.v + '&epoch=' + encodeURIComponent(S.epoch));
      for (const b of r.batches) receive(b);
    } catch (e) { resyncProject(); }
  };
  // the project taken again from the server, keeping what is mine and not sent
  const resyncProject = async () => {
    const S = sync.current;
    const unsent = S.shadow && projRef.current ? diff(S.shadow, projRef.current) : [];
    const r = await api('GET', '/api/project');
    S.conf = r.project; S.v = r.v; S.epoch = r.epoch; S.inflight = null; S.shadow = r.project;
    const disp = applyOps(r.project, unsent);
    disp.view = projRef.current ? projRef.current.view : r.project.view;
    projRef.current = disp; setProj(disp); setMeta(r.meta); setRev((x) => x + 1);
    flushRef.current();
  };
  // my changes not sent yet → one batch (when none is on its way)
  const flushRef = useRef(null);
  const flushNow = async () => {
    const S = sync.current;
    if (!S.conf || S.inflight || !projRef.current) return;
    const ops = diff(S.shadow, projRef.current);
    if (!ops.length) return;
    const id = Math.random().toString(36).slice(2, 10);
    S.inflight = { id, ops };
    S.shadow = applyOps(S.shadow, ops);
    setSave({ kind: 'saving' });
    try {
      const r = await api('POST', '/api/project/ops', { epoch: S.epoch, base: S.v, ops, client: meRef.current.id, id });
      setMeta(r.meta);
      setSave({ kind: 'saved', at: r.saved });
      receive(r.batch);
      setRev((x) => x + 1); // the server has it: the preview shows it (also paused)
    } catch (e) {
      if (e.status === 409) { S.inflight = null; resyncProject().catch(showErr); return; }
      // not taken (not valid): what is on screen goes back to what the server has
      setSave({ kind: 'error' }); showErr(e);
      S.inflight = null; S.shadow = S.conf;
      const disp = { ...clone(S.conf), view: projRef.current.view };
      projRef.current = disp; setProj(disp);
    }
  };
  if (!flushRef.current) flushRef.current = debounce(() => flushNow(), 120);

  useEffect(() => {
    loadAll().catch(showErr);
    const m = meRef.current;
    if (!m.name) setTimeout(renameMe, 600);
    const es = new EventSource('/api/events?' + new URLSearchParams({ client: m.id, name: m.name || '', color: m.color }));
    es.onmessage = (ev) => {
      const msg = JSON.parse(ev.data);
      if (msg.type === 'job') {
        setJob(msg.data);
        jobEnded(msg.data);
        if (msg.data.kind === 'lyrics' && msg.data.status === 'done') onLyricsGenerated(msg.data.output);
      }
      if (msg.type === 'ops') { receive(msg.data); if (msg.data.agent || msg.data.reverts) setAgentRev((x) => x + 1); }
      if (msg.type === 'changes') setAgentRev((x) => x + 1);
      if (msg.type === 'comfy') loadFlows();
      if (msg.type === 'depthedit') setRev((x) => x + 1); // (a depth edited by hand: the preview shows it)
      if (msg.type === 'chat' || msg.type === 'typing') chatEvent(msg.type, msg.data, meRef.current, notifyMsg);
      if (msg.type === 'presence') setPeers(msg.data || []);
      if (msg.type === 'cursor' && msg.data.id !== meRef.current.id) setCursors((c) => ({ ...c, [msg.data.id]: { ...msg.data, at: Date.now() } }));
      // an agent opened a project, or someone saved all of it at once: taken again
      if (msg.type === 'project' && msg.data && msg.data.kind === 'opened') loadAll().catch(showErr);
      if (msg.type === 'project' && msg.data && msg.data.kind === 'reload') resyncProject().catch(showErr);
    };
    es.onerror = () => { /* (it reconnects by itself) */ };
    // when the stream comes back, what was missed meanwhile
    es.onopen = () => { if (sync.current.conf) catchUp(); };
    return () => es.close();
  }, []);

  // history = false: saved with the project but not an undo step (e.g. the view of the tracks)
  const update = (fn, history = true) => {
    histBase();
    const old = projRef.current;
    const p = clone(old);
    fn(p);
    setProj(p);
    projRef.current = p;
    flushRef.current();
    if (!deepEqual(p.view, old.view)) { try { localStorage.setItem(viewKey(), JSON.stringify(p.view)); } catch (e) { /* ignore */ } }
    if (history) record();
  };
  // Studio view stored in the project: hidden timeline tracks and folded zones
  const toggleView = (key, id) => update((p) => {
    p.view = { hidden: [], collapsed: [], ...(p.view || {}) };
    const a = (p.view[key] = [...(p.view[key] || [])]);
    const i = a.indexOf(id);
    if (i >= 0) a.splice(i, 1); else a.push(id);
  }, false);

  // Undo / redo: mine only. An entry's operations (and its inverse) are applied on the project as
  // it is now (others' changes stay); the lyrics as a whole
  const applyEntry = (ops, lyrTo) => {
    const old = projRef.current;
    const np = applyOps(old, ops);
    np.view = old.view;
    projRef.current = np;
    setProj(np);
    flushRef.current();
    hist.current.cur = { proj: np, lyr: null };
    // Keep the selection valid
    setSel((s) => {
      if (s.kind === 'track') { const n = np.tracks[s.track].length; return s.seg >= n ? { ...s, seg: n - 1 } : s; }
      if (s.kind === 'text') return s.i >= np.texts.length ? { ...s, i: np.texts.length - 1 } : s;
      return s;
    });
    setLineSel((ls) => ls.filter((i) => i < ((np.lyrics && np.lyrics.lines.length) || 0)));
  };
  const undo = () => {
    const h = hist.current;
    if (!h.past.length) return;
    const e = h.past.pop();
    h.future.push(e);
    h.at = 0;
    applyEntry(e.inv, e.lyr && e.lyr.before);
    setHistTick((x) => x + 1);
  };
  const redo = () => {
    const h = hist.current;
    if (!h.future.length) return;
    const e = h.future.pop();
    h.past.push(e);
    h.at = 0;
    applyEntry(e.fwd, e.lyr && e.lyr.after);
    setHistTick((x) => x + 1);
  };
  // someone else's change: my history moves past it (its places), and so does its base
  const histRemote = (ops) => {
    const h = hist.current;
    const mv = (e) => ({ ...e, fwd: transformPair(e.fwd, ops)[0], inv: transformPair(e.inv, ops)[0] });
    h.past = h.past.map(mv);
    h.future = h.future.map(mv);
    if (h.cur) h.cur = { ...h.cur, proj: applyOps(h.cur.proj, ops) };
  };

  // --- the lyrics: a change of the project's (its /lyrics), sent by parts like any other; save =
  // false: a drag, shown now and sent (and recorded) when released (onLyricsCommit)
  const editLyrics = (fn, save = true) => {
    const ed = (p) => { p.lyrics = p.lyrics || { lines: [], path: '' }; fn(p.lyrics.lines); };
    if (save) { update(ed); return; }
    histBase();
    const p = clone(projRef.current);
    ed(p);
    projRef.current = p;
    setProj(p);
  };

  // --- transport
  const seek = (tt) => {
    const d = meta ? meta.duration : 0;
    tt = Math.max(0, Math.min(d, tt));
    timeStore.set(tt);
    if (playing && audio.current.src) {
      if (scrubber.current.position() != null) scrubber.current.startPlay(tt); else audio.current.currentTime = tt;
    } else if (!playing && scrubRef.current) scrubber.current.play(tt);
  };
  // The background's cuts: where each segment starts and where its transition ends (and the
  // song's ends). dir = +1 / -1: to the next / previous one; while playing, "previous" skips the
  // one just passed (as a player's back button does)
  const seekCut = (dir) => {
    if (!meta || !projRef.current) return;
    const cs = [0, meta.duration];
    for (const g of projRef.current.tracks.background || []) {
      cs.push(g.at);
      if (g.at > 0 && g.transition && g.transition.mode !== 'cut' && g.transition.duration > 0) cs.push(g.at + g.transition.duration);
    }
    cs.sort((a, b) => a - b);
    const now = timeStore.get(), eps = dir < 0 && playing ? 0.4 : 0.02;
    const c = dir > 0 ? cs.find((x) => x > now + eps) : cs.reverse().find((x) => x < now - eps);
    if (c != null) seek(c);
  };
  // Fast forward / rewind while held (a button or J / L): x2, x3 with Shift. Playback pauses
  // while it lasts and goes on from the new place when released; with scrub audio on, snippets
  const shuttle = useRef(null);
  const shuttleStart = (dir, speed) => {
    if (!meta || shuttle.current) return;
    const sh = { resume: playing, raf: 0, speed };
    if (playing) setPlaying(false);
    let last = performance.now();
    const tick = () => {
      const now = performance.now();
      const tt = Math.max(0, Math.min(meta.duration, timeStore.get() + dir * sh.speed * (now - last) / 1000));
      last = now;
      timeStore.set(tt);
      if (scrubRef.current) scrubber.current.play(tt);
      sh.raf = requestAnimationFrame(tick);
    };
    sh.raf = requestAnimationFrame(tick);
    shuttle.current = sh;
  };
  const shuttleStop = () => {
    const sh = shuttle.current;
    if (!sh) return;
    cancelAnimationFrame(sh.raf);
    shuttle.current = null;
    scrubber.current.stop();
    if (sh.resume && timeStore.get() < meta.duration) setPlaying(true);
  };
  useEffect(() => {
    if (!playing) return;
    const a = audio.current;
    let raf;
    scrubber.current.stop();
    const base = performance.now(), t0 = timeStore.get();
    // the decoded song (exact), else the <audio> element (while it is being decoded)
    const decoded = !!a.src && scrubber.current.startPlay(t0);
    if (a.src && !decoded) { a.currentTime = t0; a.play().catch(showErr); }
    const tick = () => {
      const pos = scrubber.current.position();
      const tt = pos != null ? pos : a.src ? a.currentTime : t0 + (performance.now() - base) / 1000;
      if (meta && tt >= meta.duration) { setPlaying(false); return; }
      timeStore.set(tt);
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => { cancelAnimationFrame(raf); a.pause(); scrubber.current.stopPlay(); };
  }, [playing]);

  // --- actions
  const ctx = {
    job, showErr,
    seek,
    get time() { return timeStore.get(); },
    selMask, setSelMask,
    openEditor: (tool, what) => { setEditorTool(tool || 'select'); setPathWhat(what || 'motion'); setPathNodeI(null); setEditor(true); },
    // the path node selected in the editor: { what: 'motion' | 'route', pi } or null
    get pathNode() { return editor && pathNodeI != null ? { what: pathWhat === 'route' ? 'route' : 'motion', pi: pathNodeI } : null; },
    // z_index choices: each layer bottom to top as [z, name]
    get layerChoices() { return (projRef.current.layers || []).map((l, i) => [i + 1, l.name || t('Layer {n}', { n: i + 1 })]); },
    get textChoices() { return (projRef.current.texts || []).map((x, i) => [i, x.label || (x.text || '').split('\n')[0] || t('Text {n}', { n: i + 1 })]); },
    kfFocus, setKfFocus, kfSel, setKfSel,
    dialog: setDlg,
    pickFile: (kind, cb, start) => setPick({ kind, cb, start }),
    reloadAna: () => loadAna(),
    openProject: async (p) => { try { await api('POST', '/api/project/open', { path: p }); await loadAll(); } catch (e) { showErr(e); } },
    importPackage: async (f) => { try { await uploadPackage(f); await loadAll(); } catch (e) { showErr(e); } },
    setAudio: (path) => update((p) => { p.audio.path = path; }),
    // The background goes to the segment active at the cursor (or the first one if still empty).
    // keepSize: with the output size on automatic (= the background's), fix the current size so
    // a smaller image (e.g. a generated one) doesn't shrink the video
    setBackground: (path, keepSize) => update((p) => {
      if (keepSize && meta && (!p.output.width || !p.output.height)) { p.output.width = meta.width; p.output.height = meta.height; }
      const segs = p.tracks.background;
      let i = 0;
      if (segs[0].cfg.path) segs.forEach((g, k) => { if (g.at <= timeStore.get()) i = k; });
      segs[i].cfg.path = path;
    }),
    setLyrics: (path) => update((p) => { p.tracks.lyrics.forEach((g) => { g.cfg.path = path; }); p.tracks.lyrics[0].cfg.enabled = true; }),
    setFont: (path) => update((p) => { p.tracks.lyrics.forEach((g) => { g.cfg.font = path; }); }),
    addSeg: (track, tt) => {
      const segs = projRef.current.tracks[track];
      if (tt <= 0.01) { setSel({ kind: 'track', track, seg: 0 }); return; }
      let i = segs.length - 1;
      while (i > 0 && segs[i].at > tt) i--;
      const ns = { at: +tt.toFixed(3), label: '', transition: { mode: 'cut', duration: 1 }, cfg: clone(segs[i].cfg) };
      update((p) => { p.tracks[track].splice(i + 1, 0, ns); });
      setSel({ kind: 'track', track, seg: i + 1 });
    },
    // Overlay layers: a new clip asks for its file; it lasts 10 s (or up to the end)
    emitterPresets: proj && proj.emitterPresets,
    // A new emitter layer in front of the others, with a clip of that preset at the cursor
    addEmitter: (tt, preset) => {
      const id = newLayerId();
      const st = Math.max(0, Math.min(meta.duration - 0.5, +tt.toFixed(3)));
      const names = { fire: t('Fire'), fire_real: t('Realistic fire'), sparks: t('Sparks'), smoke: t('Smoke'), dust: t('Dust'), light: t('Lights (bokeh)'), explosion: t('Explosion'), snow: t('Snow') };
      update((p) => {
        p.layers = [...(p.layers || []), { id, name: names[preset] || '', kind: 'emitter',
          clips: [newEmitterClip(p, preset, st, Math.min(meta.duration - st, 10))] }];
      });
      setSel({ kind: 'layer', id, clip: 0 });
    },
    addClip: (id, tt) => {
      const lay = (projRef.current.layers || []).find((x) => x.id === id);
      if (isEmitterLayer(lay)) {
        // an emitter clip like the layer's last one (or fire)
        const st = Math.max(0, Math.min(meta.duration - 0.5, +tt.toFixed(3)));
        let j = 0;
        update((p) => {
          const l = p.layers.find((x) => x.id === id);
          const prev = l.clips[l.clips.length - 1];
          const nc = prev ? { ...clone(prev), start: st, end: +Math.min(meta.duration, st + (prev.end - prev.start)).toFixed(3), keys: undefined }
            : newEmitterClip(p, 'fire', st, Math.min(meta.duration - st, 10));
          delete nc.keys;
          l.clips.push(nc);
          l.clips.sort((a, b) => a.start - b.start);
          j = l.clips.indexOf(nc);
        });
        setSel({ kind: 'layer', id, clip: Math.max(0, j) });
        return;
      }
      ctx.addMediaClip(id, tt);
    },
    addMediaClip: (id, tt) => setPick({ kind: 'bg', cb: (path) => {
      const st = Math.max(0, Math.min(meta.duration - 0.5, +tt.toFixed(3)));
      let j = 0;
      update((p) => {
        const l = p.layers.find((x) => x.id === id);
        l.clips.push({ ...clone(p.clipDefaults || {}), path, start: st, end: +Math.min(meta.duration, st + 10).toFixed(3) });
        l.clips.sort((a, b) => a.start - b.start);
        j = l.clips.findIndex((c) => c.path === path && c.start === st);
      });
      setSel({ kind: 'layer', id, clip: Math.max(0, j) });
    } }),
    // A new layer goes in front of the others, with a first clip at the cursor
    addLayer: (tt) => setPick({ kind: 'bg', cb: (path) => {
      const id = newLayerId();
      const st = Math.max(0, Math.min(meta.duration - 0.5, +tt.toFixed(3)));
      update((p) => {
        p.layers = [...(p.layers || []), { id, name: '', clips: [{ ...clone(p.clipDefaults || {}), path, start: st, end: +Math.min(meta.duration, st + 10).toFixed(3) }] }];
      });
      setSel({ kind: 'layer', id, clip: 0 });
    } }),
    // Fixed texts: a new one copies the style of the selected text (or the defaults)
    addText: (tt) => {
      const cur = projRef.current;
      const base = (sel.kind === 'text' && cur.texts[sel.i]) || cur.textDefaults || {};
      const st = Math.max(0, Math.min(meta.duration - 0.5, +tt.toFixed(3)));
      const nt = { ...clone(base), text: t('Title'), label: '', start: st, end: +Math.min(meta.duration, st + 5).toFixed(3), enabled: true };
      update((p) => { p.texts.push(nt); });
      setSel({ kind: 'text', i: projRef.current.texts.length - 1 });
    },
    selectText: (i) => {
      setSel({ kind: 'text', i });
      const x = projRef.current.texts[i];
      // Show it: jump into its time range if the cursor is outside
      if (x) { const now = timeStore.get(); if (now < x.start || now > x.end) seek(x.start + Math.min(1, (x.end - x.start) / 2)); }
    },
  };
  // Overlay clips between layers of the same kind (image layers, emitter layers): copied or moved
  // to another layer (or a new one like it), pasted at a time; clipBoard: the copied clip
  const clipBoard = useRef(null);
  const sameKind = (a, b) => (a && a.kind || '') === (b && b.kind || '');
  const clipToLayer = (fromId, j, toId, move, at = null) => {
    let target = toId, nj = 0;
    update((p) => {
      const src = p.layers.find((l) => l.id === fromId);
      const c = clone(src.clips[j]);
      let dst = p.layers.find((l) => l.id === toId);
      if (!dst) { // a new layer like its own, just in front of it
        target = newLayerId();
        dst = { ...clone(src), id: target, name: (src.name || '') ? src.name + ' 2' : '', clips: [] };
        p.layers.splice(p.layers.indexOf(src) + 1, 0, dst);
      }
      if (at != null) {
        const len = c.end - c.start;
        c.start = +Math.max(0, Math.min(meta.duration - len, at)).toFixed(3);
        c.end = +(c.start + len).toFixed(3);
      }
      if (move) src.clips.splice(j, 1);
      dst.clips.push(c);
      dst.clips.sort((a, b) => a.start - b.start);
      if (move) src.clips.sort((a, b) => a.start - b.start);
      nj = dst.clips.indexOf(c);
    });
    setSel({ kind: 'layer', id: target, clip: nj });
  };
  const pasteClip = (toId, at) => {
    const cb = clipBoard.current;
    if (!cb) return;
    let nj = 0;
    update((p) => {
      const dst = p.layers.find((l) => l.id === toId);
      const c = clone(cb.clip);
      const len = c.end - c.start;
      c.start = +Math.max(0, Math.min(meta.duration - len, at)).toFixed(3);
      c.end = +(c.start + len).toFixed(3);
      dst.clips.push(c);
      dst.clips.sort((a, b) => a.start - b.start);
      nj = dst.clips.indexOf(c);
    });
    setSel({ kind: 'layer', id: toId, clip: nj });
  };
  // the layers a clip of layer l can go to, as menu items (a second menu where the first was)
  const layerTargets = (e, l, j, move) => {
    const p = projRef.current;
    const items = [{ header: t(move ? 'Move to the layer' : 'Copy to the layer') }];
    frontFirst(p).forEach(({ l: x }) => {
      if (x.id !== l.id && sameKind(x, l)) {
        items.push({ label: layerName(x, p.layers.indexOf(x)), icon: isEmitterLayer(x) ? 'sparkles' : 'layers', onClick: () => clipToLayer(l.id, j, x.id, move) });
      }
    });
    items.push('-', { label: t('A new layer'), icon: 'plus', onClick: () => clipToLayer(l.id, j, null, move) });
    // (openMenu takes an event: the first menu's place, with no-op preventDefault / stopPropagation)
    const at = { clientX: e.clientX, clientY: e.clientY, preventDefault() {}, stopPropagation() {} };
    setTimeout(() => openMenu(at, items), 0);
  };
  // Moving a clip on the timeline (live while dragging, committed at the end)
  const onMoveClip = (id, j, s, e, commit) => {
    if (commit) { update((p) => { p.layers.find((l) => l.id === id).clips.sort((a, b) => a.start - b.start); }); return; }
    histBase();
    const p = clone(projRef.current);
    const c = p.layers.find((l) => l.id === id).clips[j];
    c.start = +s.toFixed(3);
    c.end = +e.toFixed(3);
    projRef.current = p;
    setProj(p);
  };
  const onMoveText = (i, s, e, commit) => {
    if (commit) { update(() => {}); return; }
    histBase();
    const p = clone(projRef.current);
    p.texts[i].start = +s.toFixed(3);
    p.texts[i].end = +e.toFixed(3);
    projRef.current = p;
    setProj(p);
  };

  // The last generation's language and mode: aligning an edited text with the same ones reuses
  // the cached recognition
  const lastGen = () => { try { return JSON.parse(localStorage.getItem('audioviz.lastGen')) || {}; } catch (e) { return {}; } };
  const genLyrics = (o) => {
    try { localStorage.setItem('audioviz.lastGen', JSON.stringify({ lang: o.lang, fast: !!o.fast, aligner: o.aligner || 'whisper' }));
    if (o.aligner) setAlignerState(o.aligner); } catch (e) { /* ignore */ }
    api('POST', '/api/lyrics/generate', o).then(setJob).catch(showErr);
  };

  const onLyricsGenerated = (out) => {
    if (!projRef.current || !out) return;
    update((q) => { q.tracks.lyrics.forEach((s) => { s.cfg.path = out; }); q.tracks.lyrics[0].cfg.enabled = true; });
    setTimeout(loadLyrics, 800);
  };

  const onMoveSeg = (track, i, at, commit) => {
    if (commit) {
      update((p) => {
        const segs = p.tracks[track];
        const moved = segs[i];
        p.tracks[track] = [segs[0], ...segs.slice(1).sort((a, b) => a.at - b.at)];
        setSel({ kind: 'track', track, seg: p.tracks[track].indexOf(moved) });
      });
      return;
    }
    histBase();
    const p = clone(projRef.current);
    p.tracks[track][i].at = +at.toFixed(3);
    projRef.current = p;
    setProj(p);
  };

  const onMarker = (op, i, at) => {
    if (op === 'add') {
      const label = prompt(t('Marker name (e.g. Chorus):'), t('Marker'));
      if (label === null) return;
      update((p) => { p.markers.push({ at: +(at ?? timeStore.get()).toFixed(3), label }); p.markers.sort((a, b) => a.at - b.at); });
    } else if (op === 'rename') {
      const label = prompt(t('New name:'), projRef.current.markers[i].label);
      if (label !== null) update((p) => { p.markers[i].label = label; });
    } else if (op === 'delete') {
      if (confirm(t('Delete the marker “{m}”?', { m: projRef.current.markers[i].label }))) update((p) => { p.markers.splice(i, 1); });
    } else if (op === 'remove') update((p) => { p.markers.splice(i, 1); }); // from its menu: no question (undo exists)
  };

  // Splits segment i of a track at time tt: the new segment starts there with the same settings.
  // Its keyframes are shared out so the animation goes on without a jump: each animated property
  // gets a key with its value at the split on both sides.
  const splitSeg = (track, i, tt) => {
    update((p) => {
      const segs = p.tracks[track], s = segs[i];
      const u = +(tt - s.at).toFixed(3);
      const ns = { at: +tt.toFixed(3), label: '', transition: { mode: 'cut', duration: 1 }, cfg: clone(s.cfg) };
      if (s.bind) ns.bind = [...s.bind]; // (an effect's halves stay on the same backgrounds; a background's id is not copied)
      if (s.keys) {
        const left = {}, right = {};
        for (const path in s.keys) {
          const l = s.keys[path], v = valueAt(l, u);
          const prev = l.filter((k) => k.t < u - KEY_EPS).pop();
          left[path] = [...l.filter((k) => k.t < u - KEY_EPS), { t: u, v }];
          right[path] = [{ t: 0, v, ease: prev && prev.ease }, ...l.filter((k) => k.t > u + KEY_EPS).map((k) => ({ ...k, t: +(k.t - u).toFixed(3) }))];
          setIn(ns.cfg, path, v);
        }
        s.keys = left;
        ns.keys = right;
      }
      segs.splice(i + 1, 0, ns);
    });
    setSel({ kind: 'track', track, seg: i + 1 });
  };
  // Copied segment settings (and keyframes), pasted onto another segment of the same track
  const segClip = useRef(null);

  // Right-click menus (menu.js) of the timeline and the track list
  const onContext = (e, c) => {
    const p = projRef.current;
    const def = trackById[c.track || c.id];
    if (c.kind === 'seg') {
      const segs = p.tracks[c.track], s = segs[c.seg];
      const end = c.seg + 1 < segs.length ? segs[c.seg + 1].at : meta.duration;
      setSel({ kind: 'track', track: c.track, seg: c.seg });
      const clip = segClip.current;
      openMenu(e, [
        { header: t(def.label) + ' · ' + (s.label || t('Segment {n}', { n: c.seg + 1 })) },
        { label: t('Split here ({t})', { t: fmtTime(c.at) }), icon: 'scissors', disabled: c.at - s.at < 0.05 || end - c.at < 0.05,
          onClick: () => splitSeg(c.track, c.seg, c.at) },
        { label: t('Rename…'), icon: 'pencil', onClick: () => {
          const label = prompt(t('New name:'), s.label || '');
          if (label !== null) update((q) => { q.tracks[c.track][c.seg].label = label; });
        } },
        '-',
        { label: t('Copy settings'), icon: 'copy', onClick: () => { segClip.current = { track: c.track, cfg: clone(s.cfg), keys: s.keys && clone(s.keys) }; } },
        { label: t('Paste settings'), icon: 'clipboard', disabled: !clip || clip.track !== c.track, onClick: () => update((q) => {
          const d = q.tracks[c.track][c.seg];
          d.cfg = clone(clip.cfg);
          if (clip.keys) d.keys = clone(clip.keys); else delete d.keys;
        }) },
        '-',
        { label: t('Delete segment'), icon: 'trash-2', danger: true, disabled: c.seg === 0, onClick: () => {
          update((q) => { q.tracks[c.track].splice(c.seg, 1); });
          setSel({ kind: 'track', track: c.track, seg: c.seg - 1 });
        } },
      ]);
    } else if (c.kind === 'text') {
      const x = p.texts[c.i];
      ctx.selectText(c.i);
      openMenu(e, [
        { header: t('Text') + ' · ' + (x.label || (x.text || '').split('\n')[0].slice(0, 24)) },
        { label: t('Duplicate'), icon: 'copy', onClick: () => {
          update((q) => { q.texts.splice(c.i + 1, 0, clone(q.texts[c.i])); });
          setSel({ kind: 'text', i: c.i + 1 });
        } },
        { label: t('Delete'), icon: 'trash-2', danger: true, onClick: () => {
          update((q) => { q.texts.splice(c.i, 1); });
          setSel({ kind: 'text', i: Math.min(c.i, p.texts.length - 2) });
        } },
      ]);
    } else if (c.kind === 'texts') {
      openMenu(e, [{ label: t('New text here ({t})', { t: fmtTime(c.at) }), icon: 'plus', onClick: () => ctx.addText(c.at) }]);
    } else if (c.kind === 'marker') {
      const m = p.markers[c.i];
      openMenu(e, [
        { header: m.label },
        { label: t('Go there'), icon: 'flag', onClick: () => seek(m.at) },
        { label: t('Rename…'), icon: 'pencil', onClick: () => onMarker('rename', c.i) },
        { label: t('Delete'), icon: 'trash-2', danger: true, onClick: () => onMarker('remove', c.i) },
      ]);
    } else if (c.kind === 'time') {
      openMenu(e, [
        { label: t('Move the cursor here ({t})', { t: fmtTime(c.at) }), icon: 'crosshair', onClick: () => seek(c.at) },
        { label: t('Marker here'), icon: 'flag', onClick: () => onMarker('add', null, c.at) },
      ]);
    } else if (c.kind === 'entry') {
      // An entry of the track list (track id or "texts")
      const off = (p.view && p.view.hidden || []).includes(c.id);
      openMenu(e, [
        { header: c.id === 'texts' ? t('Titles and texts') : t(def.label) },
        { label: t(off ? 'Show its track on the timeline' : 'Hide its track on the timeline'), icon: off ? 'eye' : 'eye-off',
          onClick: () => toggleView('hidden', c.id) },
        c.id === 'texts'
          ? { label: t('New text at the cursor ({t})', { t: fmtTime(timeStore.get()) }), icon: 'plus', onClick: () => ctx.addText(timeStore.get()) }
          : { label: t('New segment at the cursor ({t})', { t: fmtTime(timeStore.get()) }), icon: 'plus', onClick: () => ctx.addSeg(c.id, timeStore.get()) },
        ...(() => {
          // its help: the track's own, or what its "Enabled" says
          const h = def && (def.help || ((def.fields || []).find((f) => f.key === 'enabled') || {}).help);
          return h ? ['-', helpItem(t(def.label), t(h))] : [];
        })(),
      ]);
    } else if (c.kind === 'layer' || c.kind === 'clip' || c.kind === 'layerlane') {
      const li = p.layers.findIndex((l) => l.id === c.id), l = p.layers[li];
      const ff = frontFirst(p).findIndex((x) => x.l.id === c.id);
      const hidden = (p.view && p.view.hidden || []).includes('layer:' + c.id);
      const items = [{ header: layerName(l, li) + (c.kind === 'clip' ? ' · ' + (l.clips[c.clip].label || basename(l.clips[c.clip].path)) : '') }];
      if (c.kind === 'clip') {
        selectLayer(c.id, c.clip);
        items.push(
          { label: t('Duplicate'), icon: 'copy', onClick: () => { let nj = 0; update((q) => { nj = duplicateClip(q.layers.find((x) => x.id === c.id), c.clip); }); setSel({ kind: 'layer', id: c.id, clip: nj }); } },
          { label: t('Copy clip'), icon: 'clipboard', onClick: () => { clipBoard.current = { kind: l.kind || '', clip: clone(l.clips[c.clip]) }; } },
          { label: t('Copy to the layer…'), icon: 'copy', onClick: () => layerTargets(e, l, c.clip, false) },
          { label: t('Move to the layer…'), icon: 'arrow-left-right', onClick: () => layerTargets(e, l, c.clip, true) },
          { label: t('Delete clip'), icon: 'trash-2', danger: true, onClick: () => { update((q) => { q.layers.find((x) => x.id === c.id).clips.splice(c.clip, 1); }); setSel({ kind: 'layer', id: c.id, clip: Math.max(0, c.clip - 1) }); } },
          '-');
      }
      const at = c.at ?? timeStore.get();
      const cb = clipBoard.current;
      items.push(
        { label: t('New clip at {t}', { t: fmtTime(at) }), icon: 'plus', onClick: () => ctx.addClip(c.id, at) },
        ...(cb && (cb.kind || '') === (l.kind || '') ? [{ label: t('Paste clip at {t}', { t: fmtTime(at) }), icon: 'clipboard', onClick: () => pasteClip(c.id, at) }] : []),
        { label: t('Rename…'), icon: 'pencil', onClick: () => { const n = prompt(t('New name:'), l.name || ''); if (n !== null) update((q) => { q.layers.find((x) => x.id === c.id).name = n; }); } },
        { label: t('Bring forward'), icon: 'arrow-up', disabled: ff === 0, onClick: () => moveLayer(ff, ff - 1) },
        { label: t('Send backward'), icon: 'chevron-down', disabled: ff === p.layers.length - 1, onClick: () => moveLayer(ff, ff + 1) },
        { label: t(hidden ? 'Show its track on the timeline' : 'Hide its track on the timeline'), icon: hidden ? 'eye' : 'eye-off', onClick: () => toggleView('hidden', 'layer:' + c.id) },
        '-',
        helpItem(t('Layer'), t('Layers higher in the list are drawn in front. Drag them in the left panel to change the order.')),
        '-',
        { label: t('Delete layer'), icon: 'trash-2', danger: true, onClick: () => { if (confirm(t('Delete the layer “{n}” and its clips?', { n: layerName(l, li) }))) { update((q) => { q.layers = q.layers.filter((x) => x.id !== c.id); }); setSel({ kind: 'output' }); } } });
      openMenu(e, items);
    } else if (c.kind === 'zone') {
      const folded = (p.view && p.view.collapsed || []).includes(c.id);
      openMenu(e, [{ label: t(folded ? 'Unfold' : 'Fold'), icon: folded ? 'chevron-down' : 'chevron-right', onClick: () => toggleView('collapsed', c.id) }]);
    }
  };

  const selectLine = (i, multi) => {
    let ns;
    if (multi && lineSel.length) {
      const a = Math.min(lineSel[0], i), b = Math.max(lineSel[lineSel.length - 1], i);
      ns = Array.from({ length: b - a + 1 }, (_, k) => a + k);
    } else ns = lineSel.includes(i) && lineSel.length > 1 ? lineSel : [i];
    if (ns.length !== 1 || ns[0] !== lineSel[0] || lineSel.length !== 1) setWordEdit(false);
    setLineSel(ns);
    setSel({ kind: 'line' });
  };

  const resync = async () => {
    if (!lineSel.length) return;
    setResyncing(true);
    try {
      const before = lyrRef.current.lines;
      const r = await api('POST', '/api/lyrics/resync', { lines: before, targets: lineSel, lang: lyricLang, aligner });
      editLyrics((all) => { for (const i of lineSel) if (r.lines[i]) all[i] = r.lines[i]; });
      // Lines whose voice wasn't recognized clearly enough are returned untouched
      const same = (a, b) => a && b && a.words.length === b.words.length && a.words.every((w, k) => Math.abs(w.start - b.words[k].start) < 1e-6);
      const kept = lineSel.filter((i) => same(before[i], r.lines[i])).map((i) => i + 1);
      if (kept.length) showErr(t('Could not recognize line {n} clearly; it was left as it was. Drag it closer to where it is sung and try again.', { n: kept.join(', ') }));
    } catch (e) { showErr(e); }
    setResyncing(false);
  };

  // --- drag & drop files: uploaded to assets/ and assigned by type
  const dropFiles = async (files) => {
    for (const file of files) {
      const kind = kindOf(file.name);
      if (!kind) { showErr(t('Don’t know what to do with “{f}”', { f: file.name })); continue; }
      try {
        setSave({ kind: 'saving' });
        const fd = new FormData();
        fd.append('file', file);
        const r = await fetch('/api/upload', { method: 'POST', body: fd });
        const d = await r.json();
        if (!r.ok) throw new Error(d.error);
        if (kind === 'audio') { ctx.setAudio(d.path); setSel({ kind: 'audio' }); }
        else if (kind === 'bg') { ctx.setBackground(d.path); setSel({ kind: 'track', track: 'background', seg: 0 }); }
        else if (kind === 'lyrics') { ctx.setLyrics(d.path); setSel({ kind: 'track', track: 'lyrics', seg: 0 }); }
        else if (kind === 'font') { ctx.setFont(d.path); setSel({ kind: 'track', track: 'lyrics', seg: 0 }); }
        else if (kind === 'txt') setDlg({ type: 'genlyrics', txt: d.path });
      } catch (e) { showErr(e); }
    }
  };
  useEffect(() => {
    let depth = 0;
    const has = (e) => e.dataTransfer && [...e.dataTransfer.types].includes('Files');
    const enter = (e) => { if (!has(e)) return; e.preventDefault(); depth++; setDragging(true); };
    const over = (e) => { if (has(e)) e.preventDefault(); };
    const leave = (e) => { if (!has(e)) return; depth = Math.max(0, depth - 1); if (!depth) setDragging(false); };
    const drop = (e) => {
      if (!has(e)) return;
      e.preventDefault(); depth = 0; setDragging(false);
      if (!projRef.current) { showErr(t('Create or open a project before adding files')); return; }
      dropFiles([...e.dataTransfer.files]);
    };
    addEventListener('dragenter', enter); addEventListener('dragover', over);
    addEventListener('dragleave', leave); addEventListener('drop', drop);
    return () => { removeEventListener('dragenter', enter); removeEventListener('dragover', over); removeEventListener('dragleave', leave); removeEventListener('drop', drop); };
  }, []);

  // --- keyboard
  const kfSelRef = useRef(kfSel); kfSelRef.current = kfSel;
  const kfFocusRef = useRef(kfFocus); kfFocusRef.current = kfFocus;
  const onKeysRef = useRef(null);
  useEffect(() => {
    const k = (e) => {
      if (e.target.matches('input, select, textarea')) return; // (they keep their own undo)
      if ((e.ctrlKey || e.metaKey) && !e.altKey && (e.key === 'z' || e.key === 'Z')) { e.preventDefault(); if (e.shiftKey) redo(); else undo(); return; }
      if ((e.ctrlKey || e.metaKey) && !e.altKey && (e.key === 'y' || e.key === 'Y')) { e.preventDefault(); redo(); return; }
      if (e.ctrlKey || e.metaKey) return; // leave the browser's own shortcuts alone
      if (!projRef.current || !meta) return;
      const fr = 1 / meta.fps;
      const now = timeStore.get();
      if (e.code === 'Space') { e.preventDefault(); setPlaying((p) => !p); }
      else if (e.key === 'ArrowLeft') seek(now - (e.shiftKey ? 1 : fr));
      else if (e.key === 'ArrowRight') seek(now + (e.shiftKey ? 1 : fr));
      else if (e.key === 'Home') seek(0);
      else if (e.key === 'End') seek(meta.duration);
      else if (e.key === 'PageUp') { e.preventDefault(); seekCut(-1); }
      else if (e.key === 'PageDown') { e.preventDefault(); seekCut(1); }
      else if (e.code === 'KeyJ' || e.code === 'KeyL') { if (!e.repeat) shuttleStart(e.code === 'KeyL' ? 1 : -1, e.shiftKey ? 3 : 2); }
      else if (e.key === 'm' || e.key === 'M') onMarker('add');
      else if ((e.key === 'e' || e.key === 'E') && !editor) setEditor(true);
      else if (e.key === 's' || e.key === 'S') setScrub(!scrubRef.current);
      else if (e.key === 'Escape') { setWordEdit(false); setKfSel(null); }
      else if ((e.key === 'Delete' || e.key === 'Backspace') && kfSelRef.current && kfSelRef.current.ts.length) {
        // the selected keys (of the shown property, if one is)
        const ks = kfSelRef.current, target = JSON.parse(ks.key);
        onKeysRef.current(target, (keys) => editKeysTimes(keys, ks.ts, kfFocusRef.current, { remove: true }));
        setKfSel(null);
      }
      else if ((e.key === 'n' || e.key === 'N') && sel.kind === 'track') ctx.addSeg(sel.track, now);
      else if (e.key === 'Delete' && sel.kind === 'track' && sel.seg > 0) {
        update((p) => { p.tracks[sel.track].splice(sel.seg, 1); });
        setSel({ ...sel, seg: sel.seg - 1 });
      } else if (e.key === 'Delete' && sel.kind === 'layer') {
        update((p) => { const l = p.layers.find((x) => x.id === sel.id); if (l && l.clips[sel.clip]) l.clips.splice(sel.clip, 1); });
        setSel({ ...sel, clip: Math.max(0, sel.clip - 1) });
      } else if (e.key === 'Delete' && sel.kind === 'text' && projRef.current.texts[sel.i]) {
        update((p) => { p.texts.splice(sel.i, 1); });
        setSel({ kind: 'text', i: Math.min(sel.i, projRef.current.texts.length - 1) });
      }
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, [meta, sel, playing, editor]);
  // releasing J / L (or leaving the window while held) ends the fast forward / rewind
  useEffect(() => {
    const up = (e) => { if (e.code === 'KeyJ' || e.code === 'KeyL') shuttleStop(); };
    const blur = () => shuttleStop();
    addEventListener('keyup', up); addEventListener('blur', blur);
    return () => { removeEventListener('keyup', up); removeEventListener('blur', blur); };
  }, [meta]);

  if (!st) return html`<div class="welcome"><p class="muted">${t('Loading…')}</p></div>`;

  const dialogs = html`
    ${dlg && dlg.type === 'new' && html`<${NewProject} start=${st.workspace || st.home || st.startDir} pickFile=${ctx.pickFile} onClose=${() => setDlg(null)}
        onCreate=${async (dir, name) => { await api('POST', '/api/project/new', { dir, name }); await loadAll(); }} />`}
    ${dlg && dlg.type === 'projects' && html`<${ProjectsDialog} current=${meta && meta.path} onClose=${() => setDlg(null)}
        onOpen=${(p) => ctx.openProject(p)} onNew=${() => setDlg({ type: 'new' })} onImported=${() => loadAll()} pickFile=${ctx.pickFile} />`}
    ${dlg && dlg.type === 'package' && proj && html`<${PackageDialog} meta=${meta} onClose=${() => setDlg(null)} onCollected=${() => loadAll()} />`}
    ${dlg && dlg.type === 'exports' && proj && html`<${ExportsDialog} meta=${meta} onClose=${() => setDlg(null)} />`}
    ${dlg && dlg.type === 'export' && proj && html`<${ExportDialog} proj=${proj} meta=${meta} job=${job} onClose=${() => setDlg(null)}
        onShowExports=${() => setDlg({ type: 'exports' })}
        onStart=${(o) => api('POST', '/api/export', o).then(setJob).catch(showErr)} onCancel=${() => api('POST', '/api/job/cancel')} />`}
    ${dlg && dlg.type === 'genlyrics' && proj && html`<${GenLyricsDialog} meta=${meta} job=${job} models=${st.models} initialTxt=${dlg.txt}
        pickFile=${ctx.pickFile} onClose=${() => setDlg(null)} onCancel=${() => api('POST', '/api/job/cancel')}
        onEditText=${() => setDlg({ type: 'lyrtext' })}
        onStart=${(o) => { if (o.lang !== 'auto') setLyricLang(o.lang); genLyrics(o); }} />`}
    ${dlg && dlg.type === 'lyrtext' && proj && html`<${LyricsTextDialog} lines=${lyr.lines} lang=${lastGen().lang} onClose=${() => setDlg(null)}
        aligner=${aligner} onAlign=${(text, lang, al) => { genLyrics({ text, lang, fast: lastGen().fast, aligner: al }); setDlg({ type: 'genlyrics' }); }} />`}
    ${dlg && dlg.type === 'images' && proj && html`<${ImagesDialog} meta=${meta} job=${job} pickFile=${ctx.pickFile}
        onClose=${() => setDlg(null)} onCancel=${() => api('POST', '/api/job/cancel')}
        onStart=${(o) => api('POST', '/api/images/generate', o).then(setJob).catch(showErr)}
        onRun=${(o) => api('POST', '/api/comfy/run', o).then(setJob)}
        onUseBackground=${(p) => { ctx.setBackground(p, true); setSel({ kind: 'track', track: 'background', seg: 0 }); setDlg(null); }} />`}
    ${dlg && dlg.type === 'comfyrun' && proj && html`<${ComfyRunDialog} id=${dlg.id} job=${job} pickFile=${ctx.pickFile}
        onClose=${() => setDlg(null)} onCancel=${() => api('POST', '/api/job/cancel')}
        onRun=${(o) => api('POST', '/api/comfy/run', o).then(setJob)}
        onUseBackground=${(p) => { ctx.setBackground(p, true); setSel({ kind: 'track', track: 'background', seg: 0 }); setDlg(null); }}
        onUseAudio=${(p) => { ctx.setAudio(p); setSel({ kind: 'audio' }); setDlg(null); }} />`}
    ${pick && html`<${FilePicker} kind=${pick.kind} start=${pick.start} hasProject=${!!proj}
        onPick=${pick.cb} onClose=${() => setPick(null)} />`}
    ${toast && html`<div class="toast" onClick=${() => setToast('')}><${Icon} name="triangle-alert" size=${16} />${toast}</div>`}`;

  if (!proj || !meta) return html`<${Welcome} st=${st} ctx=${ctx} />${dialogs}<${ContextMenu} /><${HelpDialog} /><${ExprDialog} />`;

  const view = { hidden: [], collapsed: [], ...(proj.view || {}) };
  // Entries of each zone in the track list (track ids, "texts")
  const layersFF = frontFirst(proj);
  const zoneIds = { main: ['background', 'lyrics', 'texts'], layers: layersFF.map(({ l }) => 'layer:' + l.id),
    effects: TRACKS.filter((tr) => zoneOf(tr.id) === 'effects').map((tr) => tr.id) };
  // Eye: shows / hides the entry's track on the timeline (the effect keeps rendering)
  const eye = (id) => {
    const off = view.hidden.includes(id);
    return html`<button class=${'eye' + (off ? ' off' : '')} title=${t(off ? 'Show its track on the timeline' : 'Hide its track on the timeline (it keeps rendering)')}
      onClick=${(e) => { e.stopPropagation(); toggleView('hidden', id); }}><${Icon} name=${off ? 'eye-off' : 'eye'} size=${14} /></button>`;
  };
  const trackEntry = (tr) => {
    const segs = proj.tracks[tr.id];
    let cur = 0;
    segs.forEach((s, k) => { if (s.at <= time) cur = k; });
    const c = segs[cur].cfg;
    const on = tr.id === 'background' ? !!c.path : c.enabled !== false;
    return html`<div class=${'trk' + (sel.kind === 'track' && sel.track === tr.id ? ' on' : '') + (on ? '' : ' off') + (view.hidden.includes(tr.id) ? ' hid' : '')}
        title=${segSummary(tr.id, c)} onClick=${() => selTrack(tr.id, null)} onContextMenu=${(e) => onContext(e, { kind: 'entry', id: tr.id })}>
      <${Icon} name=${tr.icon} size=${15} /><span class="lbl">${t(tr.label)}</span>
      ${segs.length > 1 && html`<span class="badge" title=${t('Number of segments')}>${segs.length}</span>`}${eye(tr.id)}</div>`;
  };
  const textsEntry = () => html`<div class=${'trk' + (sel.kind === 'text' ? ' on' : '') + (view.hidden.includes('texts') ? ' hid' : '')}
      onClick=${() => ctx.selectText((proj.texts || []).length ? 0 : -1)} onContextMenu=${(e) => onContext(e, { kind: 'entry', id: 'texts' })}>
    <${Icon} name="type" size=${15} /><span class="lbl">${t('Titles and texts')}</span>
    ${(proj.texts || []).length > 0 && html`<span class="badge">${proj.texts.length}</span>`}${eye('texts')}</div>`;
  const selTrack = (track, seg) => {
    const segs = proj.tracks[track];
    if (seg === null) { seg = 0; segs.forEach((s, k) => { if (s.at <= timeStore.get()) seg = k; }); }
    setSel({ kind: 'track', track, seg });
    setLineSel([]);
  };
  const jobRunning = job && job.status === 'running';
  const saveText = { saving: t('Saving…'), saved: t('Saved {t}', { t: save.at }), error: t('Save error') }[save.kind] || '';

  const layerById_ = (id) => (proj.layers || []).find((l) => l.id === id);
  // Selecting a layer: its clip under the cursor (or the first one)
  const selectLayer = (id, clip) => {
    const l = layerById_(id);
    if (clip == null) { clip = Math.max(0, l.clips.findIndex((c) => time >= c.start && time < c.end)); }
    setSel({ kind: 'layer', id, clip });
    setLineSel([]);
  };
  // Reordering layers in the left panel: drag an entry up / down (k: position front first)
  const layerReorder = (e, k) => {
    if (e.button !== 0 || e.target.closest('.eye')) return;
    const y0 = e.clientY;
    const els = [...e.currentTarget.parentElement.querySelectorAll('.lyrent')];
    let to = k, moved = false;
    const mv = (ev) => {
      if (!moved && Math.abs(ev.clientY - y0) < 4) return;
      moved = true;
      to = els.findIndex((el) => ev.clientY < el.getBoundingClientRect().bottom);
      if (to < 0) to = els.length - 1;
      setLayerDrag({ from: k, to });
    };
    const up = () => {
      removeEventListener('mousemove', mv); removeEventListener('mouseup', up);
      setLayerDrag(null);
      if (moved && to !== k) moveLayer(k, to);
    };
    addEventListener('mousemove', mv); addEventListener('mouseup', up);
  };
  // from / to: positions front first
  const moveLayer = (from, to) => update((p) => {
    const ff = [...p.layers].reverse();
    const [m] = ff.splice(from, 1);
    ff.splice(to, 0, m);
    p.layers = ff.reverse();
  });
  const selClip = sel.kind === 'layer' && layerById_(sel.id) ? layerById_(sel.id).clips[sel.clip] : null;
  const selEmit = sel.kind === 'layer' && isEmitterLayer(layerById_(sel.id)); // the selected clip throws particles
  // the preview: the image, the scene's depth, or (adjusting a light's range) what it lights
  const previewView = (() => {
    if (adj && adj.kind === 'lightrange' && selClip && selClip.emitter) {
      const l = selClip.emitter.light || {}, tt = time - selClip.start;
      const kv = (k, d) => { const ks = selClip.keys && selClip.keys['emitter.light.' + k]; return ks && ks.length ? valueAt(ks, tt) : (l[k] ?? d); };
      return 'lightrange&' + new URLSearchParams({ from: kv('from', 1), to: kv('to', 0), soft: kv('range_soft', 0.05) }).toString();
    }
    if (adj && adj.kind === 'range' && adj.keys) {
      // a depth range (a background effect's, the depth of field's focus, a text's light): what
      // is outside, black. Its settings: the selected segment's or fixed text's (keys "a.b")
      // (with its keyframes: the value now)
      const seg = sel.kind === 'track' && proj.tracks[sel.track] ? proj.tracks[sel.track][sel.seg] || {} : null;
      const src = seg ? seg.cfg || {} : sel.kind === 'text' ? (proj.texts || [])[sel.i] || {} : null;
      const keys = seg ? seg.keys : src && src.keys, t0 = seg ? seg.at || 0 : (src && src.start) || 0;
      if (src) {
        const g = (k) => {
          const ks = keys && keys[k];
          return ks && ks.length ? valueAt(ks, time - t0) : k.split('.').reduce((o, x) => (o == null ? undefined : o[x]), src);
        };
        const [kf, kt, ks] = adj.keys;
        let from = g(kf), to = g(kt);
        if (!from && !to) from = 1;
        return 'lightrange&' + new URLSearchParams({ from: from ?? 1, to: to ?? 0, soft: Math.min(g(ks) ?? 0.05, 0.08) }).toString();
      }
    }
    if (adj && adj.kind === 'nodedepth' && ctx.pathNode) {
      // a path node's depth: what is behind it, in red
      const o = selClip || (sel.kind === 'text' ? (proj.texts || [])[sel.i] : null), pn = ctx.pathNode;
      const sh = o && (pn.what === 'route' ? o.emitter && o.emitter.route : o.motion && o.motion.path);
      const v = sh && sh.vals && sh.vals[pn.pi];
      if (v && v.z != null) return 'objdepth&' + new URLSearchParams({ z: v.z, soft: 0.03 }).toString();
    }
    if (adj && adj.kind === 'solid' && selClip) // what an emitter goes round, in cyan
      return 'solid&' + new URLSearchParams({ layer: sel.id, clip: sel.clip }).toString();
    if (adj && adj.kind === 'objdepth' && selClip) // adjusting a clip's depth: what is behind it, in red
      return 'objdepth&' + new URLSearchParams({ layer: sel.id, clip: sel.clip }).toString();
    if (adj && adj.kind === 'objdepth' && sel.kind === 'text') return 'objdepth&text=' + sel.i; // a fixed text's
    if (adj && adj.kind === 'objdepth' && sel.kind === 'track' && sel.track === 'lyrics') return 'objdepth&text=-1'; // the lyrics'
    return depthView ? 'depth' : '';
  })();
  const selText = sel.kind === 'text' ? (proj.texts || [])[sel.i] : null;
  // What the mask tools work on: the selected clip (its image space), fixed text or lyrics segment
  // (the screen). { obj: { masks, keys, start }, screen, edit(fn(obj)) inside an update }
  const selLyrSeg = sel.kind === 'track' && sel.track === 'lyrics' ? proj.tracks.lyrics[sel.seg] : null;
  const maskOwner = selClip ? { obj: selClip, screen: selEmit, edit: (fn) => update((p) => fn(p.layers.find((l) => l.id === sel.id).clips[sel.clip])) }
    : selText ? { obj: selText, screen: true, edit: (fn) => update((p) => fn(p.texts[sel.i])) }
    : selLyrSeg ? { obj: segMaskOwner(selLyrSeg), screen: true, edit: (fn) => update((p) => editSegMasks(p.tracks.lyrics[sel.seg], fn)) }
    : null;
  // What the path tool works on: the selected clip or fixed text
  const onRoute = pathWhat === 'route' && !!selEmit;
  const pathOwner = maskOwner && !selLyrSeg ? { ...maskOwner, route: onRoute,
    shape: onRoute ? (maskOwner.obj.emitter && maskOwner.obj.emitter.route) || null : (maskOwner.obj.motion && maskOwner.obj.motion.path) || null } : null;
  // A mask tool was picked: use the selected mask if it is of that kind, else the first of that
  // kind, else a new one
  const needMask = (kind) => {
    if (!maskOwner) return;
    const ms = maskOwner.obj.masks || [];
    // a tool for either kind (the fill): the selected mask, else a shape mask
    if (kind === 'any') { if (selMask != null && ms[selMask]) return; kind = 'vector'; }
    if (selMask != null && ms[selMask] && (ms[selMask].kind || 'vector') === kind) return;
    const k = ms.findIndex((m) => (m.kind || 'vector') === kind);
    if (k >= 0) { setSelMask(k); return; }
    maskOwner.edit((o) => { o.masks = [...(o.masks || []), { id: newLayerId(), kind, mode: 'inside', feather: 0, opacity: 1, shapes: [], path: '' }]; });
    setSelMask(ms.length);
  };
  // A stroke of the brush / eraser ended: the painted mask is saved as a PNG in the project
  const onPainted = (blob) => {
    const m = maskOwner && maskOwner.obj.masks && maskOwner.obj.masks[selMask];
    if (!m) return;
    const k = selMask, own = maskOwner;
    fetch('/api/mask?name=' + encodeURIComponent(m.id || newLayerId()), { method: 'PUT', body: blob })
      .then((r) => r.json()).then((r) => {
        if (r.error) throw new Error(r.error);
        own.edit((o) => { o.masks[k].path = r.path; });
        setRev((x) => x + 1);
      }).catch(showErr);
  };
  // The selection's time range and keys, for the editor's small timeline
  // The selection's time range and its keys (and how to change them), for the editor's small
  // timeline: { start, end, keys, target } or null
  const editorMarks = (() => {
    let start, end, keys, target;
    if (selClip) { ({ start, end, keys } = selClip); target = { layer: sel.id, clip: sel.clip }; }
    else if (selText) { ({ start, end, keys } = selText); target = { text: sel.i }; }
    else if (sel.kind === 'track') {
      const segs = proj.tracks[sel.track], sg = segs[sel.seg];
      if (sg) { start = sg.at; end = sel.seg + 1 < segs.length ? segs[sel.seg + 1].at : meta.duration; keys = sg.keys; target = { track: sel.track, seg: sel.seg }; }
    }
    return start == null ? null : { start, end, keys: keys || {}, target };
  })();
  // Changes the keys of a target (a segment, a text or a clip): fn(keys) → new keys
  onKeysRef.current = (...a) => onKeys(...a);
  // (a property left without keys keeps the value it had at the cursor)
  const onKeys = (target, fn) => update((p) => {
    const h = target.layer != null ? p.layers.find((l) => l.id === target.layer).clips[target.clip]
      : target.text != null ? p.texts[target.text] : p.tracks[target.track][target.seg];
    const old = h.keys || {};
    const ks = fn(old);
    const cfg = target.track != null ? h.cfg : h, u = timeStore.get() - (target.track != null ? h.at : h.start);
    for (const path in old) if (!ks[path] && old[path].length) setIn(cfg, path, valueAt(old[path], u));
    if (Object.keys(ks).length) h.keys = ks; else delete h.keys;
  });
  // Burn transition with chosen points: they are placed on the preview
  const selBg = sel.kind === 'track' && sel.track === 'background' ? proj.tracks.background[sel.seg] : null;
  const burnSel = selBg && sel.seg > 0 && selBg.transition.mode === 'burn' && (selBg.transition.burn || {}).origins === 'points' ? selBg.transition.burn : null;
  let inspector;
  if (sel.kind === 'audio') inspector = html`<div><div class="insp-h"><div class="insp-title"><${Icon} name="music" size=${16} cls="muted" />${t('Audio')}<span class="sub">${t('Project')}</span></div></div><div class="insp-b">
      <${Form} fields=${AUDIO_FIELDS} value=${proj.audio} onChange=${(v) => update((p) => { p.audio = v; })} ctx=${ctx} />
      <${StemsSection} ctx=${ctx} hasAudio=${meta.hasAudio} audio=${proj.audio} onAudio=${(v) => update((p) => { p.audio = v; })} /></div></div>`;
  else if (sel.kind === 'output') inspector = html`<div><div class="insp-h"><div class="insp-title"><${Icon} name="settings" size=${16} cls="muted" />${t('Project settings')}</div></div><div class="insp-b">
      <${Section} title=${t('Output')} id="output" defOpen=${true}>
        <${Form} fields=${OUTPUT_FIELDS} value=${proj.output} onChange=${(v) => update((p) => { p.output = v; })} ctx=${ctx} />
        <p class="faint" style="font-size:12px">${t('Current resolution')}: ${meta.width}×${meta.height} · ${meta.fps} fps · ${fmtTime(meta.duration)}</p>
      </${Section}>
      <${LibrarySection} lib=${proj.expressions || {}} Section=${Section} Edit=${Edit} onChange=${(l) => update((p) => { p.expressions = l; })} />
      <${ComfySection} comfy=${proj.comfy} onRun=${(id) => setDlg({ type: 'comfyrun', id })} onURL=${(u) => update((p) => { p.comfy = { ...(p.comfy || {}), url: u }; })} Section=${Section} Edit=${Edit} showErr=${showErr} />
      <${PluginsSection} Section=${Section} showErr=${showErr} />
      <${ModelsSection} Section=${Section} showErr=${showErr} /></div></div>`;
  else if (sel.kind === 'layer' && layerById_(sel.id)) inspector = html`<${LayerInspector} proj=${proj} sel=${sel} setSel=${setSel} update=${update} time=${time} meta=${meta} ctx=${ctx} />`;
  else if (sel.kind === 'text') inspector = html`<${TextsInspector} proj=${proj} sel=${sel} setSel=${setSel} update=${update} time=${time} meta=${meta} ctx=${ctx} />`;
  else if (sel.kind === 'line') inspector = html`<${LineEditor} lyr=${lyr} lines=${lineSel} time=${time} seek=${seek} saveState=${lyrSave}
      onChange=${(fn) => editLyrics(fn)} onResync=${resync} resyncing=${resyncing} lyricLang=${lyricLang} setLyricLang=${setLyricLang} aligner=${aligner} setAligner=${setAligner} wordEdit=${wordEdit} setWordEdit=${setWordEdit}
      onEditText=${() => setDlg({ type: 'lyrtext' })} />`;
  else inspector = html`<${TrackInspector} proj=${proj} track=${sel.track} segIdx=${sel.seg} time=${time} meta=${meta}
      update=${update} setSel=${setSel} ctx=${ctx} onGenLyrics=${() => setDlg({ type: 'genlyrics' })}
      onEditText=${() => setDlg({ type: 'lyrtext' })} hasLines=${lyr.lines.length > 0}
      onImages=${() => setDlg({ type: 'images' })} />`;

  return html`<div class="app" style=${{ '--tl-h': tlH + 'px', '--left-w': layout.left + 'px', '--right-w': layout.right + 'px' }}>
    <${ChatPanel} open=${chatOpen} onClose=${() => setChatOpen(false)} me=${meRef.current} peers=${peers} path=${meta.path}
      linkNow=${linkNow} timeStore=${timeStore} onSeek=${seek} onAvLink=${onAvLink} />
    <div class="split-v left" onMouseDown=${(e) => colDrag(e, 'left')} onDblClick=${() => setLayout('left', null)}
      title=${t('Drag to resize · double-click: default size')}></div>
    <div class="split-v right" onMouseDown=${(e) => colDrag(e, 'right')} onDblClick=${() => setLayout('right', null)}
      title=${t('Drag to resize · double-click: default size')}></div>
    <div class="top">
      <div class="brand"><span class="mark"><${Icon} name="audio-waveform" size=${13} /></span>AudioViz</div>
      <span class="sep"></span>
      <span class="projname" title=${meta.path}>${meta.name}</span>
      <span class=${'save' + (save.kind === 'error' ? ' err' : '')}>${saveText && html`<${Icon} name=${save.kind === 'error' ? 'triangle-alert' : save.kind === 'saving' ? 'loader-circle' : 'check'} size=${13} />`}${saveText}</span>
      <span class="spacer"></span>
      <${AgentChanges} me=${meRef.current} proj=${proj} rev=${agentRev} labelOf=${(id) => t(trackById[id] ? trackById[id].label : id)}
        onSelect=${setSel} showErr=${showErr} />
      <${PeersBar} peers=${peers} me=${meRef.current} show=${showCursors} setShow=${setShowCursors} onRename=${renameMe} />
      <button class=${'btn icon ghost chatbtn' + (chatOpen ? ' on' : '')} title=${t('Chat of the project')} onClick=${() => setChatOpen(!chatOpen)}>
        <${Icon} name="message-square" size=${16} />${(() => { const n = unreadCount(chatState.msgs, meRef.current, meta.path); return n > 0 && !chatOpen ? html`<i class="cbadge">${n > 99 ? '99+' : n}</i>` : null; })()}</button>
      ${job && html`<div class="jobbar" title=${job.message} onClick=${() => openJob(job)}>
        <${Icon} name=${job.status === 'running' ? (job.kind === 'export' ? 'film' : 'wand-sparkles') : job.status === 'done' ? 'check' : 'triangle-alert'} size=${14}
          cls=${{ done: 'st-ok', error: 'st-err', canceled: 'st-warn' }[job.status] || 'muted'} />
        <div class="progress"><i style=${{ width: (job.progress * 100).toFixed(1) + '%' }}></i></div>
        <span class="msg">${job.gen && job.status === 'running'
          ? `${t(STEP_LABEL[job.gen.steps[job.gen.current].id])} · ${Math.round(job.gen.steps[job.gen.current].progress * 100)}%`
          : job.export && job.status === 'running'
            ? `${t(job.export.stage === 'mux' ? 'Writing the MP4' : 'Exporting')} · ${Math.round(job.progress * 100)}%` : job.message}</span>
        ${job.status === 'running' && html`<button class="btn icon ghost sm" title=${t('Cancel it')}
          onClick=${(e) => { e.stopPropagation(); api('POST', '/api/job/cancel').catch(showErr); }}><${Icon} name="x" size=${13} /></button>`}
        <button class=${'btn icon ghost sm' + (notifyJobs ? '' : ' off')} title=${t(notifyJobs ? 'Notifying when a job ends (when this tab is not looked at)' : 'Notify me when a job ends')}
          onClick=${(e) => { e.stopPropagation(); toggleNotifyJobs(); }}><${Icon} name=${notifyJobs ? 'bell' : 'bell-off'} size=${13} /></button>
      </div>`}
      <div class="btngroup">
        <button class="btn icon ghost" disabled=${!hist.current.past.length} title=${t('Undo (Ctrl+Z)')} onClick=${undo}><${Icon} name="undo-2" size=${15} /></button>
        <button class="btn icon ghost" disabled=${!hist.current.future.length} title=${t('Redo (Ctrl+Y)')} onClick=${redo}><${Icon} name="redo-2" size=${15} /></button>
      </div>
      <button class="btn ghost" onClick=${() => setDlg({ type: 'projects' })} title=${t('Your projects: open, new, import')}><${Icon} name="folder-open" size=${15} /> ${t('Projects')}</button>
      <button class="btn ghost" onClick=${() => setDlg({ type: 'package' })} title=${t('Export the project as a .zip, collect its files')}><${Icon} name="layers" size=${15} /> ${t('Package')}</button>
      <button class="btn ghost" onClick=${() => setDlg({ type: 'exports' })} title=${t('Exported videos: play, download')}><${Icon} name="film" size=${15} /> ${t('Exports')}</button>
      <button class="btn ghost" onClick=${() => setDlg({ type: 'images' })} title=${t('Generate images with AI: backgrounds, edits')}><${Icon} name="image" size=${15} /> ${t('Images')}</button>
      <${LangSelect} />
      <button class="btn primary" disabled=${jobRunning && job.kind === 'export'} onClick=${() => setDlg({ type: 'export' })}><${Icon} name="film" size=${15} /> ${t('Export')}</button>
    </div>

    <div class="tracks">
      <div class="label-sm">${t('Project')}</div>
      <div class=${'trk' + (sel.kind === 'output' ? ' on' : '')} onClick=${() => setSel({ kind: 'output' })}>
        <${Icon} name="settings" size=${15} /><span class="lbl">${t('Settings')}</span><span class="n">${meta.width}×${meta.height}</span></div>
      ${ZONES.map((z) => {
        const folded = view.collapsed.includes(z.id);
        const ids = zoneIds[z.id];
        const nHidden = ids.filter((id) => view.hidden.includes(id)).length;
        return html`<div class=${'zone-h z-' + z.id} onClick=${() => toggleView('collapsed', z.id)} onContextMenu=${(e) => onContext(e, { kind: 'zone', id: z.id })}
            title=${t(folded ? 'Click to unfold' : 'Click to fold')}>
          <${Icon} name=${folded ? 'chevron-right' : 'chevron-down'} size=${13} />${t(z.label)}
          ${nHidden > 0 && html`<span class="n">${t(nHidden === 1 ? '1 hidden' : '{n} hidden', { n: nHidden })}</span>`}</div>
        ${!folded && html`<div class=${'zone-b z-' + z.id}>
          ${z.id === 'main' && html`<div class=${'trk' + (sel.kind === 'audio' ? ' on' : '')} onClick=${() => setSel({ kind: 'audio' })}
              title=${(proj.audio.path || t('No audio')) + ' · ' + t('Always at the top of the timeline')}>
            <${Icon} name="music" size=${15} /><span class="lbl">${t('Audio')}</span>${!proj.audio.path && html`<span class="badge">${t('empty')}</span>`}</div>`}
          ${z.id !== 'layers' && ids.map((id) => id === 'texts' ? textsEntry() : trackEntry(trackById[id]))}
          ${z.id === 'layers' && layersFF.map(({ l, i }, k) => html`<div key=${l.id}
              class=${'trk lyrent' + (sel.kind === 'layer' && sel.id === l.id ? ' on' : '') + (view.hidden.includes('layer:' + l.id) ? ' hid' : '') + (layerDrag && layerDrag.from === k ? ' dragging' : '')}
              onMouseDown=${(e) => layerReorder(e, k)} onClick=${() => selectLayer(l.id)} onContextMenu=${(e) => onContext(e, { kind: 'layer', id: l.id })}
              title=${t('Drag to change the order (higher = in front)')}>
            ${layerDrag && layerDrag.to === k && layerDrag.to < layerDrag.from && html`<i class="dropline top"></i>`}
            <${Icon} name=${isEmitterLayer(l) ? 'sparkles' : 'layers'} size=${15} /><span class="lbl">${layerName(l, i)}</span>
            ${l.clips.length > 0 && html`<span class="badge" title=${t('Clips')}>${l.clips.length}</span>`}${eye('layer:' + l.id)}
            ${layerDrag && layerDrag.to === k && layerDrag.to > layerDrag.from && html`<i class="dropline bottom"></i>`}</div>`)}
          ${z.id === 'layers' && html`<button class="btn sm ghost addlayer" onClick=${(e) => openMenu(e, [
              { label: t('Image, GIF or video…'), icon: 'image', onClick: () => ctx.addLayer(time) },
              '-', { header: t('Particle emitter') }, ...emitterMenuItems((k) => ctx.addEmitter(time, k)),
            ])}
            title=${t('New layer in front of the others: an image, GIF or video, or a particle emitter')}><${Icon} name="plus" size=${14} /> ${t('Layer')}</button>`}
        </div>`}`;
      })}
    </div>

    <div class="preview">
      <div class="stage-wrap">
        <${Preview} meta=${meta} timeStore=${timeStore} scale=${scale} rev=${rev} playing=${playing} view=${previewView}
          cursors=${showCursors ? peerCursors(peers, cursors, 'preview', meRef.current) : []} onPointer=${(c) => sendPresence({ cursor: c })}
          pin=${selText ? { x: selText.position_x, y: selText.position_y } : null}
          points=${burnSel ? (burnSel.points || []).map((q) => ({ x: q[0], y: q[1] })) : null}
          onPoints=${(pts) => update((p) => { const tr = p.tracks.background[sel.seg].transition; tr.burn = { ...(tr.burn || {}), points: pts.map((q) => [q.x, q.y]) }; })}
          onPin=${(x, y) => update((p) => { const tx = p.texts[sel.i]; if (tx) setClipProps(tx, { position_x: x, position_y: y }, timeStore.get()); })}
          text=${selText} textIdx=${selText ? sel.i : null}
          onText=${(props) => update((p) => { const tx = p.texts[sel.i]; if (tx) setClipProps(tx, props, timeStore.get()); })}
          clip=${selClip && (selClip.path || selEmit) && time >= selClip.start && time < selClip.end ? selClip : null} emitter=${selEmit}
          clipRef=${sel.kind === 'layer' ? { id: sel.id, clip: sel.clip } : null}
          onClip=${(props) => update((p) => { const c = p.layers.find((l) => l.id === sel.id).clips[sel.clip]; setClipProps(c, props, timeStore.get()); })} />
        ${!hideSetup && (!proj.audio.path || !proj.tracks.background[0].cfg.path) && html`<${Setup} proj=${proj} ctx=${ctx} onHide=${() => setHideSetup(true)} />`}
      </div>
      <div class="transport">
        <div class="btngroup">
          <button class="btn icon" title=${t('Go to start (Home)')} onClick=${() => seek(0)}><${Icon} name="skip-back" size=${15} /></button>
          <button class="btn icon" title=${t('Previous background cut: start or end of a segment (PgUp)')} onClick=${() => seekCut(-1)}><${Icon} name="chevron-first" size=${16} /></button>
          <${HoldBtn} speed=${3} dir=${-1} start=${shuttleStart} stop=${shuttleStop} />
          <${HoldBtn} speed=${2} dir=${-1} start=${shuttleStart} stop=${shuttleStop} />
          <button class="btn icon" title=${t('Previous frame (←)')} onClick=${() => seek(timeStore.get() - 1 / meta.fps)}><${Icon} name="chevron-left" size=${16} /></button>
        </div>
        <button class="btn primary play" title=${t('Play / pause (Space)')} onClick=${() => setPlaying(!playing)}>
          <${Icon} name=${playing ? 'pause' : 'play'} size=${16} /></button>
        <div class="btngroup">
          <button class="btn icon" title=${t('Next frame (→)')} onClick=${() => seek(timeStore.get() + 1 / meta.fps)}><${Icon} name="chevron-right" size=${16} /></button>
          <${HoldBtn} speed=${2} dir=${1} start=${shuttleStart} stop=${shuttleStop} />
          <${HoldBtn} speed=${3} dir=${1} start=${shuttleStart} stop=${shuttleStop} />
          <button class="btn icon" title=${t('Next background cut: start or end of a segment (PgDn)')} onClick=${() => seekCut(1)}><${Icon} name="chevron-last" size=${16} /></button>
          <button class="btn icon" title=${t('Go to end (End)')} onClick=${() => seek(meta.duration)}><${Icon} name="skip-forward" size=${15} /></button>
        </div>
        <${Clock} timeStore=${timeStore} meta=${meta} />
        <button class=${'btn icon' + (scrub ? ' on' : '')} title=${t(scrub ? 'Audio while scrubbing: on (S)' : 'Audio while scrubbing: off (S)')}
          onClick=${() => setScrub(!scrub)}><${Icon} name=${scrub ? 'volume-2' : 'volume-x'} size=${15} /></button>
        <button class=${'btn sm icon' + (depthView ? ' on' : '')} title=${t('See the depth of the scene (white = near): to place clips and emitters in it')}
          onClick=${() => setDepthView(!depthView)}><${Icon} name="layers" size=${14} /></button>
        <button class="btn sm" title=${t('Editor: the preview over the whole window, with zoom, rotation, pen and touch (E)')} onClick=${() => setEditor(true)}>
          <${Icon} name="pen-tool" size=${14} /> ${t('Editor')}</button>
        <span class="spacer"></span>
        ${!meta.hasAudio && html`<button class="btn sm ghost notice" onClick=${() => ctx.pickFile('audio', ctx.setAudio)}><${Icon} name="triangle-alert" size=${14} />${t('No audio · choose…')}</button>`}
        <span class="faint" style="font-size:12px">${t('Preview')}</span>
        <${Select} cls="" value=${scale} title=${t('Preview resolution')} onChange=${(v) => setScale(+v)}
          options=${[[1, 'Full'], [2, '1/2'], [3, '1/3'], [4, '1/4']]} />
      </div>
    </div>

    <div class="inspector">${inspector}</div>

    <${Timeline} proj=${proj} meta=${meta} ana=${ana} lyr=${lyr} timeStore=${timeStore} slowTime=${time} playing=${playing}
      remote=${showCursors ? peerCursors(peers, cursors, 'timeline', meRef.current) : []} marks=${peerMarks(peers, cursors, proj, meRef.current)}
      onPointer=${(c) => sendPresence({ cursor: c })}
      texts=${proj.texts || []} onSelectText=${(i) => ctx.selectText(i)} onMoveText=${onMoveText} onAddText=${(tt) => ctx.addText(tt)}
      sel=${sel} lineSel=${lineSel} wordEdit=${wordEdit} height=${tlH} setHeight=${setTlH}
      onSeek=${seek} onSelect=${selTrack} onMoveSeg=${onMoveSeg} onAddSeg=${ctx.addSeg} onMarker=${onMarker}
      view=${view} onToggleZone=${(z) => toggleView('collapsed', z)} kfFocus=${kfFocus} setKfFocus=${setKfFocus} kfSel=${kfSel} setKfSel=${setKfSel} onContext=${onContext}
      layers=${layersFF} onSelectLayer=${selectLayer} onMoveClip=${onMoveClip}
      onClipToLayer=${(id, j, to) => { const p = projRef.current; if (sameKind(p.layers.find((x) => x.id === id), p.layers.find((x) => x.id === to))) clipToLayer(id, j, to, true); else onMoveClip(id, j, null, null, true); }} onAddClip=${(id, tt) => ctx.addClip(id, tt)} onAddLayer=${(tt) => ctx.addLayer(tt)}
      onKeys=${onKeys}
      onSelectLine=${selectLine} onLyrics=${(lines) => editLyrics((all) => { all.splice(0, all.length, ...lines); }, false)}
      onLyricsCommit=${() => update(() => {})} />
    ${editor && html`<${Editor} meta=${meta} timeStore=${timeStore} playing=${playing} setPlaying=${setPlaying} seek=${seek} rev=${rev}
      cursors=${showCursors ? peerCursors(peers, cursors, 'preview', meRef.current) : []} onPointer=${(c) => sendPresence({ cursor: c })}
      selClip=${selClip} selEmit=${selEmit} inspector=${inspector} onClose=${() => { setEditor(false); setEditorTool('select'); setPathWhat('motion'); }} marks=${editorMarks} onKeys=${onKeys} kf=${{ focus: kfFocus, setFocus: setKfFocus, sel: kfSel, setSel: setKfSel }}
      tool=${editorTool} selMask=${selMask} onNeedMask=${needMask} selId=${sel.id} selClipIdx=${sel.clip}
      maskOwner=${maskOwner} pathOwner=${pathOwner} editPath=${(fn) => pathOwner && pathOwner.edit((o) => (pathOwner.route ? setRoute : setPath)(o, fn))} onPathNode=${setPathNodeI} view=${previewView} editMask=${(fn) => maskOwner && maskOwner.edit((o) => { const tt = timeStore.get();
        setMaskShapes(o, selMask, tt, fn(maskShapes(o, selMask, tt))); })}
      onPainted=${onPainted} selText=${selText} selTextIdx=${selText ? sel.i : null}
      onText=${(props) => update((p) => { const tx = p.texts[sel.i]; if (tx) setClipProps(tx, props, timeStore.get()); })}
      onClip=${(props) => update((p) => { const c = p.layers.find((l) => l.id === sel.id).clips[sel.clip]; setClipProps(c, props, timeStore.get()); })} />`}
    ${dialogs}
    <${ContextMenu} /><${HelpDialog} /><${ExprDialog} />
    ${dragging && html`<div class="dropzone"><div><${Icon} name="upload" size=${28} />
      <div style="margin-top:10px;font-size:15px;font-weight:600">${t('Drop to add to the project')}</div>
      <div class="faint" style="margin-top:4px">${t('Audio, background image or video, lyrics (.lrc/.srt/.txt) or font')}</div></div></div>`}
  </div>`;
}

render(html`<${App} />`, document.getElementById('app'));
