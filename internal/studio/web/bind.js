// Tying clips and background effects to background segments: they are seen (or apply) only
// while one of those segments is on screen, and during a transition only on its image
// (config.ClipConfig.Bind, config.Seg.Bind; the segments' ids, config.Seg.ID)
import { html } from './vendor/preact-htm.js';
import { Section } from './fields.js';
import { Icon } from './icons.js';
import { fmtTime } from './util.js';
import { t } from './i18n.js';

// the background effects that can be tied to background segments
export const BIND_TRACKS = ['bass_zoom', 'shake', 'chromatic_aberration', 'ripple', 'glitch', 'color_flash'];

// a new id for a background segment (not used by another one)
function newSegId(segs) {
  for (;;) {
    const id = 'bg' + Math.random().toString(36).slice(2, 8);
    if (!segs.some((s) => s.id === id)) return id;
  }
}

// bound: whether a clip or a segment is tied to background segments that are still there
export function bound(proj, bind) {
  const segs = (proj.tracks && proj.tracks.background) || [];
  return (bind || []).some((id) => segs.some((s) => s.id === id));
}

// BindSection: the background segments obj (a clip or an effect's segment) belongs to. update:
// the project's update; target(p): obj in the project being changed
export function BindSection({ proj, bind, update, target, what, time }) {
  const segs = (proj.tracks && proj.tracks.background) || [];
  const on = new Set((bind || []).filter((id) => segs.some((s) => s.id === id)));
  if (segs.length < 2 && on.size === 0) return null;
  const toggle = (k) => update((p) => {
    const all = p.tracks.background, s = all[k];
    if (!s) return;
    if (!s.id) s.id = newSegId(all);
    const o = target(p);
    if (!o) return;
    const b = new Set((o.bind || []).filter((id) => all.some((x) => x.id === id)));
    if (b.has(s.id)) b.delete(s.id);
    else b.add(s.id);
    if (b.size) o.bind = all.filter((x) => b.has(x.id)).map((x) => x.id);
    else delete o.bind;
  });
  // the background segment on screen at the cursor
  let now = -1;
  segs.forEach((s, k) => { if (time != null && (k === 0 || s.at <= time)) now = k; });
  const clear = () => update((p) => { const o = target(p); if (o) delete o.bind; });
  return html`<${Section} id="bind" title=${t('Background segments')}
      extra=${on.size > 0 && html`<span class="bind-n"><${Icon} name="link" size=${11} />${on.size}</span>`}
      help=${'Tie it to background segments: it is seen only while one of them is on screen, and during a transition only on its image (its light, particles and effects do not reach the other one). Its own times stay as they are. None chosen: on every background.'}>
    <div class="bindchips">${segs.map((s, k) => html`<button class=${'btn sm' + (s.id && on.has(s.id) ? ' on' : '') + (k === now ? ' now' : '')}
        title=${(s.label || t('Segment {n}', { n: k + 1 })) + ' · ' + t('Starts at {t}', { t: fmtTime(s.at) }) + (k === now ? ' · ' + t('On screen at the cursor') : '')} onClick=${() => toggle(k)}>
        <span class="mono bt">${fmtTime(s.at, false)}</span><span class="bn">${s.label || t('Segment {n}', { n: k + 1 })}</span></button>`)}</div>
    <div class="card-note">${on.size
      ? t(what === 'effect' ? 'Applies only while one of them is on screen; in a transition, only to its image.' : 'Seen only while one of them is on screen; in a transition, only on its image.')
      : t(what === 'effect' ? 'Applies to every background. Choose segments to tie it to them.' : 'On every background. Choose segments to tie it to them.')}
      ${on.size > 0 && html` <a href="#" class="bind-untie" onClick=${(e) => { e.preventDefault(); clear(); }}>${t('Untie')}</a>`}</div>
  </${Section}>`;
}
