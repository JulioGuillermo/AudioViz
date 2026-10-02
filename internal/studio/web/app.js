import { html, render, useState, useEffect, useRef, useCallback } from './vendor/preact-htm.js';
import { TRACKS, AUDIO_FIELDS, OUTPUT_FIELDS, trackById } from './schema.js';
import { api, clone, fmtTime, parseTime, b64u8, basename, debounce } from './util.js';
import { Form } from './fields.js';
import { Timeline, segSummary } from './timeline.js';
import { FilePicker, NewProject, ExportDialog, GenLyricsDialog, JobView } from './dialogs.js';

// ------------------------------------------------------------------ preview

function Preview({ meta, time, scale, rev, playing }) {
  const canvas = useRef();
  // inflight: peticiones en curso; lat: latencia media (ms); shownF: último cuadro dibujado
  const st = useRef({ inflight: 0, lat: 40, shownKey: null, shownF: -1, shownSeq: 0, seq: 0, lastReq: '' });
  const [info, setInfo] = useState({ ms: 0, fps: 0 });
  const fpsWin = useRef([]);

  const draw = (bmp) => {
    const c = canvas.current;
    if (!c) return;
    if (c.width !== bmp.width || c.height !== bmp.height) { c.width = bmp.width; c.height = bmp.height; }
    c.getContext('2d').drawImage(bmp, 0, 0);
    bmp.close && bmp.close();
  };

  const request = (f, key, live) => {
    const s = st.current;
    const seq = ++s.seq;
    s.inflight++;
    s.lastReq = key;
    const t0 = performance.now();
    fetch(`/api/frame?f=${f}&scale=${scale}`)
      .then((r) => { if (!r.ok) throw new Error(r.statusText); return r.blob(); })
      .then((b) => createImageBitmap(b))
      .then((bmp) => {
        const dt = performance.now() - t0;
        s.lat = s.lat * 0.8 + dt * 0.2;
        // En reproducción solo se dibuja si es más reciente que lo mostrado
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
    if (playing) {
      // Pedir por delante según la latencia, con varias peticiones en vuelo
      const ahead = time + s.lat / 1000;
      const f = Math.min(meta.frames - 1, Math.round(ahead * fps));
      const key = `${f}|${scale}|${rev}`;
      if (s.inflight < 3 && key !== s.lastReq && f > s.shownF) request(f, key, true);
      return;
    }
    const f = Math.min(meta.frames - 1, Math.max(0, Math.round(time * fps)));
    const key = `${f}|${scale}|${rev}`;
    if (key === s.shownKey) return;
    if (s.inflight === 0) { request(f, key, false); return; }
    // Hay una en curso: reintentar cuando termine
    const id = setInterval(() => {
      if (st.current.inflight === 0) { clearInterval(id); if (st.current.shownKey !== key) request(f, key, false); }
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

// ------------------------------------------------------------------ inspector

function SegHeader({ track, segs, i, set, meta }) {
  const s = segs[i];
  if (i === 0) return html`<div class="seghead"><span class="muted">Tramo inicial: se aplica desde el principio hasta el siguiente cambio.</span>
    <div class="fld"><label>Nombre</label><div class="ctl"><input type="text" class="grow" value=${s.label} placeholder="(opcional)"
      onChange=${(e) => set({ ...s, label: e.target.value })} /></div></div></div>`;
  return html`<div class="seghead">
    <div class="fld"><label>Nombre</label><div class="ctl"><input type="text" class="grow" value=${s.label} placeholder="p. ej. Estribillo"
      onChange=${(e) => set({ ...s, label: e.target.value })} /></div></div>
    <div class="fld"><label>Empieza en</label><div class="ctl">
      <input type="text" class="mono" style="width:90px" value=${fmtTime(s.at)} onChange=${(e) => set({ ...s, at: Math.max(0.01, Math.min(meta.duration - 0.01, parseTime(e.target.value))) })} />
      <span class="muted">m:ss.cc</span></div></div>
    <div class="fld"><label>Transición</label><div class="ctl">
      <select value=${s.transition.mode} onChange=${(e) => set({ ...s, transition: { ...s.transition, mode: e.target.value, duration: s.transition.duration || 1 } })}>
        <option value="cut">De golpe</option><option value="fade">Suave (fundido)</option></select>
      ${s.transition.mode === 'fade' && html`<input type="number" step="0.1" min="0.05" value=${s.transition.duration}
        onChange=${(e) => set({ ...s, transition: { ...s.transition, duration: Math.max(0.05, +e.target.value) } })} /><span class="muted">s</span>`}
    </div></div>
  </div>`;
}

function TrackInspector({ proj, track, segIdx, time, meta, update, setSel, ctx, onGenLyrics }) {
  const def = trackById[track];
  const segs = proj.tracks[track];
  const i = Math.min(segIdx, segs.length - 1);
  const seg = segs[i];
  const setSeg = (ns) => update((p) => { p.tracks[track][i] = ns; });
  const addAt = (t) => ctx.addSeg(track, t);
  return html`<div>
    <div class="insp-h">
      <h3><span>${def.icon}</span>${def.label}<span class="spacer"></span><span class="muted" style="font-size:12px">${def.group}</span></h3>
      <div class="segtabs">${segs.map((s, k) => html`<button class=${k === i ? 'on' : ''} title=${s.label || ''}
          onClick=${() => { setSel({ kind: 'track', track, seg: k }); ctx.seek(s.at); }}>${k + 1} · ${fmtTime(s.at, false)}${s.label ? ' ' + s.label : ''}</button>`)}</div>
      <div class="row">
        <button onClick=${() => addAt(time)} title="Nuevo tramo que empieza en el cursor (N)">＋ Tramo en ${fmtTime(time)}</button>
        <button class="ghost danger" disabled=${i === 0} onClick=${() => { update((p) => { p.tracks[track].splice(i, 1); }); setSel({ kind: 'track', track, seg: i - 1 }); }}>Eliminar tramo</button>
      </div>
    </div>
    <div class="insp-b">
      <${SegHeader} track=${track} segs=${segs} i=${i} set=${setSeg} meta=${meta} />
      <${Form} fields=${def.fields} value=${seg.cfg} onChange=${(cfg) => setSeg({ ...seg, cfg })} ctx=${ctx} isFirst=${i === 0} />
      ${track === 'lyrics' && html`<div class="group"><div class="gh">Generar la letra</div><div class="gb">
        <p class="muted">Crea un .lrc con el tiempo de cada palabra a partir de la letra en texto.</p>
        <button onClick=${onGenLyrics}>🎙️ Generar letra sincronizada…</button></div></div>`}
    </div>
  </div>`;
}

function LineEditor({ lyr, idx, time, onChange, onSave, dirty, seek }) {
  const line = lyr.lines[idx];
  if (!line) return html`<div class="insp-b muted">Selecciona una línea en la pista de letra.</div>`;
  const setWord = (w, k, v) => {
    const l = clone(line);
    l.words[w][k] = v;
    if (k === 'start' && w > 0 && l.words[w - 1].end > v) l.words[w - 1].end = v;
    l.start = l.words[0].start;
    l.end = l.words[l.words.length - 1].end;
    onChange(idx, l);
  };
  const shift = (dt) => {
    const l = clone(line);
    l.words.forEach((w) => { w.start += dt; w.end += dt; });
    l.start += dt; l.end += dt;
    onChange(idx, l);
  };
  return html`<div>
    <div class="insp-h"><h3>🎤 Línea ${idx + 1}<span class="spacer"></span>
      <button class=${dirty ? 'primary' : ''} disabled=${!dirty} onClick=${onSave}>💾 Guardar letra</button></h3>
      <div class="row">
        <button onClick=${() => seek(line.start - 0.5)}>▶ Ir</button>
        <button title="Adelantar toda la línea" onClick=${() => shift(-0.05)}>−0.05 s</button>
        <button title="Retrasar toda la línea" onClick=${() => shift(0.05)}>+0.05 s</button>
        <button title="Que la línea empiece en el cursor" onClick=${() => shift(time - line.start)}>⇤ al cursor</button>
      </div>
    </div>
    <div class="insp-b"><div class="words">
      <div class="word muted" style="font-size:11px"><span>Palabra</span><span>Inicio</span><span>Fin</span><span></span></div>
      ${line.words.map((w, k) => html`<div class=${'word' + (time >= w.start && time < w.end ? ' now' : '')}>
        <input type="text" value=${w.text} onChange=${(e) => setWord(k, 'text', e.target.value)} />
        <input type="number" step="0.01" value=${w.start.toFixed(2)} onChange=${(e) => setWord(k, 'start', +e.target.value)} />
        <input type="number" step="0.01" value=${w.end.toFixed(2)} onChange=${(e) => setWord(k, 'end', +e.target.value)} />
        <button class="icon" title="Inicio en el cursor" onClick=${() => setWord(k, 'start', +time.toFixed(2))}>⇤</button>
      </div>`)}
    </div>
    <p class="muted">Consejo: reproduce, pausa justo cuando empieza una palabra y pulsa ⇤.</p></div>
  </div>`;
}

// ------------------------------------------------------------------ app

function Welcome({ st, ctx }) {
  return html`<div class="welcome">
    <h1>Audio<b>Viz</b> Studio</h1>
    <p class="muted">Vídeos musicales reactivos al audio · render por GPU (Vulkan)</p>
    <div class="cards">
      <div class="card" onClick=${() => ctx.dialog({ type: 'new' })}><h3>＋ Nuevo proyecto</h3><p class="muted">Crea una carpeta de proyecto y empieza eligiendo el audio y el fondo.</p></div>
      <div class="card" onClick=${() => ctx.pickFile('project', (p) => ctx.openProject(p))}><h3>📂 Abrir proyecto</h3><p class="muted">Abre un project.yaml existente (también los creados a mano).</p></div>
    </div>
    <p class="muted mono" style="font-size:11px">${(st.gpu || '').trim()}</p>
  </div>`;
}

function App() {
  const [st, setSt] = useState(null);
  const [proj, setProj] = useState(null);
  const [meta, setMeta] = useState(null);
  const [ana, setAna] = useState(null);
  const [lyr, setLyr] = useState({ lines: [], path: '' });
  const [lyrDirty, setLyrDirty] = useState(false);
  const [sel, setSel] = useState({ kind: 'track', track: 'visualizer', seg: 0 });
  const [time, setTime] = useState(0);
  const [playing, setPlaying] = useState(false);
  const [scale, setScale] = useState(2);
  const [rev, setRev] = useState(0);
  const [save, setSave] = useState('');
  const [job, setJob] = useState(null);
  const [dlg, setDlg] = useState(null);
  const [toast, setToast] = useState('');
  const [tlH, setTlH] = useState(Math.round(innerHeight * 0.36));
  const audio = useRef(new Audio());
  const projRef = useRef(null);
  const timeRef = useRef(0);
  timeRef.current = time;
  projRef.current = proj;

  const showErr = (e) => { setToast(String(e.message || e)); setTimeout(() => setToast(''), 6000); };

  const loadAll = async () => {
    const s = await api('GET', '/api/state');
    setSt(s);
    if (s.job) setJob(s.job);
    if (!s.project) { setProj(null); return; }
    const d = await api('GET', '/api/project');
    setProj(d.project);
    setMeta(d.meta);
    loadAudio(d.project, d.meta);
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
    try { setLyr(await api('GET', '/api/lyrics')); setLyrDirty(false); } catch (e) { setLyr({ lines: [], path: '' }); showErr(e); }
  };
  const loadAudio = (p, m) => {
    const a = audio.current;
    const path = p.audio.path;
    const src = path ? '/api/media?path=' + encodeURIComponent(path) : '';
    if (a.dataset.src !== src) { a.src = src; a.dataset.src = src; a.load(); }
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

  // --- guardado (con espera para agrupar cambios rápidos)
  const pushRef = useRef(debounce(async (p, oldAudio, oldLyr) => {
    setSave('Guardando…');
    try {
      const r = await api('PUT', '/api/project', p);
      setMeta(r.meta);
      setSave('Guardado ' + r.saved);
      if (p.audio.path !== oldAudio || r.meta.frames !== (projRef.metaFrames || 0)) { loadAna(); loadAudio(p, r.meta); }
      projRef.metaFrames = r.meta.frames;
      if (p.tracks.lyrics[0].cfg.path !== oldLyr) loadLyrics();
      setRev((x) => x + 1);
    } catch (e) { setSave('Error al guardar'); showErr(e); }
  }, 250));

  const update = (fn) => {
    const old = projRef.current;
    const p = clone(old);
    fn(p);
    setProj(p);
    projRef.current = p;
    pushRef.current(p, old.audio.path, old.tracks.lyrics[0].cfg.path);
  };

  // --- transporte
  const seek = (t) => {
    const d = meta ? meta.duration : 0;
    t = Math.max(0, Math.min(d, t));
    setTime(t);
    if (playing && audio.current.src) audio.current.currentTime = t;
  };
  useEffect(() => {
    if (!playing) return;
    const a = audio.current;
    let raf, base = performance.now(), t0 = timeRef.current;
    if (a.src) { a.currentTime = t0; a.play().catch(showErr); }
    const tick = () => {
      const t = a.src ? a.currentTime : t0 + (performance.now() - base) / 1000;
      if (meta && t >= meta.duration) { setPlaying(false); return; }
      setTime(t);
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => { cancelAnimationFrame(raf); a.pause(); };
  }, [playing]);

  // --- acciones
  const ctx = {
    seek,
    dialog: setDlg,
    pickFile: (kind, cb, start) => setDlg({ type: 'file', kind, cb, start }),
    openProject: async (p) => { try { await api('POST', '/api/project/open', { path: p }); await loadAll(); } catch (e) { showErr(e); } },
    addSeg: (track, t) => {
      const segs = projRef.current.tracks[track];
      if (t <= 0.01) { setSel({ kind: 'track', track, seg: 0 }); return; }
      let i = segs.length - 1;
      while (i > 0 && segs[i].at > t) i--;
      const ns = { at: +t.toFixed(3), label: '', transition: { mode: 'cut', duration: 1 }, cfg: clone(segs[i].cfg) };
      update((p) => { p.tracks[track].splice(i + 1, 0, ns); });
      setSel({ kind: 'track', track, seg: i + 1 });
    },
  };

  const onLyricsGenerated = (out) => {
    const p = projRef.current;
    if (!p || !out) return;
    update((q) => { q.tracks.lyrics.forEach((s) => { s.cfg.path = out; }); q.tracks.lyrics[0].cfg.enabled = true; });
    setTimeout(loadLyrics, 800);
  };

  const onMoveSeg = (track, i, at, commit) => {
    if (commit) {
      // Reordenar por tiempo y guardar
      update((p) => {
        const segs = p.tracks[track];
        const moved = segs[i];
        const rest = segs.slice(1).sort((a, b) => a.at - b.at);
        p.tracks[track] = [segs[0], ...rest];
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
      const label = prompt('Nombre de la marca (p. ej. Estribillo):', 'Marca');
      if (label === null) return;
      update((p) => { p.markers.push({ at: +timeRef.current.toFixed(3), label }); p.markers.sort((a, b) => a.at - b.at); });
    } else if (op === 'rename') {
      const label = prompt('Nuevo nombre:', projRef.current.markers[i].label);
      if (label !== null) update((p) => { p.markers[i].label = label; });
    } else if (op === 'delete') {
      if (confirm('¿Borrar la marca "' + projRef.current.markers[i].label + '"?')) update((p) => { p.markers.splice(i, 1); });
    }
  };

  const saveLyrics = async () => {
    try {
      await api('PUT', '/api/lyrics', { lines: lyr.lines, path: lyr.path });
      setLyrDirty(false);
      // Volver a aplicar el proyecto para que la sesión recargue la letra
      await api('PUT', '/api/project', projRef.current);
      setRev((x) => x + 1);
    } catch (e) { showErr(e); }
  };

  // --- teclado
  useEffect(() => {
    const k = (e) => {
      if (e.target.matches('input, select, textarea')) return;
      if (!projRef.current || !meta) return;
      const fr = 1 / meta.fps;
      if (e.code === 'Space') { e.preventDefault(); setPlaying((p) => !p); }
      else if (e.key === 'ArrowLeft') seek(timeRef.current - (e.shiftKey ? 1 : fr));
      else if (e.key === 'ArrowRight') seek(timeRef.current + (e.shiftKey ? 1 : fr));
      else if (e.key === 'Home') seek(0);
      else if (e.key === 'End') seek(meta.duration);
      else if (e.key === 'm' || e.key === 'M') onMarker('add');
      else if ((e.key === 'n' || e.key === 'N') && sel.kind === 'track') ctx.addSeg(sel.track, timeRef.current);
      else if (e.key === 'Delete' && sel.kind === 'track' && sel.seg > 0) {
        update((p) => { p.tracks[sel.track].splice(sel.seg, 1); });
        setSel({ ...sel, seg: sel.seg - 1 });
      }
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, [meta, sel]);

  if (!st) return html`<div class="welcome"><p class="muted">Cargando…</p></div>`;

  const dialogs = html`
    ${dlg && dlg.type === 'file' && html`<${FilePicker} kind=${dlg.kind} start=${dlg.start} hasProject=${!!proj}
        onPick=${dlg.cb} onClose=${() => setDlg(null)} />`}
    ${dlg && dlg.type === 'new' && html`<${NewProject} start=${st.home || st.startDir} pickFile=${ctx.pickFile} onClose=${() => setDlg(null)}
        onCreate=${async (dir, name) => { await api('POST', '/api/project/new', { dir, name }); await loadAll(); }} />`}
    ${dlg && dlg.type === 'export' && proj && html`<${ExportDialog} proj=${proj} meta=${meta} job=${job} onClose=${() => setDlg(null)}
        onStart=${(o) => api('POST', '/api/export', o).then(setJob).catch(showErr)} onCancel=${() => api('POST', '/api/job/cancel')} />`}
    ${dlg && dlg.type === 'genlyrics' && proj && html`<${GenLyricsDialog} meta=${meta} job=${job} available=${st.lyricsTool}
        pickFile=${ctx.pickFile} onClose=${() => setDlg(null)} onCancel=${() => api('POST', '/api/job/cancel')}
        onStart=${(txt, lang) => api('POST', '/api/lyrics/generate', { txt, lang }).then(setJob).catch(showErr)} />`}
    ${toast && html`<div class="toast" onClick=${() => setToast('')}>⚠ ${toast}</div>`}`;

  if (!proj || !meta) return html`<${Welcome} st=${st} ctx=${ctx} />${dialogs}`;

  const groups = [...new Set(TRACKS.map((t) => t.group))];
  const selTrack = (track, seg) => {
    const segs = proj.tracks[track];
    if (seg === null) { seg = 0; segs.forEach((s, k) => { if (s.at <= timeRef.current) seg = k; }); }
    setSel({ kind: 'track', track, seg });
  };
  const jobRunning = job && job.status === 'running';

  let inspector;
  if (sel.kind === 'audio') inspector = html`<div><div class="insp-h"><h3>🎵 Audio</h3></div><div class="insp-b">
      <${Form} fields=${AUDIO_FIELDS} value=${proj.audio} onChange=${(v) => update((p) => { p.audio = v; })} ctx=${ctx} /></div></div>`;
  else if (sel.kind === 'output') inspector = html`<div><div class="insp-h"><h3>🎬 Salida</h3></div><div class="insp-b">
      <${Form} fields=${OUTPUT_FIELDS} value=${proj.output} onChange=${(v) => update((p) => { p.output = v; })} ctx=${ctx} />
      <p class="muted">Resolución actual: ${meta.width}×${meta.height} · ${meta.fps} fps · ${fmtTime(meta.duration)}</p></div></div>`;
  else if (sel.kind === 'line') inspector = html`<${LineEditor} lyr=${lyr} idx=${sel.line} time=${time} seek=${seek} dirty=${lyrDirty}
      onSave=${saveLyrics} onChange=${(i, l) => { const n = clone(lyr); n.lines[i] = l; setLyr(n); setLyrDirty(true); }} />`;
  else inspector = html`<${TrackInspector} proj=${proj} track=${sel.track} segIdx=${sel.seg} time=${time} meta=${meta}
      update=${update} setSel=${setSel} ctx=${ctx} onGenLyrics=${() => setDlg({ type: 'genlyrics' })} />`;

  return html`<div class="app" style=${{ '--tl-h': tlH + 'px' }}>
    <div class="top">
      <div class="logo">▮▮▮ Audio<b>Viz</b></div>
      <span class="projname" title=${meta.path}>${meta.name}</span>
      <span class=${'save' + (save.startsWith('Error') ? ' err' : '')}>${save}</span>
      <span class="spacer"></span>
      ${job && html`<div class="jobbar" title=${job.message} onClick=${() => setDlg({ type: job.kind === 'lyrics' ? 'genlyrics' : 'export' })}>
        <span>${job.kind === 'export' ? '🎬' : '🎙️'}</span>
        <div class="bar"><i style=${{ width: (job.progress * 100).toFixed(1) + '%' }}></i></div>
        <span class="msg">${job.status === 'running' ? job.message : { done: '✔ ', error: '✖ ', canceled: '■ ' }[job.status] + job.message}</span>
      </div>`}
      <button class="ghost" onClick=${() => setDlg({ type: 'new' })}>Nuevo</button>
      <button class="ghost" onClick=${() => ctx.pickFile('project', (p) => ctx.openProject(p), meta.path)}>Abrir</button>
      <button class="primary" disabled=${jobRunning && job.kind === 'export'} onClick=${() => setDlg({ type: 'export' })}>🎬 Exportar</button>
    </div>

    <div class="tracks">
      <h4>Proyecto</h4>
      <div class=${'trk' + (sel.kind === 'audio' ? ' on' : '')} onClick=${() => setSel({ kind: 'audio' })}>
        <span>🎵</span><span class="lbl">Audio</span><span class="n">${proj.audio.path ? basename(proj.audio.path).slice(0, 14) : '—'}</span></div>
      <div class=${'trk' + (sel.kind === 'output' ? ' on' : '')} onClick=${() => setSel({ kind: 'output' })}>
        <span>🎬</span><span class="lbl">Salida</span><span class="n">${meta.width}×${meta.height}</span></div>
      ${groups.map((g) => html`<h4>${g}</h4>${TRACKS.filter((t) => t.group === g).map((t) => {
        const segs = proj.tracks[t.id];
        let cur = 0;
        segs.forEach((s, k) => { if (s.at <= time) cur = k; });
        const c = segs[cur].cfg;
        const on = t.id === 'background' ? !!c.path : c.enabled !== false;
        return html`<div class=${'trk' + (sel.kind === 'track' && sel.track === t.id ? ' on' : '') + (on ? '' : ' off')}
            title=${segSummary(t.id, c)} onClick=${() => selTrack(t.id, null)}>
          <span class="dot" style=${{ background: t.color }}></span><span>${t.icon}</span><span class="lbl">${t.label}</span>
          <span class="n">${segs.length > 1 ? segs.length + ' tramos' : ''}</span></div>`;
      })}`)}
    </div>

    <div class="preview">
      <${Preview} meta=${meta} time=${time} scale=${scale} rev=${rev} playing=${playing} />
      <div class="transport">
        <button class="icon" title="Inicio (Inicio)" onClick=${() => seek(0)}>⏮</button>
        <button class="icon" title="Cuadro anterior (←)" onClick=${() => seek(time - 1 / meta.fps)}>◀</button>
        <button class="primary icon" style="width:44px;justify-content:center" title="Reproducir / pausa (Espacio)" onClick=${() => setPlaying(!playing)}>${playing ? '❚❚' : '▶'}</button>
        <button class="icon" title="Cuadro siguiente (→)" onClick=${() => seek(time + 1 / meta.fps)}>▶</button>
        <span class="time">${fmtTime(time)} <small>/ ${fmtTime(meta.duration)} · cuadro ${Math.round(time * meta.fps)}</small></span>
        <span class="spacer"></span>
        ${!meta.hasAudio && html`<span style="color:var(--warn)">Sin audio: elige uno en 🎵 Audio</span>`}
        <label class="muted">Calidad preview</label>
        <select value=${scale} onChange=${(e) => setScale(+e.target.value)}>
          <option value="1">Completa</option><option value="2">1/2</option><option value="3">1/3</option><option value="4">1/4</option>
        </select>
      </div>
    </div>

    <div class="inspector">${inspector}</div>

    <${Timeline} proj=${proj} meta=${meta} ana=${ana} lyr=${lyr} time=${time} sel=${sel} height=${tlH} setHeight=${setTlH}
      onSeek=${seek} onSelect=${selTrack} onMoveSeg=${onMoveSeg} onAddSeg=${ctx.addSeg} onMarker=${onMarker}
      onSelectLine=${(i) => { setSel({ kind: 'line', line: i }); seek(lyr.lines[i].start); }} />
    ${dialogs}
  </div>`;
}

render(html`<${App} />`, document.getElementById('app'));
