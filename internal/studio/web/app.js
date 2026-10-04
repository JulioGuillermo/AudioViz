import { html, render, useState, useEffect, useRef } from './vendor/preact-htm.js';
import { TRACKS, AUDIO_FIELDS, OUTPUT_FIELDS, trackById, TRANSITIONS, SIMPLE_TRANSITIONS, DIRECTED, DIRECTIONS, TEXT_FIELDS } from './schema.js';
import { api, clone, fmtTime, parseTime, b64u8, basename, debounce } from './util.js';
import { Form, Select, Edit } from './fields.js';
import { Icon } from './icons.js';
import { Timeline, segSummary } from './timeline.js';
import { FilePicker, NewProject, ExportDialog, GenLyricsDialog, LyricsTextDialog, LYRIC_LANGS, STEP_LABEL, ProjectsDialog, PackageDialog, ExportsDialog, WorkspaceGrid, uploadPackage } from './dialogs.js';
import { createStore, useStore, useThrottledStore } from './store.js';
import { t, setLang, langStore, LANGS } from './i18n.js';
import { createScrubber } from './scrub.js';

// ------------------------------------------------------------------ preview

function Preview({ meta, timeStore, scale, rev, playing, pin, onPin }) {
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
  const pinDown = (e) => {
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
  let pinStyle = null;
  const shownPin = dragPin || pin;
  if (shownPin && canvas.current && stage.current) {
    const cr = canvas.current.getBoundingClientRect(), sr = stage.current.getBoundingClientRect();
    pinStyle = { left: cr.left - sr.left + shownPin.x * cr.width + 'px', top: cr.top - sr.top + shownPin.y * cr.height + 'px' };
  }
  // inflight: requests in progress; lat: average latency (ms); shownF: last drawn frame
  const st = useRef({ inflight: 0, lat: 40, shownKey: null, shownF: -1, shownSeq: 0, seq: 0, lastReq: '', lastT: 0 });
  const [info, setInfo] = useState({ ms: 0, fps: 0 });
  const fpsWin = useRef([]);

  const draw = (bmp) => {
    const c = canvas.current;
    if (!c) return;
    if (c.width !== bmp.width || c.height !== bmp.height) { c.width = bmp.width; c.height = bmp.height; }
    c.getContext('2d').drawImage(bmp, 0, 0);
    bmp.close && bmp.close();
  };

  const request = (f, key) => {
    const s = st.current;
    const seq = ++s.seq;
    s.inflight++;
    s.lastReq = key;
    const t0 = performance.now();
    fetch(`/api/frame?f=${f}&scale=${scale}`)
      .then((r) => { if (!r.ok) throw new Error(r.statusText); return r.blob(); })
      .then((b) => createImageBitmap(b))
      .then((bmp) => {
        s.lat = s.lat * 0.8 + (performance.now() - t0) * 0.2;
        // Only draw if it is newer than what is on screen
        if (seq > s.shownSeq) { draw(bmp); s.shownSeq = seq; s.shownF = f; s.shownKey = key; }
        const now = performance.now();
        fpsWin.current = [...fpsWin.current.filter((x) => now - x < 1000), now];
        setInfo({ ms: Math.round(s.lat), fps: fpsWin.current.length });
      })
      .catch(() => {})
      .finally(() => { s.inflight--; });
  };

  useEffect(() => {
    const s = st.current;
    const fps = meta.fps;
    // A jump (seeking while playing) restarts the frame sequence, otherwise a jump backwards
    // would never be requested
    if (time < s.lastT - 0.05 || time > s.lastT + 1.0) { s.shownF = -1; s.lastReq = ''; }
    s.lastT = time;
    if (playing) {
      // Request ahead by the measured latency, with several requests in flight
      const f = Math.min(meta.frames - 1, Math.round((time + s.lat / 1000) * fps));
      const key = `${f}|${scale}|${rev}`;
      if (s.inflight < 3 && key !== s.lastReq && f > s.shownF) request(f, key);
      return;
    }
    const f = Math.min(meta.frames - 1, Math.max(0, Math.round(time * fps)));
    const key = `${f}|${scale}|${rev}`;
    if (key === s.shownKey) return;
    if (s.inflight === 0) { request(f, key); return; }
    // One in progress: retry when it finishes
    const id = setInterval(() => {
      if (st.current.inflight === 0) { clearInterval(id); if (st.current.shownKey !== key) request(f, key); }
    }, 15);
    return () => clearInterval(id);
  }, [time, scale, rev, meta.frames, playing]);

  useEffect(() => { if (!playing) st.current.shownF = -1; }, [playing]);

  return html`<div class="stage" ref=${stage}>
    <canvas ref=${canvas} width=${Math.round(meta.width / scale)} height=${Math.round(meta.height / scale)}
      style=${{ width: box ? box.w + 'px' : '', height: box ? box.h + 'px' : '', cursor: pin ? 'crosshair' : '' }} onMouseDown=${pinDown}
      title=${pin ? t('Drag to place the selected text') : ''}></canvas>
    ${pinStyle && html`<div class=${'pin' + (dragPin ? ' drag' : '')} style=${pinStyle}></div>`}
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

function SegHeader({ segs, i, set, meta, track }) {
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
      <div class="card">
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
        <div class="card-note">${t('Drag on the preview to place it. In and out animations happen inside this time.')}</div>
      </div>
      <${Form} fields=${TEXT_FIELDS} value=${x} onChange=${set} ctx=${ctx} />`}
    </div>
  </div>`;
}

function TrackInspector({ proj, track, segIdx, time, meta, update, setSel, ctx, onGenLyrics, onEditText, hasLines }) {
  const def = trackById[track];
  const segs = proj.tracks[track];
  const i = Math.min(segIdx, segs.length - 1);
  const seg = segs[i];
  const setSeg = (ns) => update((p) => { p.tracks[track][i] = ns; });
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
      <${SegHeader} segs=${segs} i=${i} set=${setSeg} meta=${meta} track=${track} />
      <${Form} fields=${def.fields} value=${seg.cfg} onChange=${(cfg) => setSeg({ ...seg, cfg })} ctx=${ctx} isFirst=${i === 0} />
      ${track === 'lyrics' && html`<div class="section"><div class="sh">${t('Generate lyrics')}</div><div class="sb">
        <p class="faint" style="margin:2px 0 8px;font-size:12px">${t('Creates an .lrc with the timing of every word, from the lyrics as text or transcribing the song.')}</p>
        <div class="row"><button class="btn" onClick=${onGenLyrics}><${Icon} name="wand-sparkles" size=${15} /> ${t('Generate synced lyrics…')}</button>
        ${hasLines && html`<button class="btn" title=${t('Correct the whole lyrics as text and align it again')} onClick=${onEditText}><${Icon} name="pencil" size=${14} /> ${t('Edit text')}</button>`}</div></div></div>`}
    </div>
  </div>`;
}

