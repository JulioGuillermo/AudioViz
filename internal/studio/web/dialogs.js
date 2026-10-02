import { html, useEffect, useState, useRef } from './vendor/preact-htm.js';
import { api, fmtTime, basename } from './util.js';
import { Icon } from './icons.js';
import { FILE_ICON } from './fields.js';

const KIND_LABEL = {
  audio: 'audio', bg: 'fondo', lyrics: 'letra', txt: 'letra en texto', font: 'fuente', project: 'proyecto', dir: 'carpeta',
};
const KIND_HINT = {
  audio: 'FLAC, WAV, MP3 u OGG', bg: 'Imagen, GIF o vídeo (MP4, MOV, MKV, WebM, AVI)', lyrics: '.lrc o .srt',
  txt: '.txt (vale el formato de Suno)', font: '.ttf u .otf', project: 'project.yaml', dir: 'Navega hasta la carpeta y pulsa «Usar esta carpeta»',
};

export function Modal({ title, icon, onClose, children, footer, wide }) {
  const ref = useRef();
  useEffect(() => {
    const k = (e) => {
      if (e.key !== 'Escape') return;
      const all = document.querySelectorAll('.modal-bg');
      if (all[all.length - 1] === ref.current) onClose(); // solo el diálogo de arriba
    };
    addEventListener('keydown', k);
    return () => removeEventListener('keydown', k);
  }, []);
  return html`<div class="modal-bg" ref=${ref} onMouseDown=${(e) => e.target === e.currentTarget && onClose()}>
    <div class="modal" style=${wide ? { width: 'min(820px,96vw)' } : null}>
      <div class="modal-h">${icon && html`<${Icon} name=${icon} size=${16} cls="muted" />`}${title}<span class="spacer"></span>
        <button class="btn icon ghost" title="Cerrar (Esc)" onClick=${onClose}><${Icon} name="x" size=${16} /></button></div>
      <div class="mb">${children}</div>
      ${footer && html`<div class="mf">${footer}</div>`}
    </div>
  </div>`;
}

// Explorador de archivos del equipo donde corre el estudio (+ subir archivo al proyecto)
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
  return html`<${Modal} title=${'Elegir ' + (KIND_LABEL[kind] || 'archivo')} icon="folder-open" onClose=${onClose} wide=${true}
    footer=${html`
      ${hasProject && kind !== 'dir' && kind !== 'project' && html`
        <input type="file" ref=${fileRef} style="display:none" onChange=${(e) => e.target.files[0] && upload(e.target.files[0])} />
        <button class="btn" disabled=${busy} onClick=${() => fileRef.current.click()}><${Icon} name="upload" size=${15} /> Subir desde este equipo…</button>`}
      <span class="spacer"></span>
      <button class="btn" onClick=${onClose}>Cancelar</button>
      ${kind === 'dir' && dir && html`<button class="btn primary" onClick=${() => { onPick(dir.path); onClose(); }}>Usar esta carpeta</button>`}`}>
    <p class="faint" style="margin-top:0">${KIND_HINT[kind] || ''}</p>
    ${err && html`<div class="alert err" style="margin-bottom:10px"><${Icon} name="triangle-alert" size=${15} />${err}</div>`}
    ${dir && html`<div class="row" style="margin-bottom:10px">
      <div class="btngroup">
        <button class="btn icon" title="Carpeta superior" onClick=${() => load(dir.parent)}><${Icon} name="arrow-up" size=${15} /></button>
        <button class="btn icon" title="Carpeta personal" onClick=${() => load('~')}><${Icon} name="house" size=${15} /></button>
      </div>
      <div class="crumb grow">${dir.path}</div>
    </div>
    <div class="files">
      ${entries.length === 0 && html`<div class="f faint">No hay ${kind === 'dir' ? 'carpetas' : 'archivos de este tipo'} aquí</div>`}
      ${entries.map((e) => html`<div class=${'f' + (e.dir ? ' dir' : '')} onClick=${() => (e.dir ? load(e.path) : (onPick(e.path), onClose()))}>
        <${Icon} name=${e.dir ? 'folder' : FILE_ICON[kind] || 'file'} size=${15} cls=${e.dir ? 'muted' : 'faint'} /><span>${e.name}</span>
        ${!e.dir && html`<span class="sz">${(e.size / 1048576).toFixed(1)} MB</span>`}
      </div>`)}
    </div>`}
  </${Modal}>`;
}

