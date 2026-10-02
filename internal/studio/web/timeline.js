import { html, useEffect, useRef, useState, useMemo } from './vendor/preact-htm.js';
import { TRACKS } from './schema.js';
import { fmtTime, basename } from './util.js';

const H = { ruler: 26, audio: 74, lyrics: 30, track: 28 };

// Resumen corto de un tramo para mostrarlo en el bloque
export function segSummary(track, cfg) {
  if (!cfg) return '';
  switch (track) {
    case 'background': return basename(cfg.path) || 'sin fondo';
    case 'visualizer': return cfg.enabled ? `${cfg.type} · ${cfg.bars}` : 'oculto';
    case 'particles': return cfg.enabled ? (cfg.layers || []).map((l) => l.react_to || 'bass').join(' + ') : 'apagadas';
    case 'lyrics': return cfg.enabled ? `${cfg.position} · ${cfg.mode}` : 'oculta';
    default: return cfg.enabled ? (cfg.react_to ? `↯ ${cfg.react_to}` : 'activo') : 'apagado';
  }
}

function AudioLane({ ana, width, pps, duration }) {
  const ref = useRef();
  useEffect(() => {
    const c = ref.current;
    if (!c || !ana) return;
    const dpr = window.devicePixelRatio || 1;
    c.width = Math.max(1, Math.floor(width * dpr));
    c.height = H.audio * dpr;
    c.style.width = width + 'px';
    c.style.height = H.audio + 'px';
    const g = c.getContext('2d');
    g.scale(dpr, dpr);
    g.clearRect(0, 0, width, H.audio);
    const n = ana.frames;
    const fps = ana.fps;
    const mid = H.audio / 2;
    // Forma de onda (columna por píxel)
    g.fillStyle = '#3a4252';
    for (let x = 0; x < width; x++) {
      const i0 = Math.floor((x / pps) * fps), i1 = Math.max(i0 + 1, Math.floor(((x + 1) / pps) * fps));
      let pk = 0;
      for (let i = i0; i < i1 && i < n; i++) pk = Math.max(pk, ana.wave[i] || 0);
      const h = (pk / 255) * (H.audio - 6) / 2;
      g.fillRect(x, mid - h, 1, h * 2);
    }
    // Curvas de bandas
    const curve = (arr, color) => {
      g.strokeStyle = color;
      g.lineWidth = 1.2;
      g.beginPath();
      for (let x = 0; x < width; x += 1) {
        const i0 = Math.floor((x / pps) * fps), i1 = Math.max(i0 + 1, Math.floor(((x + 1) / pps) * fps));
        let s = 0, k = 0;
        for (let i = i0; i < i1 && i < n; i++) { s += arr[i]; k++; }
        const v = k ? s / k / 255 : 0;
        const y = H.audio - 2 - v * (H.audio - 6);
        x === 0 ? g.moveTo(x, y) : g.lineTo(x, y);
      }
      g.stroke();
    };
    curve(ana.bass, '#ff5d5dcc');
    curve(ana.mid, '#3ddc97b0');
    curve(ana.treble, '#5ab0ffb0');
    // Golpes
    g.fillStyle = '#ffd16655';
    for (const b of ana.beats) g.fillRect((b / fps) * pps, 0, 1, 5);
  }, [ana, width, pps]);
  return html`<canvas ref=${ref}></canvas>`;
}

function Ruler({ width, pps, duration }) {
  const ref = useRef();
  useEffect(() => {
    const c = ref.current;
    if (!c) return;
    const dpr = window.devicePixelRatio || 1;
    c.width = Math.max(1, Math.floor(width * dpr));
    c.height = H.ruler * dpr;
    c.style.width = width + 'px';
    c.style.height = H.ruler + 'px';
    const g = c.getContext('2d');
    g.scale(dpr, dpr);
    g.clearRect(0, 0, width, H.ruler);
    const steps = [0.1, 0.25, 0.5, 1, 2, 5, 10, 15, 30, 60];
    const step = steps.find((s) => s * pps >= 70) || 60;
    g.fillStyle = '#8b93a5';
    g.strokeStyle = '#3a4252';
    g.font = '10px ui-monospace, monospace';
    for (let t = 0; t <= duration; t += step) {
      const x = Math.round(t * pps) + 0.5;
      g.beginPath(); g.moveTo(x, 14); g.lineTo(x, H.ruler); g.stroke();
      g.fillText(fmtTime(t, step < 1), x + 3, 11);
      const sub = step / 4;
      for (let k = 1; k < 4; k++) {
        const xs = Math.round((t + sub * k) * pps) + 0.5;
        g.beginPath(); g.moveTo(xs, 20); g.lineTo(xs, H.ruler); g.stroke();
      }
    }
  }, [width, pps, duration]);
  return html`<canvas ref=${ref}></canvas>`;
}

