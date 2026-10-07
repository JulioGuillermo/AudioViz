import { html, useEffect, useLayoutEffect, useRef, useState, useMemo } from './vendor/preact-htm.js';
import { TRACKS, ZONES, zoneOf } from './schema.js';
import { fmtTime, basename, clone } from './util.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { useStore } from './store.js';
import { EASES, EaseIcon, keyTimes, editKeysAt } from './keys.js';
import { openMenu } from './menu.js';

const H = { ruler: 26, audio: 74, lyrics: 30, track: 28, textRow: 24, zone: 22 };
const cssVar = (n) => getComputedStyle(document.documentElement).getPropertyValue(n).trim();
const BAND = { bass: 'bass', mid: 'mids', treble: 'treble', overall: 'volume', transient: 'transients', beat: 'beats' };

const MOTION_LABEL = Object.fromEntries(TRACKS.find((tr) => tr.id === 'background').fields
  .find((f) => f.key === 'motion').fields.find((f) => f.key === 'type').options);

// Short summary of a segment, shown inside its block
export function segSummary(track, cfg) {
  if (!cfg) return '';
  switch (track) {
    case 'background': {
      const name = basename(cfg.path) || t('no background');
      const m = cfg.motion && cfg.motion.type;
      return m && m !== 'none' ? `${name} · ${t(MOTION_LABEL[m] || m)}` : name;
    }
    case 'visualizer': return cfg.enabled ? `${t(cfg.type)} · ${cfg.bars}` : t('hidden');
    case 'particles': {
      const n = (cfg.layers || []).length;
      return cfg.enabled ? t(n === 1 ? '{n} layer' : '{n} layers', { n }) : t('off');
    }
    case 'lyrics': return cfg.enabled ? `${t(cfg.position)} · ${t(cfg.mode)}` : t('hidden');
    case 'glitch': return cfg.enabled ? t(cfg.react_to ? BAND[cfg.react_to] : 'beats') : t('off');
    default: return cfg.enabled ? t(BAND[cfg.react_to || 'bass'] || 'on') : t('off');
  }
}

// Canvas that only covers the visible part of the timeline (the whole song at high zoom
// would be wider than the browser allows for a canvas). It follows the scroll and repaints;
// draw(g, x0, x1) paints in timeline coordinates the range [x0, x1).
function ViewCanvas({ scroller, height, draw, deps }) {
  const ref = useRef();
  const drawRef = useRef(draw);
  drawRef.current = draw;
  useEffect(() => {
    const sc = scroller.current, c = ref.current;
    if (!sc || !c) return;
    let raf = 0;
    const paint = () => {
      raf = 0;
      const dpr = window.devicePixelRatio || 1;
      const x0 = sc.scrollLeft, w = Math.max(1, sc.clientWidth);
      const cw = Math.floor(w * dpr), ch = Math.floor(height * dpr);
      if (c.width !== cw || c.height !== ch) {
        c.width = cw; c.height = ch;
        c.style.width = w + 'px'; c.style.height = height + 'px';
      }
      c.style.transform = `translateX(${x0}px)`;
      const g = c.getContext('2d');
      g.setTransform(dpr, 0, 0, dpr, -x0 * dpr, 0);
      g.clearRect(x0, 0, w, height);
      drawRef.current(g, x0, x0 + w);
    };
    const req = () => { if (!raf) raf = requestAnimationFrame(paint); };
    paint();
    sc.addEventListener('scroll', req, { passive: true });
    const ro = new ResizeObserver(req);
    ro.observe(sc);
    return () => { sc.removeEventListener('scroll', req); ro.disconnect(); cancelAnimationFrame(raf); };
  }, deps);
  return html`<canvas ref=${ref}></canvas>`;
}

