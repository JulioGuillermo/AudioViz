import { html, render, useState, useEffect, useRef } from './vendor/preact-htm.js';
import { TRACKS, AUDIO_FIELDS, OUTPUT_FIELDS, trackById } from './schema.js';
import { api, clone, fmtTime, parseTime, b64u8, basename, debounce } from './util.js';
import { Form, Select } from './fields.js';
import { Icon } from './icons.js';
import { Timeline, segSummary } from './timeline.js';
import { FilePicker, NewProject, ExportDialog, GenLyricsDialog, LYRIC_LANGS, STEP_LABEL } from './dialogs.js';
import { createStore, useStore, useThrottledStore } from './store.js';
import { t, setLang, langStore, LANGS } from './i18n.js';
import { createScrubber } from './scrub.js';

// ------------------------------------------------------------------ preview

function Preview({ meta, timeStore, scale, rev, playing }) {
  const time = useStore(timeStore);
  const canvas = useRef();
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

  return html`<div class="stage">
    <canvas ref=${canvas} width=${Math.round(meta.width / scale)} height=${Math.round(meta.height / scale)}
      style=${{ aspectRatio: `${meta.width}/${meta.height}` }}></canvas>
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
    { done: !!lyr.path, optional: true, title: 'Add the lyrics', text: 'An already synced .lrc / .srt, or generate it from a .txt with the lyrics.',
      btn: 'Choose .lrc / .srt…', act: () => ctx.pickFile('lyrics', ctx.setLyrics),
      btn2: 'Generate from .txt…', act2: () => ctx.dialog({ type: 'genlyrics' }) },
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

function SegHeader({ segs, i, set, meta }) {
  const s = segs[i];
  const name = html`<div class="fld"><label>${t('Name')}</label><div class="ctl"><input type="text" class="grow" value=${s.label}
      placeholder=${i === 0 ? t('Optional') : t('e.g. Chorus')} onChange=${(e) => set({ ...s, label: e.target.value })} /></div></div>`;
  if (i === 0) return html`<div class="card">${name}<div class="card-note">${t('First segment: applies from the start until the next change.')}</div></div>`;
  return html`<div class="card">
    ${name}
    <div class="fld"><label>${t('Starts at')}</label><div class="ctl">
      <input type="text" class="mono" style="width:96px" value=${fmtTime(s.at)}
        onChange=${(e) => set({ ...s, at: Math.max(0.01, Math.min(meta.duration - 0.01, parseTime(e.target.value))) })} />
      <span class="faint" style="font-size:11.5px">min:sec</span></div></div>
    <div class="fld"><label>${t('Transition')}</label><div class="ctl">
      <${Select} value=${s.transition.mode} options=${[['cut', 'Cut'], ['fade', 'Fade']]}
        onChange=${(m) => set({ ...s, transition: { ...s.transition, mode: m, duration: s.transition.duration || 1 } })} />
      ${s.transition.mode === 'fade' && html`<input type="number" step="0.1" min="0.05" value=${s.transition.duration} title=${t('Duration (s)')}
        onChange=${(e) => set({ ...s, transition: { ...s.transition, duration: Math.max(0.05, +e.target.value) } })} /><span class="faint">s</span>`}
    </div></div>
  </div>`;
}

function TrackInspector({ proj, track, segIdx, time, meta, update, setSel, ctx, onGenLyrics }) {
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
          <span class="mono">${fmtTime(s.at, false)}</span>${s.label || t('Segment {n}', { n: k + 1 })}</button>`)}</div>
      <div class="row">
        <button class="btn sm" onClick=${() => ctx.addSeg(track, time)} title=${t('New segment starting at the cursor (N)')}><${Icon} name="plus" size=${14} /> ${t('Segment at {t}', { t: fmtTime(time) })}</button>
        <span class="spacer"></span>
        <button class="btn sm ghost danger" disabled=${i === 0} title=${t('Delete this segment (Del)')}
          onClick=${() => { update((p) => { p.tracks[track].splice(i, 1); }); setSel({ kind: 'track', track, seg: i - 1 }); }}><${Icon} name="trash-2" size=${14} /> ${t('Delete')}</button>
      </div>
    </div>
    <div class="insp-b">
      <${SegHeader} segs=${segs} i=${i} set=${setSeg} meta=${meta} />
      <${Form} fields=${def.fields} value=${seg.cfg} onChange=${(cfg) => setSeg({ ...seg, cfg })} ctx=${ctx} isFirst=${i === 0} />
      ${track === 'lyrics' && html`<div class="section"><div class="sh">${t('Generate lyrics')}</div><div class="sb">
        <p class="faint" style="margin:2px 0 8px;font-size:12px">${t('Creates an .lrc with the timing of every word from the lyrics as text.')}</p>
        <button class="btn" onClick=${onGenLyrics}><${Icon} name="wand-sparkles" size=${15} /> ${t('Generate synced lyrics…')}</button></div></div>`}
    </div>
  </div>`;
}

function LineEditor({ lyr, lines, time, onChange, seek, saveState, onResync, resyncing, lyricLang, setLyricLang, wordEdit, setWordEdit }) {
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
    </div>
    ${multi ? html`<div class="insp-b"><p class="faint" style="font-size:12px">${t('Several lines selected (Shift+click). Drag them together on the timeline, shift them with the buttons, or resync them.')}</p></div>`
      : html`<div class="insp-b"><div class="words">
      <div class="word head"><span>${t('Word')}</span><span>${t('Start')}</span><span>${t('End')}</span><span></span></div>
      ${line.words.map((w, k) => html`<div class=${'word' + (time >= w.start && time < w.end ? ' now' : '')}>
        <input type="text" value=${w.text} onChange=${(e) => setWord(k, 'text', e.target.value)} />
        <input type="number" step="0.01" value=${w.start.toFixed(2)} onChange=${(e) => setWord(k, 'start', +e.target.value)} />
        <input type="number" step="0.01" value=${w.end.toFixed(2)} onChange=${(e) => setWord(k, 'end', +e.target.value)} />
        <button class="btn icon" title=${t('Set the start at the cursor')} onClick=${() => setWord(k, 'start', +time.toFixed(2))}><${Icon} name="crosshair" size=${14} /></button>
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
      <div class="card2" onClick=${() => ctx.pickFile('project', (p) => ctx.openProject(p))}><${Icon} name="folder-open" size=${20} />
        <h3>${t('Open project')}</h3><p>${t('Opens an existing project.yaml, including hand-written ones.')}</p></div>
    </div>
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
  const [tlH, setTlH] = useState(Math.round(innerHeight * 0.36));
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

  const setLyricLang = (l) => { setLyricLangState(l); try { localStorage.setItem('audioviz.lyricLang', l); } catch (e) { /* ignore */ } };
  const showErr = (e) => { setToast(String(e.message || e)); setTimeout(() => setToast(''), 6000); };

  const loadAll = async () => {
    const s = await api('GET', '/api/state');
    setSt(s);
    if (s.job) setJob(s.job);
    if (!s.project) { setProj(null); return; }
    const d = await api('GET', '/api/project');
    setProj(d.project);
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
    try { setLyr(await api('GET', '/api/lyrics')); setLineSel([]); } catch (e) { setLyr({ lines: [], path: '' }); showErr(e); }
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
    const old = projRef.current;
    const p = clone(old);
    fn(p);
    setProj(p);
    projRef.current = p;
    pushRef.current(p, old.audio.path, old.tracks.lyrics[0].cfg.path);
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
    const n = clone(lyrRef.current);
    fn(n.lines);
    lyrRef.current = n;
    setLyr(n);
    if (save) lyrSaveRef.current();
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
      if (e.target.matches('input, select, textarea')) return;
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
      }
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, [meta, sel, playing]);

  if (!st) return html`<div class="welcome"><p class="muted">${t('Loading…')}</p></div>`;

  const dialogs = html`
    ${dlg && dlg.type === 'new' && html`<${NewProject} start=${st.home || st.startDir} pickFile=${ctx.pickFile} onClose=${() => setDlg(null)}
        onCreate=${async (dir, name) => { await api('POST', '/api/project/new', { dir, name }); await loadAll(); }} />`}
    ${dlg && dlg.type === 'export' && proj && html`<${ExportDialog} proj=${proj} meta=${meta} job=${job} onClose=${() => setDlg(null)}
        onStart=${(o) => api('POST', '/api/export', o).then(setJob).catch(showErr)} onCancel=${() => api('POST', '/api/job/cancel')} />`}
    ${dlg && dlg.type === 'genlyrics' && proj && html`<${GenLyricsDialog} meta=${meta} job=${job} models=${st.models} initialTxt=${dlg.txt}
        pickFile=${ctx.pickFile} onClose=${() => setDlg(null)} onCancel=${() => api('POST', '/api/job/cancel')}
        onStart=${(txt, lang, fast) => { setLyricLang(lang); api('POST', '/api/lyrics/generate', { txt, lang, fast }).then(setJob).catch(showErr); }} />`}
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

  let inspector;
  if (sel.kind === 'audio') inspector = html`<div><div class="insp-h"><div class="insp-title"><${Icon} name="music" size=${16} cls="muted" />${t('Audio')}<span class="sub">${t('Project')}</span></div></div><div class="insp-b">
      <${Form} fields=${AUDIO_FIELDS} value=${proj.audio} onChange=${(v) => update((p) => { p.audio = v; })} ctx=${ctx} /></div></div>`;
  else if (sel.kind === 'output') inspector = html`<div><div class="insp-h"><div class="insp-title"><${Icon} name="monitor" size=${16} cls="muted" />${t('Output')}<span class="sub">${t('Project')}</span></div></div><div class="insp-b">
      <${Form} fields=${OUTPUT_FIELDS} value=${proj.output} onChange=${(v) => update((p) => { p.output = v; })} ctx=${ctx} />
      <p class="faint" style="font-size:12px">${t('Current resolution')}: ${meta.width}×${meta.height} · ${meta.fps} fps · ${fmtTime(meta.duration)}</p></div></div>`;
  else if (sel.kind === 'line') inspector = html`<${LineEditor} lyr=${lyr} lines=${lineSel} time=${time} seek=${seek} saveState=${lyrSave}
      onChange=${(fn) => editLyrics(fn)} onResync=${resync} resyncing=${resyncing} lyricLang=${lyricLang} setLyricLang=${setLyricLang} wordEdit=${wordEdit} setWordEdit=${setWordEdit} />`;
  else inspector = html`<${TrackInspector} proj=${proj} track=${sel.track} segIdx=${sel.seg} time=${time} meta=${meta}
      update=${update} setSel=${setSel} ctx=${ctx} onGenLyrics=${() => setDlg({ type: 'genlyrics' })} />`;

  return html`<div class="app" style=${{ '--tl-h': tlH + 'px' }}>
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
          ? `${t(STEP_LABEL[job.gen.steps[job.gen.current].id])} · ${Math.round(job.gen.steps[job.gen.current].progress * 100)}%` : job.message}</span>
      </div>`}
      <button class="btn ghost" onClick=${() => setDlg({ type: 'new' })}><${Icon} name="plus" size=${15} /> ${t('New')}</button>
      <button class="btn ghost" onClick=${() => ctx.pickFile('project', (p) => ctx.openProject(p), meta.path)}><${Icon} name="folder-open" size=${15} /> ${t('Open')}</button>
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
    </div>

    <div class="preview">
      <div class="stage-wrap">
        <${Preview} meta=${meta} timeStore=${timeStore} scale=${scale} rev=${rev} playing=${playing} />
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
      sel=${sel} lineSel=${lineSel} wordEdit=${wordEdit} height=${tlH} setHeight=${setTlH}
      onSeek=${seek} onSelect=${selTrack} onMoveSeg=${onMoveSeg} onAddSeg=${ctx.addSeg} onMarker=${onMarker}
      onSelectLine=${selectLine} onLyrics=${(lines) => editLyrics((all) => { all.splice(0, all.length, ...lines); }, false)}
      onLyricsCommit=${() => lyrSaveRef.current()} />
    ${dialogs}
    ${dragging && html`<div class="dropzone"><div><${Icon} name="upload" size=${28} />
      <div style="margin-top:10px;font-size:15px;font-weight:600">${t('Drop to add to the project')}</div>
      <div class="faint" style="margin-top:4px">${t('Audio, background image or video, lyrics (.lrc/.srt/.txt) or font')}</div></div></div>`}
  </div>`;
}

render(html`<${App} />`, document.getElementById('app'));
