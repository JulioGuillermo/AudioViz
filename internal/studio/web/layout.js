// Box layout for the image models that place elements where you say (imagegen Family.Layout):
// boxes drawn on a canvas of the output's shape (over the first reference image, when editing),
// each with a name, a description and optional text, written into the prompt in the model's
// format when generating:
//   flux3 (FLUX.3 Image Klein): the description names each element as <id>, then a JSON list of
//     { id, bbox: [top, left, bottom, right] in 0..1000, desc }
//   moss (MOSS-Image): a JSON layout { summary: { caption }, items: [{ label, caption, text,
//     bbox: [y0, x0, y1, x1] }] } in pixels with the canvas centre as the origin
// The canvas (BoxCanvas) goes in the generator's big area; the selected box's fields (BoxFields)
// beside the prompt.

import { html, useRef } from './vendor/preact-htm.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { thumbURL } from './dialogs.js';

// "Red bicycle" -> "red_bicycle_1" (the number keeps ids apart)
const slug = (s, i) => ((s || '').toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '')
  .replace(/[^a-z0-9]+/g, '_').replace(/^_+|_+$/g, '') || 'element') + '_' + (i + 1);

const clamp = (v) => Math.max(0, Math.min(1, v));

// The prompt for the model: the description plus the boxes (none: the description alone)
export function layoutPrompt(format, caption, boxes, w, h) {
  const bs = (boxes || []).filter((b) => b.x1 - b.x0 > 0.005 && b.y1 - b.y0 > 0.005);
  if (!format || !bs.length) return caption;
  if (format === 'flux3') {
    const ids = bs.map((b, i) => slug(b.name, i));
    let text = caption.trim();
    const missing = ids.filter((id) => !text.includes('<' + id + '>'));
    if (missing.length) {
      text += (text && !/[.!?]$/.test(text) ? '.' : '') + ' ' + bs.map((b, i) => (missing.includes(ids[i]) ? (b.name || 'element') + ' <' + ids[i] + '>' : null))
        .filter(Boolean).join(', ') + '.';
    }
    const n = (v) => Math.round(clamp(v) * 1000);
    const rows = bs.map((b, i) => ({ id: ids[i], bbox: [n(b.y0), n(b.x0), n(b.y1), n(b.x1)],
      desc: (b.desc || b.name || '').trim() + (b.text ? (b.desc ? ' ' : '') + 'With the text "' + b.text + '".' : '') }));
    return text.trim() + ' ' + JSON.stringify(rows);
  }
  if (format === 'moss') {
    const px = (v, size) => Math.round(clamp(v) * size - size / 2);
    const items = bs.map((b) => {
      const it = { label: b.kind === 'text' || b.text ? 'text' : 'object', caption: (b.desc || b.name || '').trim() };
      if (b.text) it.text = b.text;
      it.bbox = [px(b.y0, h), px(b.x0, w), px(b.y1, h), px(b.x1, w)];
      return it;
    });
    return JSON.stringify({ summary: { meta: [], canvas: 'single', caption: caption.trim() }, items,
      bbox_coord_space: 'abs_centralized', bbox_format: 'yxyx' });
  }
  return caption;
}