function AudioLane({ ana, pps, scroller }) {
  const draw = (g, xa, xb) => {
    if (!ana) return;
    const n = ana.frames, fps = ana.fps, mid = H.audio / 2;
    const x0 = Math.floor(xa), x1 = Math.ceil(xb);
    const range = (x) => {
      const i0 = Math.floor((x / pps) * fps);
      return [i0, Math.max(i0 + 1, Math.floor(((x + 1) / pps) * fps))];
    };
    g.fillStyle = '#2b2f36';
    for (let x = x0; x < x1; x++) {
      const [i0, i1] = range(x);
      let pk = 0;
      for (let i = i0; i < i1 && i < n; i++) pk = Math.max(pk, ana.wave[i] || 0);
      const h = (pk / 255) * (H.audio - 6) / 2;
      g.fillRect(x, mid - h, 1, h * 2);
    }
    const curve = (arr, color) => {
      g.strokeStyle = color;
      g.lineWidth = 1.2;
      g.beginPath();
      for (let x = x0; x < x1; x++) {
        const [i0, i1] = range(x);
        let sum = 0, k = 0;
        for (let i = i0; i < i1 && i < n; i++) { sum += arr[i]; k++; }
        const y = H.audio - 2 - (k ? sum / k / 255 : 0) * (H.audio - 6);
        x === x0 ? g.moveTo(x, y) : g.lineTo(x, y);
      }
      g.stroke();
    };
    curve(ana.bass, cssVar('--band-bass'));
    curve(ana.mid, cssVar('--band-mid'));
    curve(ana.treble, cssVar('--band-treble'));
    g.fillStyle = 'rgba(255,255,255,0.22)';
    for (const b of ana.beats || []) {
      const x = (b / fps) * pps;
      if (x >= x0 - 1 && x <= x1) g.fillRect(x, 0, 1, 5);
    }
  };
  return html`<${ViewCanvas} scroller=${scroller} height=${H.audio} draw=${draw} deps=${[ana, pps]} />`;
}

function Ruler({ pps, duration, scroller }) {
  const draw = (g, xa, xb) => {
    const steps = [0.1, 0.25, 0.5, 1, 2, 5, 10, 15, 30, 60];
    const step = steps.find((s) => s * pps >= 70) || 60;
    g.fillStyle = cssVar('--text-3');
    g.strokeStyle = '#30343b';
    g.font = '10px ' + cssVar('--mono');
    const first = Math.max(0, Math.floor(xa / pps / step) * step);
    for (let tt = first; tt <= duration && tt * pps <= xb; tt += step) {
      const x = Math.round(tt * pps) + 0.5;
      g.beginPath(); g.moveTo(x, 14); g.lineTo(x, H.ruler); g.stroke();
      g.fillText(fmtTime(tt, step < 1), x + 3, 11);
      const sub = step / 4;
      for (let k = 1; k < 4; k++) {
        const xs = Math.round((tt + sub * k) * pps) + 0.5;
        g.beginPath(); g.moveTo(xs, 20); g.lineTo(xs, H.ruler); g.stroke();
      }
    }
  };
  return html`<${ViewCanvas} scroller=${scroller} height=${H.ruler} draw=${draw} deps=${[pps, duration]} />`;
}

// Playhead: the only part of the timeline that follows the playback time every frame
function Playhead({ timeStore, pps, scroller, follow }) {
  const time = useStore(timeStore);
  useEffect(() => {
    const sc = scroller.current;
    if (!sc || !follow) return;
    const x = time * pps;
    if (x < sc.scrollLeft || x > sc.scrollLeft + sc.clientWidth - 30) sc.scrollLeft = Math.max(0, x - sc.clientWidth * 0.2);
  }, [Math.floor((time * pps) / 100), follow]);
  return html`<div class="playhead" style=${{ transform: `translateX(${time * pps}px)` }}></div>`;
}