export function Timeline({ proj, meta, ana, lyr, time, sel, onSeek, onSelect, onMoveSeg, onAddSeg, onSelectLine,
  onMarker, height, setHeight }) {
  const scroller = useRef();
  const namesRef = useRef();
  const [pps, setPps] = useState(null); // píxeles por segundo
  const duration = meta.duration || 30;
  const effPps = pps ?? 12;
  const width = Math.max(200, Math.ceil(duration * effPps) + 40);

  // Ajustar zoom al ancho disponible la primera vez
  useEffect(() => {
    if (pps === null && scroller.current) setPps(Math.max(2, (scroller.current.clientWidth - 40) / duration));
  }, [duration]);

  // Mantener el cursor visible durante la reproducción
  useEffect(() => {
    const sc = scroller.current;
    if (!sc) return;
    const x = time * effPps;
    if (x < sc.scrollLeft || x > sc.scrollLeft + sc.clientWidth - 30) sc.scrollLeft = Math.max(0, x - sc.clientWidth * 0.2);
  }, [Math.floor(time * effPps / 200)]);

  // Puntos de imán: marcas, golpes, cursor e inicios de tramos
  const snaps = useMemo(() => {
    const s = [time, ...proj.markers.map((m) => m.at)];
    for (const id in proj.tracks) proj.tracks[id].forEach((g) => s.push(g.at));
    if (ana) for (const b of ana.beats) s.push(b / ana.fps);
    return s;
  }, [proj, ana, time]);
  const snap = (t, ev) => {
    if (ev && ev.altKey) return t;
    let best = t, bd = 8 / effPps;
    for (const s of snaps) if (Math.abs(s - t) < bd) { bd = Math.abs(s - t); best = s; }
    return best;
  };

  const xToT = (clientX) => {
    const r = scroller.current.getBoundingClientRect();
    return Math.max(0, Math.min(duration, (clientX - r.left + scroller.current.scrollLeft) / effPps));
  };

  const seekDrag = (e) => {
    if (e.button !== 0) return;
    onSeek(xToT(e.clientX));
    const mv = (ev) => onSeek(xToT(ev.clientX));
    const up = () => { removeEventListener('mousemove', mv); removeEventListener('mouseup', up); };
    addEventListener('mousemove', mv);
    addEventListener('mouseup', up);
  };

  const segDrag = (e, track, i) => {
    e.stopPropagation();
    onSelect(track, i);
    if (i === 0 || e.button !== 0) return;
    const startX = e.clientX;
    const at0 = proj.tracks[track][i].at;
    let moved = false;
    const mv = (ev) => {
      const dt = (ev.clientX - startX) / effPps;
      if (!moved && Math.abs(ev.clientX - startX) < 3) return;
      moved = true;
      onMoveSeg(track, i, Math.max(0.01, Math.min(duration - 0.01, snap(at0 + dt, ev))), false);
    };
    const up = () => {
      removeEventListener('mousemove', mv); removeEventListener('mouseup', up);
      if (moved) onMoveSeg(track, i, null, true);
    };
    addEventListener('mousemove', mv);
    addEventListener('mouseup', up);
  };

  const wheel = (e) => {
    if (e.ctrlKey || e.metaKey) {
      e.preventDefault();
      const sc = scroller.current;
      const t = xToT(e.clientX);
      const np = Math.max(1, Math.min(600, effPps * (e.deltaY < 0 ? 1.18 : 1 / 1.18)));
      setPps(np);
      requestAnimationFrame(() => { sc.scrollLeft = t * np - (e.clientX - sc.getBoundingClientRect().left); });
    }
  };

  const onScroll = () => { if (namesRef.current) namesRef.current.scrollTop = scroller.current.scrollTop; };

  const resize = (e) => {
    const y0 = e.clientY, h0 = height;
    const mv = (ev) => setHeight(Math.max(160, Math.min(innerHeight - 220, h0 + (y0 - ev.clientY))));
    const up = () => { removeEventListener('mousemove', mv); removeEventListener('mouseup', up); };
    addEventListener('mousemove', mv); addEventListener('mouseup', up);
  };

  const curLine = lyr.lines.findIndex((l) => time >= l.start && time <= l.end);

  return html`<div class="timeline">
    <div class="tl-resize" onMouseDown=${resize}></div>
    <div class="tl-bar">
      <b>Línea de tiempo</b>
      <span class="muted">Doble clic en una pista = nuevo tramo · arrastra un tramo para moverlo (imán a marcas/golpes, Alt sin imán) · Ctrl+rueda = zoom</span>
      <span class="spacer"></span>
      <span class="row muted" style="gap:10px;font-size:11px">
        <span style="color:#ff5d5d">━ graves</span><span style="color:#3ddc97">━ medios</span><span style="color:#5ab0ff">━ agudos</span>
      </span>
      <button class="icon" title="Marca en el cursor (M)" onClick=${() => onMarker('add')}>⚑ Marca</button>
      <button class="icon" title="Alejar" onClick=${() => setPps(Math.max(1, effPps / 1.5))}>－</button>
      <button class="icon" title="Acercar" onClick=${() => setPps(Math.min(600, effPps * 1.5))}>＋</button>
      <button class="icon" title="Ver todo" onClick=${() => setPps(Math.max(1, (scroller.current.clientWidth - 40) / duration))}>⤢</button>
    </div>
    <div class="tl-body">
      <div class="tl-names" ref=${namesRef}>
        <div class="nm" style=${{ '--h': H.ruler + 'px' }}></div>
        <div class="nm" style=${{ '--h': H.audio + 'px' }}>🎵 Audio</div>
        <div class="nm" style=${{ '--h': H.lyrics + 'px' }} onClick=${() => onSelect('lyrics', 0)}>🎤 Letra (líneas)</div>
        ${TRACKS.map((t) => html`<div class=${'nm' + (sel.track === t.id ? ' on' : '')} style=${{ '--h': H.track + 'px' }}
            onClick=${() => onSelect(t.id, null)}>${t.icon} ${t.label}</div>`)}
      </div>
      <div class="tl-scroll" ref=${scroller} onWheel=${wheel} onScroll=${onScroll}>
        <div class="tl-inner" style=${{ width: width + 'px' }}>
          <div class="lane ruler" style=${{ '--h': H.ruler + 'px' }} onMouseDown=${seekDrag}>
            <${Ruler} width=${width} pps=${effPps} duration=${duration} />
          </div>
          <div class="lane" style=${{ '--h': H.audio + 'px' }} onMouseDown=${seekDrag}>
            <${AudioLane} ana=${ana} width=${width} pps=${effPps} duration=${duration} />
          </div>
          <div class="lane" style=${{ '--h': H.lyrics + 'px' }} onMouseDown=${seekDrag}>
            ${lyr.lines.map((l, i) => html`<div class=${'lyrline' + (sel.kind === 'line' && sel.line === i ? ' on' : '')}
                style=${{ left: l.start * effPps + 'px', width: Math.max(4, (l.end - l.start) * effPps) + 'px', opacity: i === curLine ? 1 : 0.8 }}
                title=${l.words.map((w) => w.text).join(' ')}
                onMouseDown=${(e) => { e.stopPropagation(); onSelectLine(i); }}>${l.words.map((w) => w.text).join(' ')}</div>`)}
          </div>
          ${TRACKS.map((t) => {
            const segs = proj.tracks[t.id] || [];
            return html`<div class=${'lane' + (sel.track === t.id ? ' sel' : '')} style=${{ '--h': H.track + 'px' }}
                onMouseDown=${seekDrag} onDblClick=${(e) => onAddSeg(t.id, snap(xToT(e.clientX), e))}>
              ${segs.map((s, i) => {
                const end = i + 1 < segs.length ? segs[i + 1].at : duration;
                const on = sel.kind === 'track' && sel.track === t.id && sel.seg === i;
                const fade = s.transition && s.transition.mode === 'fade' && i > 0 ? s.transition.duration * effPps : 0;
                const enabled = t.id === 'background' ? !!s.cfg.path : s.cfg.enabled !== false;
                return html`<div class=${'seg' + (on ? ' on' : '') + (enabled ? '' : ' off')}
                    style=${{ left: s.at * effPps + 'px', width: Math.max(3, (end - s.at) * effPps - 1) + 'px', background: t.color + (on ? 'ee' : '99') }}
                    title=${(s.label ? s.label + ' · ' : '') + fmtTime(s.at) + ' → ' + fmtTime(end)}
                    onMouseDown=${(e) => segDrag(e, t.id, i)} onDblClick=${(e) => e.stopPropagation()}>
                  ${fade > 0 && html`<div class="fade" style=${{ width: fade + 'px' }}></div>`}
                  ${i > 0 && html`<div class="grip"></div>`}
                  <span>${s.label ? html`<b>${s.label}</b> · ` : ''}${segSummary(t.id, s.cfg)}</span>
                </div>`;
              })}
            </div>`;
          })}
          ${proj.markers.map((m, i) => html`<div class="marker" style=${{ left: m.at * effPps + 'px' }}>
            <b title="Clic: ir · doble clic: renombrar · clic derecho: borrar"
              onMouseDown=${(e) => { e.stopPropagation(); if (e.button === 0) onSeek(m.at); }}
              onDblClick=${() => onMarker('rename', i)}
              onContextMenu=${(e) => { e.preventDefault(); onMarker('delete', i); }}>⚑ ${m.label}</b>
          </div>`)}
          <div class="playhead" style=${{ left: time * effPps + 'px' }}></div>
        </div>
      </div>
    </div>
  </div>`;
}
