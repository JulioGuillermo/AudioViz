import { html, useEffect, useState, useRef } from './vendor/preact-htm.js';
import { api, fmtTime, basename } from './util.js';

const KIND_LABEL = {
  audio: 'audio (FLAC, WAV, MP3, OGG)', bg: 'imagen, GIF o vídeo', lyrics: 'letra (.lrc, .srt)',
  txt: 'letra en texto (.txt)', font: 'fuente (.ttf, .otf)', project: 'proyecto (project.yaml)', dir: 'carpeta',
};

function Modal({ title, onClose, children, footer, wide }) {
  useEffect(() => {
    const k = (e) => e.key === 'Escape' && onClose();
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, []);
  return html`<div class="modal-bg" onMouseDown=${(e) => e.target === e.currentTarget && onClose()}>
    <div class="modal" style=${wide ? { width: 'min(900px,96vw)' } : null}>
      <h3>${title}<span class="spacer"></span><button class="icon ghost" onClick=${onClose}>✕</button></h3>
      <div class="mb">${children}</div>
      ${footer && html`<div class="mf">${footer}</div>`}
    </div>
  </div>`;
}

// Explorador de archivos del servidor (+ subir archivo al proyecto)
export function FilePicker({ kind, start, onPick, onClose, hasProject }) {
  const [dir, setDir] = useState(null);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const fileRef = useRef();
  const load = async (path) => {
    try {
      setErr('');
      const q = new URLSearchParams({ kind: kind === 'dir' ? 'none-' : kind });
      if (path) q.set('path', path);
      setDir(await api('GET', '/api/fs?' + q));
    } catch (e) { setErr(e.message); }
  };
  useEffect(() => { load(start ? (kind === 'dir' ? start : start.replace(/\/[^/]*$/, '')) : ''); }, []);
  const upload = async (file) => {
    setBusy(true);
    try {
      const fd = new FormData();
      fd.append('file', file);
      const r = await fetch('/api/upload', { method: 'POST', body: fd });
      const d = await r.json();
      if (!r.ok) throw new Error(d.error);
      onPick(d.path);
      onClose();
    } catch (e) { setErr(e.message); }
    setBusy(false);
  };
  const entries = dir ? dir.entries.filter((e) => kind !== 'dir' || e.dir) : [];
  return html`<${Modal} title=${'Elegir ' + (KIND_LABEL[kind] || 'archivo')} onClose=${onClose} wide=${true}
    footer=${html`
      ${hasProject && kind !== 'dir' && kind !== 'project' && html`
        <input type="file" ref=${fileRef} style="display:none" onChange=${(e) => e.target.files[0] && upload(e.target.files[0])} />
        <button disabled=${busy} onClick=${() => fileRef.current.click()}>⬆ Subir desde este equipo…</button>`}
      <span class="spacer"></span>
      ${kind === 'dir' && dir && html`<button class="primary" onClick=${() => { onPick(dir.path); onClose(); }}>Usar esta carpeta</button>`}
      <button onClick=${onClose}>Cancelar</button>`}>
    ${err && html`<div class="toast" style="position:static;transform:none;margin-bottom:8px">${err}</div>`}
    ${dir && html`<div class="row" style="margin-bottom:8px">
      <button class="icon" title="Subir un nivel" onClick=${() => load(dir.parent)}>⬆</button>
      <button class="icon" title="Inicio" onClick=${() => load('~')}>⌂</button>
      <div class="crumb grow">${dir.path}</div>
    </div>
    <div class="files">
      ${entries.length === 0 && html`<div class="f muted">Carpeta vacía (o sin archivos de este tipo)</div>`}
      ${entries.map((e) => html`<div class="f" onClick=${() => (e.dir ? load(e.path) : (onPick(e.path), onClose()))}>
        <span>${e.dir ? '📁' : '📄'}</span><span>${e.name}</span>
        ${!e.dir && html`<span class="sz">${(e.size / 1048576).toFixed(1)} MB</span>`}
      </div>`)}
    </div>`}
  </${Modal}>`;
}

export function NewProject({ start, onCreate, onClose, pickFile }) {
  const [dir, setDir] = useState(start || '');
  const [name, setName] = useState('mi-video');
  const [err, setErr] = useState('');
  return html`<${Modal} title="Nuevo proyecto" onClose=${onClose}
    footer=${html`<button onClick=${onClose}>Cancelar</button>
      <button class="primary" onClick=${async () => { try { await onCreate(dir, name); onClose(); } catch (e) { setErr(e.message); } }}>Crear</button>`}>
    <div class="fld"><label>Carpeta donde crearlo</label><div class="ctl filectl">
      <div class="fn">${dir}</div><button onClick=${() => pickFile('dir', setDir, dir)}>Elegir…</button></div></div>
    <div class="fld"><label>Nombre</label><div class="ctl"><input type="text" class="grow" value=${name} onInput=${(e) => setName(e.target.value)} /></div></div>
    <p class="muted">Se crea <span class="mono">${dir}/${name}/project.yaml</span> y una carpeta <span class="mono">assets/</span> para los archivos que subas.</p>
    ${err && html`<p style="color:var(--err)">${err}</p>`}
  </${Modal}>`;
}

export function ExportDialog({ proj, meta, job, onStart, onCancel, onClose }) {
  const [path, setPath] = useState(proj.output.path || 'output.mp4');
  const [range, setRange] = useState('all');
  const [from, setFrom] = useState(0);
  const [to, setTo] = useState(meta.duration);
  const running = job && job.kind === 'export' && job.status === 'running';
  return html`<${Modal} title="Exportar vídeo" onClose=${onClose}
    footer=${html`
      ${running ? html`<button class="danger" onClick=${onCancel}>Cancelar exportación</button>`
        : html`<button class="primary" disabled=${!meta.hasAudio} onClick=${() => onStart({ path, start: range === 'all' ? 0 : from, end: range === 'all' ? 0 : to })}>🎬 Exportar</button>`}
      <button onClick=${onClose}>Cerrar</button>`}>
    <div class="fld"><label>Archivo</label><div class="ctl"><input type="text" class="grow mono" value=${path} onInput=${(e) => setPath(e.target.value)} /></div></div>
    <div class="fld"><label>Tramo</label><div class="ctl">
      <select value=${range} onChange=${(e) => setRange(e.target.value)}><option value="all">Canción completa (${fmtTime(meta.duration, false)})</option><option value="part">Solo una parte</option></select>
    </div></div>
    ${range === 'part' && html`<div class="fld"><label>Desde / hasta (s)</label><div class="ctl">
      <input type="number" step="0.1" value=${from} onInput=${(e) => setFrom(+e.target.value)} />
      <input type="number" step="0.1" value=${to} onInput=${(e) => setTo(+e.target.value)} /></div></div>`}
    <p class="muted">${meta.width}×${meta.height} · ${meta.fps} fps · H.264 (${proj.output.preset}, CRF ${proj.output.crf}) + audio FLAC sin pérdidas.
      Puedes seguir editando mientras exporta: se usa una copia del proyecto tal como está ahora.</p>
    ${!meta.hasAudio && html`<p style="color:var(--warn)">El proyecto no tiene audio todavía.</p>`}
    ${job && job.kind === 'export' && html`<${JobView} job=${job} />`}
  </${Modal}>`;
}

export function JobView({ job }) {
  const color = { done: 'var(--ok)', error: 'var(--err)', canceled: 'var(--warn)' }[job.status] || 'var(--text)';
  return html`<div style="margin-top:10px">
    <div class="jobbar" style="max-width:none"><div class="bar"><i style=${{ width: (job.progress * 100).toFixed(1) + '%' }}></i></div>
      <span class="mono">${(job.progress * 100).toFixed(0)}%</span></div>
    <p style=${{ color }}>${job.message}</p>
    ${job.log && job.log.length > 0 && html`<pre class="mono" style="max-height:180px;overflow:auto;background:#0d1015;padding:8px;border-radius:6px;font-size:11px">${job.log.join('\n')}</pre>`}
  </div>`;
}

export function GenLyricsDialog({ meta, job, available, pickFile, onStart, onCancel, onClose }) {
  const [txt, setTxt] = useState('');
  const [lang, setLang] = useState('en');
  const running = job && job.kind === 'lyrics' && job.status === 'running';
  return html`<${Modal} title="Generar letra sincronizada" onClose=${onClose}
    footer=${html`
      ${running ? html`<button class="danger" onClick=${onCancel}>Cancelar</button>`
        : html`<button class="primary" disabled=${!txt || !available || !meta.hasAudio} onClick=${() => onStart(txt, lang)}>🎙️ Generar</button>`}
      <button onClick=${onClose}>Cerrar</button>`}>
    ${!available && html`<p style="color:var(--warn)">No se encontró <span class="mono">tools/lyrics</span> junto al binario. Mira docs/lyrics/generar.md.</p>`}
    <p class="muted">Separa la voz (Demucs) y alinea tu letra con ella (Whisper) para obtener el tiempo de cada palabra.
      Acepta el formato de Suno ([Verse], [Chorus]... se ignoran). Tarda unos minutos; la primera vez descarga los modelos (~3 GB).</p>
    <div class="fld"><label>Letra en texto</label><div class="ctl filectl">
      <div class="fn">${txt ? basename(txt) : html`<span class="muted">ninguno</span>`}</div><button onClick=${() => pickFile('txt', setTxt, txt)}>Elegir…</button></div></div>
    <div class="fld"><label>Idioma</label><div class="ctl"><select value=${lang} onChange=${(e) => setLang(e.target.value)}>
      ${[['en', 'Inglés'], ['es', 'Español'], ['fr', 'Francés'], ['de', 'Alemán'], ['it', 'Italiano'], ['pt', 'Portugués'], ['ja', 'Japonés']].map(([v, l]) => html`<option value=${v}>${l}</option>`)}
    </select></div></div>
    ${job && job.kind === 'lyrics' && html`<${JobView} job=${job} />`}
  </${Modal}>`;
}