function LineEditor({ lyr, lines, time, onChange, seek, saveState, onResync, resyncing, lyricLang, setLyricLang, wordEdit, setWordEdit, onEditText }) {
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
      </div>
      ${!multi && html`<div class="row" style="margin-top:8px">
        <button class="btn sm" disabled=${splitAt < 0} title=${t('Split the line before the first word after the cursor')} onClick=${() => split(splitAt)}>
          <${Icon} name="scissors" size=${13} /> ${t('Split at cursor')}</button>
        <button class="btn sm" disabled=${!next} title=${t('Join this line with the next one')} onClick=${join}><${Icon} name="merge" size=${13} /> ${t('Join next')}</button>
        <button class="btn sm" title=${t('Correct the whole lyrics as text and align it again')} onClick=${onEditText}><${Icon} name="file-text" size=${13} /> ${t('Edit text')}</button>
      </div>`}
    </div>
    ${multi ? html`<div class="insp-b"><p class="faint" style="font-size:12px">${t('Several lines selected (Shift+click). Drag them together on the timeline, shift them with the buttons, or resync them.')}</p></div>`
      : html`<div class="insp-b"><div class="words">
      <div class="word head"><span>${t('Word')}</span><span>${t('Start')}</span><span>${t('End')}</span><span></span></div>
      ${line.words.map((w, k) => html`<div class=${'word' + (time >= w.start && time < w.end ? ' now' : '')}>
        <${Edit} type="text" value=${w.text} onChange=${(e) => setWord(k, 'text', e.target.value)} />
        <${Edit} type="number" step="0.01" value=${w.start.toFixed(2)} onChange=${(e) => setWord(k, 'start', +e.target.value)} />
        <${Edit} type="number" step="0.01" value=${w.end.toFixed(2)} onChange=${(e) => setWord(k, 'end', +e.target.value)} />
        <span class="row" style="gap:2px"><button class="btn icon" title=${t('Set the start at the cursor')} onClick=${() => setWord(k, 'start', +time.toFixed(2))}><${Icon} name="crosshair" size=${14} /></button>
        ${k > 0 && html`<button class="btn icon" title=${t('Start a new line at this word')} onClick=${() => split(k)}><${Icon} name="scissors" size=${14} /></button>`}</span>
      </div>`)}
    </div>
    <p class="faint" style="font-size:12px">${t('Tip: with Edit words, drag the words (or their edges) on the timeline; or play, pause right when a word starts and press the crosshair button. If a line is far off, drag it roughly into place and press Resync.')}</p></div>`}
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

function App() {
  useStore(langStore); // re-render everything when the language changes
  const [st, setSt] = useState(null);
  const [proj, setProj] = useState(null);
  const [meta, setMeta] = useState(null);
  const [ana, setAna] = useState(null);
  const [lyr, setLyr] = useState({ lines: [], path: '' });
  const [lyrSave, setLyrSave] = useState('');
  const [sel, setSel] = useState({ kind: 'track', track: 'visualizer', seg: 0 });
  const [lineSel, setLineSel] = useState([]);
  const [wordEdit, setWordEdit] = useState(false); // word blocks of the selected line on the timeline
  const [playing, setPlaying] = useState(false);
  const [scale, setScale] = useState(2);
  const [rev, setRev] = useState(0);
  const [save, setSave] = useState({ kind: '', at: '' });
  const [job, setJob] = useState(null);
  const [dlg, setDlg] = useState(null);
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
  const histBase = () => { if (!hist.current.cur) hist.current.cur = { proj: projRef.current, lyr: lyrRef.current }; };
  const record = () => {
    const h = hist.current;
    const snap = { proj: projRef.current, lyr: lyrRef.current };
    if (!h.cur) { h.cur = snap; return; }
    if (snap.proj === h.cur.proj && snap.lyr === h.cur.lyr) return;
    const now = performance.now();
    if (now - h.at > 600) {
      h.past.push(h.cur);
      if (h.past.length > 32) h.past.shift();
    }
    h.at = now;
    h.cur = snap;
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
    projRef.current = d.project;
    setProj(d.project);
    hist.current = { past: [], future: [], cur: null, at: 0 }; // a (re)opened project starts a new history
    setHistTick((x) => x + 1);
    setMeta(d.meta);
    loadAudio(d.project);
    loadAna();
    loadLyrics();
    setRev((r) => r + 1);
  };
  const loadAna = async () => {
    try {
      const a = await api('GET', '/api/analysis');
      setAna({ ...a, bass: b64u8(a.bass), mid: b64u8(a.mid), treble: b64u8(a.treble), rms: b64u8(a.rms), wave: b64u8(a.wave) });
    } catch (e) { showErr(e); }
  };
  const loadLyrics = async () => {
    let l;
    try { l = await api('GET', '/api/lyrics'); } catch (e) { l = { lines: [], path: '' }; showErr(e); }
    lyrRef.current = l;
    setLyr(l);
    setLineSel([]);
    // A (re)loaded file is the current state, not a change of its own
    if (hist.current.cur) hist.current.cur = { ...hist.current.cur, lyr: l };
  };
  const loadAudio = (p) => {
    const a = audio.current;
    const src = p.audio.path ? '/api/media?path=' + encodeURIComponent(p.audio.path) : '';
    if (a.dataset.src !== src) { a.src = src; a.dataset.src = src; a.load(); }
    scrubber.current.setSource(src);
    if (scrubRef.current && src) scrubber.current.preload();
  };

  useEffect(() => {
    loadAll().catch(showErr);
    const es = new EventSource('/api/events');
    es.onmessage = (ev) => {
      const m = JSON.parse(ev.data);
      if (m.type === 'job') {
        setJob(m.data);
        if (m.data.kind === 'lyrics' && m.data.status === 'done') onLyricsGenerated(m.data.output);
      }
    };
    return () => es.close();
  }, []);

  // --- saving the project (debounced to group fast changes)
  const pushRef = useRef(debounce(async (p, oldAudio, oldLyr) => {
    setSave({ kind: 'saving' });
    try {
      const r = await api('PUT', '/api/project', p);
      setMeta(r.meta);
      setSave({ kind: 'saved', at: r.saved });
      if (p.audio.path !== oldAudio) { loadAna(); loadAudio(p); }
      if (p.tracks.lyrics[0].cfg.path !== oldLyr) loadLyrics();
      setRev((x) => x + 1);
    } catch (e) { setSave({ kind: 'error' }); showErr(e); }
  }, 250));

  const update = (fn) => {
    histBase();
    const old = projRef.current;
    const p = clone(old);
    fn(p);
    setProj(p);
    projRef.current = p;
    pushRef.current(p, old.audio.path, old.tracks.lyrics[0].cfg.path);
    record();
  };

  // Restores a snapshot (undo/redo): saves the project, and the lyrics if they differ
  const applySnap = (snap) => {
    const old = projRef.current;
    projRef.current = snap.proj;
    setProj(snap.proj);
    pushRef.current(snap.proj, old.audio.path, old.tracks.lyrics[0].cfg.path);
    if (snap.lyr !== lyrRef.current) {
      lyrRef.current = snap.lyr;
      setLyr(snap.lyr);
      lyrSaveRef.current();
    }
    // Keep the selection valid
    setSel((s) => {
      if (s.kind === 'track') { const n = snap.proj.tracks[s.track].length; return s.seg >= n ? { ...s, seg: n - 1 } : s; }
      if (s.kind === 'text') return s.i >= snap.proj.texts.length ? { ...s, i: snap.proj.texts.length - 1 } : s;
      return s;
    });
    setLineSel((ls) => ls.filter((i) => i < snap.lyr.lines.length));
  };
  const undo = () => {
    const h = hist.current;
    if (!h.past.length) return;
    h.future.push(h.cur);
    h.cur = h.past.pop();
    h.at = 0;
    applySnap(h.cur);
    setHistTick((x) => x + 1);
  };
  const redo = () => {
    const h = hist.current;
    if (!h.future.length) return;
    h.past.push(h.cur);
    h.cur = h.future.pop();
    h.at = 0;
    applySnap(h.cur);
    setHistTick((x) => x + 1);
  };

  // --- saving the lyrics (edits on the timeline / inspector are saved automatically)
  const lyrSaveRef = useRef(debounce(async () => {
    setLyrSave('saving');
    try {
      await api('PUT', '/api/lyrics', { lines: lyrRef.current.lines, path: lyrRef.current.path });
      await api('PUT', '/api/project', projRef.current); // reloads the lyrics in the session
      setLyrSave('saved');
      setRev((x) => x + 1);
    } catch (e) { setLyrSave('error'); showErr(e); }
  }, 600));
  const editLyrics = (fn, save = true) => {
    histBase();
    const n = clone(lyrRef.current);
    fn(n.lines);
    lyrRef.current = n;
    setLyr(n);
    if (save) { lyrSaveRef.current(); record(); } // save = false: a drag, recorded when released
  };

  // --- transport
  const seek = (tt) => {
    const d = meta ? meta.duration : 0;
    tt = Math.max(0, Math.min(d, tt));
    timeStore.set(tt);
    if (playing && audio.current.src) audio.current.currentTime = tt;
    else if (!playing && scrubRef.current) scrubber.current.play(tt);
  };
  useEffect(() => {
    if (!playing) return;
    const a = audio.current;
    let raf;
    scrubber.current.stop();
    const base = performance.now(), t0 = timeStore.get();
    if (a.src) { a.currentTime = t0; a.play().catch(showErr); }
    const tick = () => {
      const tt = a.src ? a.currentTime : t0 + (performance.now() - base) / 1000;
      if (meta && tt >= meta.duration) { setPlaying(false); return; }
      timeStore.set(tt);
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => { cancelAnimationFrame(raf); a.pause(); };
  }, [playing]);

  // --- actions
  const ctx = {
    seek,
    dialog: setDlg,
    pickFile: (kind, cb, start) => setPick({ kind, cb, start }),
    openProject: async (p) => { try { await api('POST', '/api/project/open', { path: p }); await loadAll(); } catch (e) { showErr(e); } },
    importPackage: async (f) => { try { await uploadPackage(f); await loadAll(); } catch (e) { showErr(e); } },
    setAudio: (path) => update((p) => { p.audio.path = path; }),
    // The background goes to the segment active at the cursor (or the first one if still empty)
    setBackground: (path) => update((p) => {
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
    try { localStorage.setItem('audioviz.lastGen', JSON.stringify({ lang: o.lang, fast: !!o.fast })); } catch (e) { /* ignore */ }
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

  const onMarker = (op, i) => {
    if (op === 'add') {
      const label = prompt(t('Marker name (e.g. Chorus):'), t('Marker'));
      if (label === null) return;
      update((p) => { p.markers.push({ at: +timeStore.get().toFixed(3), label }); p.markers.sort((a, b) => a.at - b.at); });
    } else if (op === 'rename') {
      const label = prompt(t('New name:'), projRef.current.markers[i].label);
      if (label !== null) update((p) => { p.markers[i].label = label; });
    } else if (op === 'delete') {
      if (confirm(t('Delete the marker “{m}”?', { m: projRef.current.markers[i].label }))) update((p) => { p.markers.splice(i, 1); });
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
      const r = await api('POST', '/api/lyrics/resync', { lines: before, targets: lineSel, lang: lyricLang });
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
      else if (e.key === 'm' || e.key === 'M') onMarker('add');
      else if (e.key === 's' || e.key === 'S') setScrub(!scrubRef.current);
      else if (e.key === 'Escape') setWordEdit(false);
      else if ((e.key === 'n' || e.key === 'N') && sel.kind === 'track') ctx.addSeg(sel.track, now);
      else if (e.key === 'Delete' && sel.kind === 'track' && sel.seg > 0) {
        update((p) => { p.tracks[sel.track].splice(sel.seg, 1); });
        setSel({ ...sel, seg: sel.seg - 1 });
      } else if (e.key === 'Delete' && sel.kind === 'text' && projRef.current.texts[sel.i]) {
        update((p) => { p.texts.splice(sel.i, 1); });
        setSel({ kind: 'text', i: Math.min(sel.i, projRef.current.texts.length - 1) });
      }
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, [meta, sel, playing]);

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
        onAlign=${(text, lang) => { genLyrics({ text, lang, fast: lastGen().fast }); setDlg({ type: 'genlyrics' }); }} />`}
    ${pick && html`<${FilePicker} kind=${pick.kind} start=${pick.start} hasProject=${!!proj}
        onPick=${pick.cb} onClose=${() => setPick(null)} />`}
    ${toast && html`<div class="toast" onClick=${() => setToast('')}><${Icon} name="triangle-alert" size=${16} />${toast}</div>`}`;

  if (!proj || !meta) return html`<${Welcome} st=${st} ctx=${ctx} />${dialogs}`;

  const groups = [...new Set(TRACKS.map((tr) => tr.group))];
  const selTrack = (track, seg) => {
    const segs = proj.tracks[track];
    if (seg === null) { seg = 0; segs.forEach((s, k) => { if (s.at <= timeStore.get()) seg = k; }); }
    setSel({ kind: 'track', track, seg });
    setLineSel([]);
  };
  const jobRunning = job && job.status === 'running';
  const saveText = { saving: t('Saving…'), saved: t('Saved {t}', { t: save.at }), error: t('Save error') }[save.kind] || '';

  const selText = sel.kind === 'text' ? (proj.texts || [])[sel.i] : null;
  let inspector;
  if (sel.kind === 'audio') inspector = html`<div><div class="insp-h"><div class="insp-title"><${Icon} name="music" size=${16} cls="muted" />${t('Audio')}<span class="sub">${t('Project')}</span></div></div><div class="insp-b">
      <${Form} fields=${AUDIO_FIELDS} value=${proj.audio} onChange=${(v) => update((p) => { p.audio = v; })} ctx=${ctx} /></div></div>`;
  else if (sel.kind === 'output') inspector = html`<div><div class="insp-h"><div class="insp-title"><${Icon} name="monitor" size=${16} cls="muted" />${t('Output')}<span class="sub">${t('Project')}</span></div></div><div class="insp-b">
      <${Form} fields=${OUTPUT_FIELDS} value=${proj.output} onChange=${(v) => update((p) => { p.output = v; })} ctx=${ctx} />
      <p class="faint" style="font-size:12px">${t('Current resolution')}: ${meta.width}×${meta.height} · ${meta.fps} fps · ${fmtTime(meta.duration)}</p></div></div>`;
  else if (sel.kind === 'text') inspector = html`<${TextsInspector} proj=${proj} sel=${sel} setSel=${setSel} update=${update} time=${time} meta=${meta} ctx=${ctx} />`;
  else if (sel.kind === 'line') inspector = html`<${LineEditor} lyr=${lyr} lines=${lineSel} time=${time} seek=${seek} saveState=${lyrSave}
      onChange=${(fn) => editLyrics(fn)} onResync=${resync} resyncing=${resyncing} lyricLang=${lyricLang} setLyricLang=${setLyricLang} wordEdit=${wordEdit} setWordEdit=${setWordEdit}
      onEditText=${() => setDlg({ type: 'lyrtext' })} />`;
  else inspector = html`<${TrackInspector} proj=${proj} track=${sel.track} segIdx=${sel.seg} time=${time} meta=${meta}
      update=${update} setSel=${setSel} ctx=${ctx} onGenLyrics=${() => setDlg({ type: 'genlyrics' })}
      onEditText=${() => setDlg({ type: 'lyrtext' })} hasLines=${lyr.lines.length > 0} />`;

  return html`<div class="app" style=${{ '--tl-h': tlH + 'px', '--left-w': layout.left + 'px', '--right-w': layout.right + 'px' }}>
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
      ${job && html`<div class="jobbar" title=${job.message} onClick=${() => setDlg({ type: job.kind === 'lyrics' ? 'genlyrics' : 'export' })}>
        <${Icon} name=${job.status === 'running' ? (job.kind === 'export' ? 'film' : 'wand-sparkles') : job.status === 'done' ? 'check' : 'triangle-alert'} size=${14}
          cls=${{ done: 'st-ok', error: 'st-err', canceled: 'st-warn' }[job.status] || 'muted'} />
        <div class="progress"><i style=${{ width: (job.progress * 100).toFixed(1) + '%' }}></i></div>
        <span class="msg">${job.gen && job.status === 'running'
          ? `${t(STEP_LABEL[job.gen.steps[job.gen.current].id])} · ${Math.round(job.gen.steps[job.gen.current].progress * 100)}%`
          : job.export && job.status === 'running'
            ? `${t(job.export.stage === 'mux' ? 'Writing the MP4' : 'Exporting')} · ${Math.round(job.progress * 100)}%` : job.message}</span>
      </div>`}
      <div class="btngroup">
        <button class="btn icon ghost" disabled=${!hist.current.past.length} title=${t('Undo (Ctrl+Z)')} onClick=${undo}><${Icon} name="undo-2" size=${15} /></button>
        <button class="btn icon ghost" disabled=${!hist.current.future.length} title=${t('Redo (Ctrl+Y)')} onClick=${redo}><${Icon} name="redo-2" size=${15} /></button>
      </div>
      <button class="btn ghost" onClick=${() => setDlg({ type: 'projects' })} title=${t('Your projects: open, new, import')}><${Icon} name="folder-open" size=${15} /> ${t('Projects')}</button>
      <button class="btn ghost" onClick=${() => setDlg({ type: 'package' })} title=${t('Export the project as a .zip, collect its files')}><${Icon} name="layers" size=${15} /> ${t('Package')}</button>
      <button class="btn ghost" onClick=${() => setDlg({ type: 'exports' })} title=${t('Exported videos: play, download')}><${Icon} name="film" size=${15} /> ${t('Exports')}</button>
      <${LangSelect} />
      <button class="btn primary" disabled=${jobRunning && job.kind === 'export'} onClick=${() => setDlg({ type: 'export' })}><${Icon} name="film" size=${15} /> ${t('Export')}</button>
    </div>

    <div class="tracks">
      <div class="label-sm">${t('Project')}</div>
      <div class=${'trk' + (sel.kind === 'audio' ? ' on' : '')} onClick=${() => setSel({ kind: 'audio' })} title=${proj.audio.path || t('No audio')}>
        <${Icon} name="music" size=${15} /><span class="lbl">${t('Audio')}</span>${!proj.audio.path && html`<span class="badge">${t('empty')}</span>`}</div>
      <div class=${'trk' + (sel.kind === 'output' ? ' on' : '')} onClick=${() => setSel({ kind: 'output' })}>
        <${Icon} name="monitor" size=${15} /><span class="lbl">${t('Output')}</span><span class="n">${meta.width}×${meta.height}</span></div>
      ${groups.map((g) => html`<div class="label-sm">${t(g)}</div>${TRACKS.filter((tr) => tr.group === g).map((tr) => {
        const segs = proj.tracks[tr.id];
        let cur = 0;
        segs.forEach((s, k) => { if (s.at <= time) cur = k; });
        const c = segs[cur].cfg;
        const on = tr.id === 'background' ? !!c.path : c.enabled !== false;
        return html`<div class=${'trk' + (sel.kind === 'track' && sel.track === tr.id ? ' on' : '') + (on ? '' : ' off')}
            title=${segSummary(tr.id, c)} onClick=${() => selTrack(tr.id, null)}>
          <${Icon} name=${tr.icon} size=${15} /><span class="lbl">${t(tr.label)}</span>
          ${segs.length > 1 && html`<span class="badge" title=${t('Number of segments')}>${segs.length}</span>`}</div>`;
      })}`)}
      <div class="label-sm">${t('Texts')}</div>
      <div class=${'trk' + (sel.kind === 'text' ? ' on' : '')} onClick=${() => ctx.selectText((proj.texts || []).length ? 0 : -1)}>
        <${Icon} name="type" size=${15} /><span class="lbl">${t('Titles and texts')}</span>
        ${(proj.texts || []).length > 0 && html`<span class="badge">${proj.texts.length}</span>`}</div>
    </div>

    <div class="preview">
      <div class="stage-wrap">
        <${Preview} meta=${meta} timeStore=${timeStore} scale=${scale} rev=${rev} playing=${playing}
          pin=${selText ? { x: selText.position_x, y: selText.position_y } : null}
          onPin=${(x, y) => update((p) => { const tx = p.texts[sel.i]; if (tx) { tx.position_x = x; tx.position_y = y; } })} />
        ${!hideSetup && (!proj.audio.path || !proj.tracks.background[0].cfg.path) && html`<${Setup} proj=${proj} ctx=${ctx} onHide=${() => setHideSetup(true)} />`}
      </div>
      <div class="transport">
        <div class="btngroup">
          <button class="btn icon" title=${t('Go to start (Home)')} onClick=${() => seek(0)}><${Icon} name="skip-back" size=${15} /></button>
          <button class="btn icon" title=${t('Previous frame (←)')} onClick=${() => seek(timeStore.get() - 1 / meta.fps)}><${Icon} name="chevron-left" size=${16} /></button>
        </div>
        <button class="btn primary play" title=${t('Play / pause (Space)')} onClick=${() => setPlaying(!playing)}>
          <${Icon} name=${playing ? 'pause' : 'play'} size=${16} /></button>
        <div class="btngroup">
          <button class="btn icon" title=${t('Next frame (→)')} onClick=${() => seek(timeStore.get() + 1 / meta.fps)}><${Icon} name="chevron-right" size=${16} /></button>
          <button class="btn icon" title=${t('Go to end (End)')} onClick=${() => seek(meta.duration)}><${Icon} name="skip-forward" size=${15} /></button>
        </div>
        <${Clock} timeStore=${timeStore} meta=${meta} />
        <button class=${'btn icon' + (scrub ? ' on' : '')} title=${t(scrub ? 'Audio while scrubbing: on (S)' : 'Audio while scrubbing: off (S)')}
          onClick=${() => setScrub(!scrub)}><${Icon} name=${scrub ? 'volume-2' : 'volume-x'} size=${15} /></button>
        <span class="spacer"></span>
        ${!meta.hasAudio && html`<button class="btn sm ghost notice" onClick=${() => ctx.pickFile('audio', ctx.setAudio)}><${Icon} name="triangle-alert" size=${14} />${t('No audio · choose…')}</button>`}
        <span class="faint" style="font-size:12px">${t('Preview')}</span>
        <${Select} cls="" value=${scale} title=${t('Preview resolution')} onChange=${(v) => setScale(+v)}
          options=${[[1, 'Full'], [2, '1/2'], [3, '1/3'], [4, '1/4']]} />
      </div>
    </div>

    <div class="inspector">${inspector}</div>

    <${Timeline} proj=${proj} meta=${meta} ana=${ana} lyr=${lyr} timeStore=${timeStore} slowTime=${time} playing=${playing}
      texts=${proj.texts || []} onSelectText=${(i) => ctx.selectText(i)} onMoveText=${onMoveText} onAddText=${(tt) => ctx.addText(tt)}
      sel=${sel} lineSel=${lineSel} wordEdit=${wordEdit} height=${tlH} setHeight=${setTlH}
      onSeek=${seek} onSelect=${selTrack} onMoveSeg=${onMoveSeg} onAddSeg=${ctx.addSeg} onMarker=${onMarker}
      onSelectLine=${selectLine} onLyrics=${(lines) => editLyrics((all) => { all.splice(0, all.length, ...lines); }, false)}
      onLyricsCommit=${() => { lyrSaveRef.current(); record(); }} />
    ${dialogs}
    ${dragging && html`<div class="dropzone"><div><${Icon} name="upload" size=${28} />
      <div style="margin-top:10px;font-size:15px;font-weight:600">${t('Drop to add to the project')}</div>
      <div class="faint" style="margin-top:4px">${t('Audio, background image or video, lyrics (.lrc/.srt/.txt) or font')}</div></div></div>`}
  </div>`;
}

render(html`<${App} />`, document.getElementById('app'));