export function Timeline({ proj, meta, ana, lyr, timeStore, slowTime, playing, sel, lineSel, onSeek, onSelect, onMoveSeg,
  onAddSeg, onSelectLine, onLyrics, onLyricsCommit, onMarker, height, setHeight, wordEdit,
  texts = [], onSelectText, onMoveText, onAddText, view = { hidden: [], collapsed: [] }, onToggleZone,
  kfFocus = null, onKeys, onContext, layers = [], onSelectLayer, onMoveClip, onAddClip, onAddLayer }) {
  const scroller = useRef();
  const namesRef = useRef();
  const [pps, setPps] = useState(null); // pixels per second
  const duration = meta.duration || 30;
  const effPps = pps ?? 12;
  const width = Math.max(200, Math.ceil(duration * effPps) + 40);

  // Zoom keeping one point still: the time under `offset` px (from the left edge of
  // the visible area; default its center) stays under it after the new scale.
  const zoomAnchor = useRef(null);
  const zoomTo = (np, offset) => {
    const sc = scroller.current;
    np = Math.max(1, Math.min(600, np));
    if (!sc || np === effPps) return;
    if (offset == null) offset = sc.clientWidth / 2;
    zoomAnchor.current = { tt: (sc.scrollLeft + offset) / effPps, offset };
    setPps(np);
  };
  useLayoutEffect(() => {
    const a = zoomAnchor.current, sc = scroller.current;
    if (!a || !sc) return;
    zoomAnchor.current = null;
    sc.scrollLeft = Math.max(0, a.tt * effPps - a.offset);
  }, [effPps]);

  // Fit the whole song on open and whenever the duration changes (e.g. after choosing the audio)
  useEffect(() => {
    if (scroller.current) setPps(Math.max(2, (scroller.current.clientWidth - 40) / duration));
  }, [duration]);

  // Snap points: markers, beats, cursor and segment starts
  const snaps = useMemo(() => {
    const s = [slowTime, ...proj.markers.map((m) => m.at)];
    for (const id in proj.tracks) proj.tracks[id].forEach((g) => s.push(g.at));
    if (ana) for (const b of ana.beats || []) s.push(b / ana.fps);
    return s;
  }, [proj, ana, slowTime]);
  const snap = (tt, ev) => {
    if (ev && ev.altKey) return tt;
    let best = tt, bd = 8 / effPps;
    for (const s of snaps) if (Math.abs(s - tt) < bd) { bd = Math.abs(s - tt); best = s; }
    return best;
  };

  const xToT = (clientX) => {
    const r = scroller.current.getBoundingClientRect();
    return Math.max(0, Math.min(duration, (clientX - r.left + scroller.current.scrollLeft) / effPps));
  };

  const drag = (e, onMove, onEnd) => {
    const startX = e.clientX;
    let moved = false;
    const mv = (ev) => {
      if (!moved && Math.abs(ev.clientX - startX) < 3) return;
      moved = true;
      onMove((ev.clientX - startX) / effPps, ev);
    };
    const up = () => {
      removeEventListener('mousemove', mv); removeEventListener('mouseup', up);
      onEnd && onEnd(moved);
    };
    addEventListener('mousemove', mv);
    addEventListener('mouseup', up);
  };

  // Ruler: dragging scrolls the view (grab and pull); a click without moving puts the cursor there
  const rulerDrag = (e) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const sc = scroller.current;
    const x0 = e.clientX, s0 = sc.scrollLeft;
    let moved = false;
    const mv = (ev) => {
      const dx = ev.clientX - x0;
      if (!moved && Math.abs(dx) > 3) { moved = true; document.body.classList.add('panning'); }
      if (moved) sc.scrollLeft = s0 - dx;
    };
    const up = (ev) => {
      removeEventListener('mousemove', mv);
      removeEventListener('mouseup', up);
      document.body.classList.remove('panning');
      if (!moved) onSeek(xToT(ev.clientX));
    };
    addEventListener('mousemove', mv);
    addEventListener('mouseup', up);
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
    const at0 = proj.tracks[track][i].at;
    drag(e, (dt, ev) => onMoveSeg(track, i, Math.max(0.01, Math.min(duration - 0.01, snap(at0 + dt, ev))), false),
      (moved) => moved && onMoveSeg(track, i, null, true));
  };

  // --- lyrics: drag whole lines (selected ones move together) and, for a single selected line, its words
  const lineDrag = (e, i) => {
    e.stopPropagation();
    if (e.button !== 0) return;
    const multi = e.shiftKey;
    // A line only moves once it is selected: the first click just selects it
    if (multi || sel.kind !== 'line' || !lineSel.includes(i)) { onSelectLine(i, multi); return; }
    const moving = lineSel;
    const orig = clone(lyr.lines);
    const start0 = orig[moving[0]].start;
    drag(e, (dt, ev) => {
      const shift = snap(start0 + dt, ev) - start0;
      const lines = clone(orig);
      for (const k of moving) {
        const l = lines[k];
        l.words.forEach((w) => { w.start = Math.max(0, w.start + shift); w.end = Math.max(0, w.end + shift); });
        l.start = l.words[0].start;
        l.end = l.words[l.words.length - 1].end;
      }
      onLyrics(lines);
    }, (moved) => moved && onLyricsCommit());
  };

  const wordDrag = (e, li, wi, edge) => {
    e.stopPropagation();
    if (e.button !== 0) return;
    const orig = clone(lyr.lines);
    const ws = orig[li].words;
    const w0 = ws[wi];
    const lo = wi > 0 ? ws[wi - 1].start + 0.03 : 0;
    const hi = wi + 1 < ws.length ? ws[wi + 1].start - 0.03 : duration;
    drag(e, (dt, ev) => {
      const lines = clone(orig);
      const w = lines[li].words[wi];
      if (edge === 'start') {
        // Left edge: change the start only
        w.start = Math.max(lo, Math.min(w0.end - 0.03, snap(w0.start + dt, ev)));
      } else if (edge === 'end') {
        // Right edge: change the end only (up to the next word, or the next line for the last one)
        const next = lyr.lines[li + 1];
        const max = wi + 1 < ws.length ? ws[wi + 1].start : next && next.words.length ? next.words[0].start : duration;
        w.end = Math.max(w0.start + 0.03, Math.min(max, snap(w0.end + dt, ev)));
      } else {
        const len = w0.end - w0.start;
        w.start = Math.max(lo, Math.min(hi - Math.min(len, 0.1), snap(w0.start + dt, ev)));
        w.end = w.start + len;
      }
      if (wi > 0 && lines[li].words[wi - 1].end > w.start) lines[li].words[wi - 1].end = w.start;
      if (wi + 1 < ws.length && edge !== 'end') w.end = Math.min(w.end, lines[li].words[wi + 1].start);
      const l = lines[li];
      l.start = l.words[0].start;
      l.end = l.words[l.words.length - 1].end;
      onLyrics(lines);
    }, (moved) => moved && onLyricsCommit());
  };

  // --- fixed texts: drag to move, edges change start / end
  const textDrag = (e, i, edge) => {
    e.stopPropagation();
    onSelectText(i);
    if (e.button !== 0) return;
    const { start: s0, end: e0 } = texts[i];
    drag(e, (dt, ev) => {
      let s = s0, en = e0;
      if (edge === 'start') s = Math.max(0, Math.min(e0 - 0.1, snap(s0 + dt, ev)));
      else if (edge === 'end') en = Math.min(duration, Math.max(s0 + 0.1, snap(e0 + dt, ev)));
      else {
        s = Math.max(0, Math.min(duration - (e0 - s0), snap(s0 + dt, ev)));
        en = s + (e0 - s0);
      }
      onMoveText(i, s, en, false);
    }, (moved) => moved && onMoveText(i, null, null, true));
  };
  // Overlapping texts go to separate rows of the lane
  const textRows = useMemo(() => {
    const ends = [], rowOf = [];
    texts.map((x, i) => i).sort((a, b) => texts[a].start - texts[b].start).forEach((i) => {
      let r = 0;
      while (ends[r] !== undefined && ends[r] > texts[i].start + 1e-6) r++;
      ends[r] = texts[i].end;
      rowOf[i] = r;
    });
    return { n: Math.max(1, ends.length), rowOf };
  }, [texts]);
  const textLaneH = textRows.n * H.textRow + 4;

  const wheel = (e) => {
    if (e.ctrlKey || e.metaKey) {
      e.preventDefault();
      // with the wheel the point under the pointer stays put
      zoomTo(effPps * (e.deltaY < 0 ? 1.18 : 1 / 1.18), e.clientX - scroller.current.getBoundingClientRect().left);
    }
  };

  const onScroll = () => { if (namesRef.current) namesRef.current.scrollTop = scroller.current.scrollTop; };

  const resize = (e) => {
    e.preventDefault();
    const y0 = e.clientY, h0 = height, el = e.currentTarget;
    el.classList.add('on');
    document.body.classList.add('resizing-row');
    const mv = (ev) => setHeight(Math.max(160, Math.min(innerHeight - 220, h0 + (y0 - ev.clientY))));
    const up = () => {
      removeEventListener('mousemove', mv); removeEventListener('mouseup', up);
      el.classList.remove('on');
      document.body.classList.remove('resizing-row');
    };
    addEventListener('mousemove', mv); addEventListener('mouseup', up);
  };

  const curLine = lyr.lines.findIndex((l) => slowTime >= l.start && slowTime <= l.end);
  // Word blocks only in edit mode ("Edit words" in the inspector)
  const editLine = wordEdit && sel.kind === 'line' && lineSel.length === 1 ? lineSel[0] : -1;
  // Entering word editing zooms in so the line takes about half of the view, centered
  useEffect(() => {
    const sc = scroller.current, l = lyr.lines[editLine];
    if (!sc || !l) return;
    const mid = (l.start + l.end) / 2;
    const want = Math.min(600, (sc.clientWidth * 0.5) / Math.max(0.5, l.end - l.start));
    if (want > effPps * 1.05) {
      zoomAnchor.current = { tt: mid, offset: sc.clientWidth / 2 };
      setPps(want);
    } else sc.scrollLeft = Math.max(0, mid * effPps - sc.clientWidth / 2);
  }, [editLine]);

  // --- rows, top to bottom: ruler and audio (always visible, they stay at the top), then each
  // zone with a header (click: fold / unfold) and the lanes of its entries that are not hidden.
  // The names column and the lanes are built from the same list so they always line up.
  const hidden = new Set(view.hidden || []), folded = new Set(view.collapsed || []);

  // --- keyframes: a diamond per instant with keys (of every property, or only of the focused
  // one in the selected segment / text). Drag: move them (snaps; within the segment); click: go
  // there; right-click: easing towards the next key, or delete.
  const keyDrag = (e, target, keys, k, base, end) => {
    e.stopPropagation();
    if (e.button !== 0) return;
    const orig = keys;
    drag(e, (dt, ev) => {
      const nt = Math.max(0, Math.min(end - base, snap(base + k.t + dt, ev) - base));
      onKeys(target, () => editKeysAt(orig, k.t, k.paths, { to: nt }));
    }, (moved) => { if (!moved) onSeek(base + k.t); });
  };
  const keyMarks = (keys, base, end, target, isSel, top) => keyTimes(keys, isSel ? kfFocus : null).map((k) => html`<div
      class=${'kf' + (isSel ? ' sel' : '') + (k.ease === 'step' ? ' hold' : '')} key=${'k' + k.t}
      style=${{ left: (base + k.t) * effPps + 'px', top: top == null ? '50%' : top + 'px' }}
      title=${fmtTime(base + k.t) + ' · ' + k.paths.join(', ') + ' — ' + t('drag to move · right-click: curve, delete')}
      onMouseDown=${(e) => keyDrag(e, target, keys, k, base, end)} onDblClick=${(e) => e.stopPropagation()}
      onContextMenu=${(e) => keyMenu(e, target, keys, k)}></div>`);
  // Right-click on a key: the curve towards the next key (each drawn), or delete
  const keyMenu = (e, target, keys, k) => openMenu(e, [
    { header: t('Curve to the next key') },
    ...EASES.map(([id, label]) => ({ label: t(label), icon: html`<${EaseIcon} name=${id} />`, checked: k.ease === id,
      onClick: () => onKeys(target, (ks) => editKeysAt(ks, k.t, k.paths, { ease: id })) })),
    '-',
    { label: t(k.paths.length > 1 ? 'Delete these keys' : 'Delete the key'), icon: 'trash-2', danger: true,
      onClick: () => onKeys(target, (ks) => editKeysAt(ks, k.t, k.paths, { remove: true })) },
  ]);
  const trackRow = (tr) => {
    const segs = proj.tracks[tr.id] || [];
    const on = sel.kind === 'track' && sel.track === tr.id;
    return {
      key: tr.id, h: H.track, on, sel: on, onName: () => onSelect(tr.id, null),
      onNameCtx: (e) => onContext(e, { kind: 'entry', id: tr.id }),
      name: html`<${Icon} name=${tr.icon} size=${14} /> ${t(tr.label)}`,
      onDown: seekDrag, onDbl: (e) => onAddSeg(tr.id, snap(xToT(e.clientX), e)),
      lane: segs.map((s, i) => {
        const end = i + 1 < segs.length ? segs[i + 1].at : duration;
        const son = on && sel.seg === i;
        const fade = s.transition && s.transition.mode !== 'cut' && i > 0 ? s.transition.duration * effPps : 0;
        const enabled = tr.id === 'background' ? !!s.cfg.path : s.cfg.enabled !== false;
        return html`<div class=${'seg' + (son ? ' on' : '') + (enabled ? '' : ' off')}
            style=${{ left: s.at * effPps + 'px', width: Math.max(3, (end - s.at) * effPps - 1) + 'px' }}
            title=${(s.label ? s.label + ' · ' : '') + fmtTime(s.at) + ' → ' + fmtTime(end)}
            onMouseDown=${(e) => segDrag(e, tr.id, i)} onDblClick=${(e) => e.stopPropagation()}
            onContextMenu=${(e) => onContext(e, { kind: 'seg', track: tr.id, seg: i, at: snap(xToT(e.clientX), e) })}>
          ${fade > 0 && html`<div class="fade" style=${{ width: fade + 'px' }}></div>`}
          ${i > 0 && html`<div class="grip"></div>`}
          <span class="t">${s.label ? html`<b>${s.label}</b> · ` : ''}${segSummary(tr.id, s.cfg)}</span>
        </div>${s.keys && keyMarks(s.keys, s.at, end, { track: tr.id, seg: i }, on && sel.seg === i)}`;
      }),
    };
  };
  const linesRow = {
    key: 'lines', h: H.lyrics, onName: () => onSelect('lyrics', 0), onDown: seekDrag,
    name: html`<${Icon} name="text-cursor-input" size=${14} /> ${t('Lyric lines')}`,
    lane: html`${lyr.lines.map((l, i) => {
      const cls = 'lyrline' + (i === editLine ? ' edit' : lineSel.includes(i) ? (lineSel.length > 1 ? ' multi' : ' on') : i === curLine ? ' now' : '');
      return html`<div class=${cls} key=${i}
        style=${{ left: l.start * effPps + 'px', width: Math.max(4, (l.end - l.start) * effPps) + 'px' }}
        title=${l.words.map((w) => w.text).join(' ') + ' — ' + t(sel.kind === 'line' && lineSel.includes(i) ? 'drag to move · Shift+click: select several' : 'click to select · Shift+click: select several')}
        onMouseDown=${(e) => lineDrag(e, i)}>${i === editLine ? '' : l.words.map((w) => w.text).join(' ')}</div>`;
    })}
    ${editLine >= 0 && lyr.lines[editLine].words.map((w, wi) => html`<div class=${'lyrword' + (slowTime >= w.start && slowTime < w.end ? ' now' : '')} key=${'w' + wi}
        style=${{ left: w.start * effPps + 'px', width: Math.max(6, (w.end - w.start) * effPps - 1) + 'px' }}
        title=${w.text + ' · ' + fmtTime(w.start) + ' — ' + t('drag to move, edges = start / end')}
        onMouseDown=${(e) => wordDrag(e, editLine, wi, false)}>
        <div class="grip" onMouseDown=${(e) => wordDrag(e, editLine, wi, 'start')}></div>${w.text}<div class="grip end" onMouseDown=${(e) => wordDrag(e, editLine, wi, 'end')}></div></div>`)}`,
  };
  const textsRow = {
    key: 'texts', h: textLaneH, on: sel.kind === 'text', sel: sel.kind === 'text',
    onName: () => onSelectText(texts.length ? 0 : -1), title: t('Double-click the lane: new text at that point'),
    name: html`<${Icon} name="type" size=${14} /> ${t('Texts')}`,
    onDown: seekDrag, onDbl: (e) => onAddText(snap(xToT(e.clientX), e)),
    onCtx: (e) => onContext(e, { kind: 'texts', at: snap(xToT(e.clientX), e) }),
    onNameCtx: (e) => onContext(e, { kind: 'entry', id: 'texts' }),
    lane: texts.map((x, i) => {
      const on = sel.kind === 'text' && sel.i === i;
      return html`<div class=${'seg txt' + (on ? ' on' : '') + (x.enabled === false ? ' off' : '')} key=${'tx' + i}
          style=${{ left: x.start * effPps + 'px', width: Math.max(4, (x.end - x.start) * effPps - 1) + 'px',
            top: 3 + textRows.rowOf[i] * H.textRow + 'px', '--rowh': H.textRow + 'px' }}
          title=${(x.text || '').replace(/\n/g, ' / ') + ' · ' + fmtTime(x.start) + ' → ' + fmtTime(x.end)}
          onMouseDown=${(e) => textDrag(e, i)} onDblClick=${(e) => e.stopPropagation()}
          onContextMenu=${(e) => onContext(e, { kind: 'text', i })}>
        <div class="grip" onMouseDown=${(e) => textDrag(e, i, 'start')}></div>
        <span class="t">${x.label ? html`<b>${x.label}</b> · ` : ''}${(x.text || '').replace(/\n/g, ' / ')}</span>
        <div class="grip end" onMouseDown=${(e) => textDrag(e, i, 'end')}></div>
      </div>${x.keys && keyMarks(x.keys, x.start, x.end, { text: i }, on, 3 + textRows.rowOf[i] * H.textRow + H.textRow / 2)}`;
    }),
  };
  // --- overlay layers: one lane per layer (front first), its clips as blocks: drag to move,
  // edges change start / end
  const clipDrag = (e, id, j, edge) => {
    e.stopPropagation();
    onSelectLayer(id, j);
    if (e.button !== 0) return;
    const c = layers.find((x) => x.l.id === id).l.clips[j];
    const s0 = c.start, e0 = c.end;
    drag(e, (dt, ev) => {
      let st = s0, en = e0;
      if (edge === 'start') st = Math.max(0, Math.min(e0 - 0.1, snap(s0 + dt, ev)));
      else if (edge === 'end') en = Math.min(duration, Math.max(s0 + 0.1, snap(e0 + dt, ev)));
      else {
        st = Math.max(0, Math.min(duration - (e0 - s0), snap(s0 + dt, ev)));
        en = st + (e0 - s0);
      }
      onMoveClip(id, j, st, en, false);
    }, (moved) => moved && onMoveClip(id, j, null, null, true));
  };
  const layerRow = ({ l, i }) => {
    const on = sel.kind === 'layer' && sel.id === l.id;
    return {
      key: 'layer-' + l.id, h: H.track, on, sel: on,
      name: html`<${Icon} name="layers" size=${14} /> ${l.name || t('Layer {n}', { n: i + 1 })}`,
      onName: () => onSelectLayer(l.id), onNameCtx: (e) => onContext(e, { kind: 'layer', id: l.id }),
      title: t('Double-click the lane: new clip at that point'),
      onDown: seekDrag, onDbl: (e) => onAddClip(l.id, snap(xToT(e.clientX), e)),
      onCtx: (e) => onContext(e, { kind: 'layerlane', id: l.id, at: snap(xToT(e.clientX), e) }),
      lane: l.clips.map((c, j) => {
        const son = on && sel.clip === j;
        const name = c.label || (c.path || '').split(/[\\/]/).pop() || t('Clip {n}', { n: j + 1 });
        return html`<div class=${'seg clip' + (son ? ' on' : '')} key=${'c' + j}
            style=${{ left: c.start * effPps + 'px', width: Math.max(4, (c.end - c.start) * effPps - 1) + 'px' }}
            title=${name + ' · ' + fmtTime(c.start) + ' → ' + fmtTime(c.end)}
            onMouseDown=${(e) => clipDrag(e, l.id, j)} onDblClick=${(e) => e.stopPropagation()}
            onContextMenu=${(e) => onContext(e, { kind: 'clip', id: l.id, clip: j, at: snap(xToT(e.clientX), e) })}>
          <div class="grip" onMouseDown=${(e) => clipDrag(e, l.id, j, 'start')}></div>
          <span class="t">${name}</span>
          <div class="grip end" onMouseDown=${(e) => clipDrag(e, l.id, j, 'end')}></div>
        </div>${c.keys && keyMarks(c.keys, c.start, c.end, { layer: l.id, clip: j }, son)}`;
      }),
    };
  };
  const byId = Object.fromEntries(TRACKS.map((tr) => [tr.id, tr]));
  const zoneLanes = {
    main: [
      !hidden.has('background') && trackRow(byId.background),
      !hidden.has('lyrics') && linesRow,
      !hidden.has('lyrics') && trackRow(byId.lyrics),
      !hidden.has('texts') && textsRow,
    ],
    layers: layers.length ? layers.filter(({ l }) => !hidden.has('layer:' + l.id)).map(layerRow)
      : [{ key: 'layers-empty', h: H.track, cls: 'empty', name: '', onDbl: (e) => onAddLayer(snap(xToT(e.clientX), e)),
        lane: t('No layers yet: “+ Layer” in the left panel (or double-click here) adds one with an image, GIF or video') }],
    effects: TRACKS.filter((tr) => zoneOf(tr.id) === 'effects' && !hidden.has(tr.id)).map(trackRow),
  };
  const zoneEntries = { main: ['background', 'lyrics', 'texts'], layers: layers.map(({ l }) => 'layer:' + l.id), effects: TRACKS.filter((tr) => zoneOf(tr.id) === 'effects').map((tr) => tr.id) };
  const rows = [
    { key: 'ruler', h: H.ruler, cls: 'ruler sticky', top: 0, name: '', onDown: rulerDrag,
      onCtx: (e) => onContext(e, { kind: 'time', at: snap(xToT(e.clientX), e) }), laneTitle: t('Drag to scroll the timeline · click to move the cursor'),
      lane: html`<${Ruler} pps=${effPps} duration=${duration} scroller=${scroller} />` },
    { key: 'audio', h: H.audio, cls: 'sticky', top: H.ruler + 'px', on: sel.kind === 'audio', onDown: seekDrag,
      onCtx: (e) => onContext(e, { kind: 'time', at: snap(xToT(e.clientX), e) }),
      name: html`<${Icon} name="audio-waveform" size=${14} /> ${t('Audio')}`,
      lane: html`<${AudioLane} ana=${ana} pps=${effPps} scroller=${scroller} />` },
  ];
  for (const z of ZONES) {
    const nHidden = zoneEntries[z.id].filter((id) => hidden.has(id)).length;
    const isFolded = folded.has(z.id);
    rows.push({ key: 'zone-' + z.id, h: H.zone, cls: 'zh z-' + z.id, onName: () => onToggleZone(z.id),
      onNameCtx: (e) => onContext(e, { kind: 'zone', id: z.id }),
      title: t(isFolded ? 'Click to unfold' : 'Click to fold'),
      name: html`<${Icon} name=${isFolded ? 'chevron-right' : 'chevron-down'} size=${13} />${t(z.label)}
        ${nHidden > 0 && html`<span class="n">${t(nHidden === 1 ? '1 hidden' : '{n} hidden', { n: nHidden })}</span>`}`,
      onDown: seekDrag, lane: '' });
    if (!isFolded) for (const r of zoneLanes[z.id]) if (r) rows.push({ ...r, cls: (r.cls ? r.cls + ' ' : '') + 'zl z-' + z.id, onDown: r.onDown || seekDrag });
  }

  return html`<div class="timeline">
    <div class="tl-resize" onMouseDown=${resize} onDblClick=${() => setHeight(null)}
      title=${t('Drag to resize · double-click: default size')}></div>
    <div class="tl-bar">
      <span class="label-sm" style="color:var(--text-2)">${t('Timeline')}</span>
      <span class="hint">${t('Double-click a track: new segment · Drag to move (snaps; Alt = free) · Ctrl + wheel: zoom · Drag the ruler: scroll')}</span>
      <span class="spacer"></span>
      <span class="legend">
        <span><i style="background:var(--band-bass)"></i>${t('Bass')}</span>
        <span><i style="background:var(--band-mid)"></i>${t('Mids')}</span>
        <span><i style="background:var(--band-treble)"></i>${t('Treble')}</span>
      </span>
      <button class="btn sm" title=${t('Marker at the cursor (M)')} onClick=${() => onMarker('add')}><${Icon} name="flag" size=${14} /> ${t('Marker')}</button>
      <div class="btngroup">
        <button class="btn sm icon" title=${t('Zoom out')} onClick=${() => zoomTo(effPps / 1.5)}><${Icon} name="zoom-out" size=${14} /></button>
        <button class="btn sm icon" title=${t('Zoom in')} onClick=${() => zoomTo(effPps * 1.5)}><${Icon} name="zoom-in" size=${14} /></button>
        <button class="btn sm icon" title=${t('Show the whole song')} onClick=${() => setPps(Math.max(1, (scroller.current.clientWidth - 40) / duration))}><${Icon} name="maximize-2" size=${14} /></button>
      </div>
    </div>
    <div class="tl-body">
      <div class="tl-names" ref=${namesRef}>
        ${rows.map((r) => html`<div key=${r.key} class=${'nm ' + (r.cls || '') + (r.on ? ' on' : '')} style=${{ '--h': r.h + 'px', top: r.top }}
            onClick=${r.onName} onContextMenu=${r.onNameCtx} title=${r.title || ''}>${r.name}</div>`)}
      </div>
      <div class="tl-scroll" ref=${scroller} onWheel=${wheel} onScroll=${onScroll}>
        <div class="tl-inner" style=${{ width: width + 'px' }}>
          ${rows.map((r) => html`<div key=${r.key} class=${'lane ' + (r.cls || '') + (r.sel ? ' sel' : '')} style=${{ '--h': r.h + 'px', top: r.top }}
              onMouseDown=${r.onDown} onDblClick=${r.onDbl} onContextMenu=${r.onCtx} title=${r.laneTitle || null}>${r.lane}</div>`)}
          ${proj.markers.map((m, i) => html`<div class="marker" style=${{ left: m.at * effPps + 'px' }}>
            <b title=${t('Click: go · double-click: rename · right-click: menu')}
              onMouseDown=${(e) => { e.stopPropagation(); if (e.button === 0) onSeek(m.at); }}
              onDblClick=${() => onMarker('rename', i)}
              onContextMenu=${(e) => onContext(e, { kind: 'marker', i })}><${Icon} name="flag" size=${11} /> ${m.label}</b>
          </div>`)}
          <${Playhead} timeStore=${timeStore} pps=${effPps} scroller=${scroller} follow=${playing} />
        </div>
      </div>
    </div>
  </div>`;
}