export function NewProject({ start, onCreate, onClose, pickFile }) {
  const [dir, setDir] = useState(start || '');
  const [name, setName] = useState('mi-video');
  const [err, setErr] = useState('');
  return html`<${Modal} title="Nuevo proyecto" icon="plus" onClose=${onClose}
    footer=${html`<button class="btn" onClick=${onClose}>Cancelar</button>
      <button class="btn primary" onClick=${async () => { try { await onCreate(dir, name); onClose(); } catch (e) { setErr(e.message); } }}>Crear proyecto</button>`}>
    <div class="fld"><label>Ubicación</label><div class="ctl filectl">
      <div class="fn"><${Icon} name="folder" size=${14} cls="faint" /><span>${dir}</span></div>
      <button class="btn icon" title="Elegir carpeta" onClick=${() => pickFile('dir', setDir, dir)}><${Icon} name="folder-open" size=${15} /></button></div></div>
    <div class="fld"><label>Nombre</label><div class="ctl"><input type="text" class="grow" value=${name} onInput=${(e) => setName(e.target.value)} /></div></div>
    <p class="faint">Se creará <span class="mono">${dir}/${name}/project.yaml</span> y una carpeta <span class="mono">assets/</span> para los archivos que subas.</p>
    ${err && html`<div class="alert err"><${Icon} name="triangle-alert" size=${15} />${err}</div>`}
  </${Modal}>`;
}

export function ExportDialog({ proj, meta, job, onStart, onCancel, onClose }) {
  const [path, setPath] = useState(proj.output.path || 'output.mp4');
  const [range, setRange] = useState('all');
  const [from, setFrom] = useState(0);
  const [to, setTo] = useState(meta.duration);
  const running = job && job.kind === 'export' && job.status === 'running';
  return html`<${Modal} title="Exportar vídeo" icon="clapperboard" onClose=${onClose}
    footer=${html`
      <button class="btn" onClick=${onClose}>Cerrar</button>
      ${running ? html`<button class="btn danger" onClick=${onCancel}><${Icon} name="x" size=${15} /> Cancelar exportación</button>`
        : html`<button class="btn primary" disabled=${!meta.hasAudio} onClick=${() => onStart({ path, start: range === 'all' ? 0 : from, end: range === 'all' ? 0 : to })}>
          <${Icon} name="film" size=${15} /> Exportar</button>`}`}>
    <div class="fld"><label>Archivo de salida</label><div class="ctl"><input type="text" class="grow mono" value=${path} onInput=${(e) => setPath(e.target.value)} /></div></div>
    <div class="fld"><label>Duración</label><div class="ctl">
      <select class="grow" value=${range} onChange=${(e) => setRange(e.target.value)}><option value="all">Canción completa (${fmtTime(meta.duration, false)})</option><option value="part">Solo una parte</option></select>
    </div></div>
    ${range === 'part' && html`<div class="fld"><label>Desde / hasta (s)</label><div class="ctl">
      <input type="number" step="0.1" value=${from} onInput=${(e) => setFrom(+e.target.value)} />
      <input type="number" step="0.1" value=${to} onInput=${(e) => setTo(+e.target.value)} /></div></div>`}
    <p class="faint">${meta.width}×${meta.height} · ${meta.fps} fps · H.264 (${proj.output.preset}, CRF ${proj.output.crf}) · audio FLAC sin pérdidas.
      Se exporta una copia del proyecto tal como está ahora, así que puedes seguir editando.</p>
    ${!meta.hasAudio && html`<div class="alert"><${Icon} name="triangle-alert" size=${15} />El proyecto todavía no tiene audio.</div>`}
    ${job && job.kind === 'export' && html`<${JobView} job=${job} />`}
  </${Modal}>`;
}

export function JobView({ job }) {
  const cls = { done: 'st-ok', error: 'st-err', canceled: 'st-warn' }[job.status] || '';
  const icon = { done: 'check', error: 'triangle-alert', canceled: 'x' }[job.status] || 'loader-circle';
  return html`<div style="margin-top:12px">
    <div class="row"><div class="progress"><i style=${{ width: (job.progress * 100).toFixed(1) + '%' }}></i></div>
      <span class="mono faint" style="font-size:11px;width:36px;text-align:right">${(job.progress * 100).toFixed(0)}%</span></div>
    <p class=${'row ' + cls} style="font-size:12.5px"><${Icon} name=${icon} size=${15} />${job.message}</p>
    ${job.log && job.log.length > 0 && html`<div class="logbox">${job.log.join('\n')}</div>`}
  </div>`;
}

const fmtDur = (s) => {
  if (!(s >= 0)) return '–';
  s = Math.round(s);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
};