// The canvas: draw (drag on empty space), move (drag a box), resize (its corner), select. maxH:
// the largest height it may take (CSS length); it keeps the output's shape within the width.
export function BoxCanvas({ w, h, boxes, setBoxes, bg, sel, setSel, maxH = '260px' }) {
  const area = useRef();
  const pos = (e) => {
    const r = area.current.getBoundingClientRect();
    return { x: clamp((e.clientX - r.left) / r.width), y: clamp((e.clientY - r.top) / r.height) };
  };
  const drag = (e, i, mode) => {
    if (e.button !== 0) return;
    e.preventDefault();
    e.stopPropagation();
    const p0 = pos(e);
    let list = boxes.map((b) => ({ ...b }));
    let k = i;
    if (mode === 'new') {
      list.push({ x0: p0.x, y0: p0.y, x1: p0.x, y1: p0.y, name: '', desc: '', text: '', kind: 'object' });
      k = list.length - 1;
    }
    const b0 = { ...list[k] };
    setSel(k);
    const move = (ev) => {
      const p = pos(ev);
      const b = list[k];
      if (mode === 'new') {
        b.x0 = Math.min(p0.x, p.x); b.x1 = Math.max(p0.x, p.x);
        b.y0 = Math.min(p0.y, p.y); b.y1 = Math.max(p0.y, p.y);
      } else if (mode === 'move') {
        const dx = Math.max(-b0.x0, Math.min(1 - b0.x1, p.x - p0.x));
        const dy = Math.max(-b0.y0, Math.min(1 - b0.y1, p.y - p0.y));
        b.x0 = b0.x0 + dx; b.x1 = b0.x1 + dx; b.y0 = b0.y0 + dy; b.y1 = b0.y1 + dy;
      } else {
        b.x1 = Math.max(b0.x0 + 0.02, p.x); b.y1 = Math.max(b0.y0 + 0.02, p.y);
      }
      list = [...list];
      setBoxes(list);
    };
    const up = () => {
      removeEventListener('pointermove', move); removeEventListener('pointerup', up);
      const b = list[k];
      if (mode === 'new' && (b.x1 - b.x0 < 0.02 || b.y1 - b.y0 < 0.02)) {
        list.splice(k, 1);
        setSel(-1);
      }
      setBoxes([...list]);
    };
    addEventListener('pointermove', move); addEventListener('pointerup', up);
  };
  const pct = (v) => (v * 100).toFixed(2) + '%';
  return html`<div class="bx-area" ref=${area}
      style=${{ aspectRatio: w + ' / ' + h, width: 'min(100%, calc(' + maxH + ' * ' + (w / h).toFixed(4) + '))',
        backgroundImage: bg ? 'url(' + thumbURL(bg) + ')' : '' }}
      onPointerDown=${(e) => drag(e, -1, 'new')}>
    ${boxes.map((b, i) => html`<div class=${'bx-box' + (i === sel ? ' on' : '') + (b.text || b.kind === 'text' ? ' txt' : '')} key=${i}
        style=${{ left: pct(b.x0), top: pct(b.y0), width: pct(b.x1 - b.x0), height: pct(b.y1 - b.y0) }}
        onPointerDown=${(e) => drag(e, i, 'move')}>
      <span class="bx-tag">${b.name || t('Element {n}', { n: i + 1 })}</span>
      <i class="bx-size" onPointerDown=${(e) => drag(e, i, 'size')}></i></div>`)}
    ${!boxes.length && html`<span class="bx-empty">${t('Drag to draw a box')}</span>`}
  </div>`;
}

// The selected box's fields (or a hint), and clearing them all
export function BoxFields({ format, boxes, setBoxes, sel, setSel }) {
  const edit = (k, v) => setBoxes(boxes.map((b, i) => (i === sel ? { ...b, [k]: v } : b)));
  const cur = boxes[sel];
  return html`<div class="bx-edit">
    ${cur ? html`
      <div class="bx-row"><input type="text" value=${cur.name} placeholder=${t('Name, e.g. red bicycle')} onInput=${(e) => edit('name', e.target.value)} />
        <button class="btn icon sm ghost" title=${t('Delete the box')} onClick=${() => { setBoxes(boxes.filter((_, i) => i !== sel)); setSel(-1); }}>
          <${Icon} name="trash-2" size=${13} /></button></div>
      <textarea class="bx-desc" rows="2" value=${cur.desc} placeholder=${t('What it looks like: shape, colour, material, style…')} onInput=${(e) => edit('desc', e.target.value)}></textarea>
      <div class="bx-row"><input type="text" value=${cur.text} placeholder=${t('Text written in it (optional)')} onInput=${(e) => edit('text', e.target.value)} />
        ${format === 'moss' && html`<select value=${cur.text ? 'text' : cur.kind} disabled=${!!cur.text} title=${t('Kind')} onChange=${(e) => edit('kind', e.target.value)}>
          <option value="object">${t('Object')}</option><option value="text">${t('Text')}</option></select>`}</div>`
    : html`<p class="faint ig-hint">${t(boxes.length ? 'Select a box to describe it; drag its corner to resize it.' : 'Draw boxes on the canvas on the right.')}</p>`}
    ${boxes.length > 0 && html`<button class="btn sm ghost bx-clear" onClick=${() => { setBoxes([]); setSel(-1); }}>
      <${Icon} name="x" size=${12} /> ${t('Remove all the boxes')}</button>`}
  </div>`;
}