// Pasos de la generación de letra: barra global + barra del paso actual + lista de pasos
export function GenSteps({ gen, status }) {
  if (!gen) return null;
  const cur = gen.steps[gen.current] || gen.steps[0];
  const icon = { done: 'check', skipped: 'minus', error: 'triangle-alert', running: 'loader-circle', pending: 'circle' };
  return html`<div class="gen">
    <div class="gen-row">
      <span class="gen-l">Total</span>
      <div class="progress"><i style=${{ width: (gen.progress * 100).toFixed(1) + '%' }}></i></div>
      <span class="gen-v mono">${Math.round(gen.progress * 100)}%</span>
      <span class="gen-t mono">${fmtDur(gen.elapsed)}</span>
    </div>
    ${status === 'running' && cur && html`<div class="gen-row">
      <span class="gen-l">${cur.label}</span>
      <div class="progress sub"><i style=${{ width: (cur.progress * 100).toFixed(1) + '%' }}></i></div>
      <span class="gen-v mono">${Math.round(cur.progress * 100)}%</span>
      <span class="gen-t mono" title="Transcurrido / restante">${fmtDur(cur.elapsed)}${cur.eta > 0 ? ' · −' + fmtDur(cur.eta) : ''}</span>
    </div>`}
    <div class="gen-steps">
      ${gen.steps.map((st, i) => html`<div class=${'gen-step ' + st.state}>
        <${Icon} name=${icon[st.state] || 'circle-help'} size=${14} cls=${st.state === 'running' ? 'spin' : ''} />
        <span class="n">${i + 1}. ${st.label}</span>
        <span class="d">${st.detail}</span>
        <span class="t mono">${st.state === 'done' || st.state === 'running' ? fmtDur(st.elapsed) : ''}</span>
      </div>`)}
    </div>
    ${gen.device && html`<div class="faint" style="font-size:11.5px;margin-top:6px">Reconocimiento en: ${gen.device}</div>`}
  </div>`;
}

export function GenLyricsDialog({ meta, job, models, pickFile, onStart, onCancel, onClose, initialTxt }) {
  const [txt, setTxt] = useState(initialTxt || '');
  const [lang, setLang] = useState('en');
  const [mode, setMode] = useState('precise');
  const mine = job && job.kind === 'lyrics' ? job : null;
  const running = mine && mine.status === 'running';
  const toDownload = (models && !models.whisper ? 574 : 0) + (models && !models.demucs && mode === 'precise' ? 84 : 0);
  return html`<${Modal} title="Generar letra sincronizada" icon="wand-sparkles" onClose=${onClose}
    footer=${html`
      <button class="btn" onClick=${onClose}>${running ? 'Seguir en segundo plano' : 'Cerrar'}</button>
      ${running ? html`<button class="btn danger" onClick=${onCancel}><${Icon} name="x" size=${15} /> Cancelar</button>`
        : html`<button class="btn primary" disabled=${!txt || !meta.hasAudio} onClick=${() => onStart(txt, lang, mode === 'fast')}>
          <${Icon} name="wand-sparkles" size=${15} /> ${mine && mine.status === 'done' ? 'Volver a generar' : 'Generar'}</button>`}`}>
    ${!running && html`
      <p class="faint" style="margin-top:0">A partir de la letra en texto (vale el formato de Suno: las etiquetas [ ] se ignoran) se calcula
        cuándo se canta cada palabra. Todo se hace dentro de AudioViz; el reconocimiento usa la GPU si hay.</p>
      <div class="fld"><label>Letra en texto</label><div class="ctl filectl">
        <div class="fn"><${Icon} name="file-text" size=${14} cls="faint" /><span>${txt ? basename(txt) : html`<span class="faint">Ninguno</span>`}</span></div>
        <button class="btn icon" title="Elegir archivo" onClick=${() => pickFile('txt', setTxt, txt)}><${Icon} name="folder-open" size=${15} /></button></div></div>
      <div class="fld"><label>Idioma</label><div class="ctl"><select class="grow" value=${lang} onChange=${(e) => setLang(e.target.value)}>
        ${[['en', 'Inglés'], ['es', 'Español'], ['fr', 'Francés'], ['de', 'Alemán'], ['it', 'Italiano'], ['pt', 'Portugués'], ['ja', 'Japonés']].map(([v, l]) => html`<option value=${v}>${l}</option>`)}
      </select></div></div>
      <div class="fld"><label>Modo</label><div class="ctl"><select class="grow" value=${mode} onChange=${(e) => setMode(e.target.value)}>
        <option value="precise">Preciso: separa la voz primero (≈ la duración de la canción)</option>
        <option value="fast">Rápido: sobre la mezcla completa (segundos, menos fiable)</option>
      </select></div></div>
      ${toDownload > 0 && html`<div class="alert" style="margin-top:8px"><${Icon} name="info" size=${15} />
        La primera vez se descargan los modelos (≈ ${toDownload} MB) a la caché; después funciona sin conexión.</div>`}
      ${!meta.hasAudio && html`<div class="alert" style="margin-top:8px"><${Icon} name="triangle-alert" size=${15} />El proyecto todavía no tiene audio.</div>`}`}
    ${mine && mine.gen && html`<${GenSteps} gen=${mine.gen} status=${mine.status} />`}
    ${mine && mine.status !== 'running' && html`<p class=${'row ' + ({ done: 'st-ok', error: 'st-err', canceled: 'st-warn' }[mine.status] || '')} style="font-size:12.5px">
      <${Icon} name=${{ done: 'check', error: 'triangle-alert', canceled: 'x' }[mine.status] || 'info'} size=${15} />${mine.message}</p>`}
  </${Modal}>`;
}
