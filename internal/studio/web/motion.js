import { html } from './vendor/preact-htm.js';
import { Form, Section } from './fields.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';

// Motion along a path (config.Motion) of a clip or a fixed text: the path is drawn in the editor
// (its path tool), the progress along it (0 start … 1 end; a closed path loops past 1) is keyed
// like any property.

export const MOTION_FIELDS = [
  { key: 'progress', label: 'Progress along the path', type: 'num', min: 0, max: 4, step: 0.001,
    help: '0 = start, 1 = end (a closed path goes round again past 1). Key it: the curves give the motion.' },
  { key: 'orient', label: 'Turn with the path', type: 'bool', anim: false, help: 'Its rotation follows the direction of the path' },
];

const hasPath = (o) => !!(o && o.motion && o.motion.path && (o.motion.path.points || []).length >= 2);

// Sets the path of an owner (inside an update): fn(current [path] or []) → new list (its last
// shape is the path; empty = no path). A new path gets progress keys from 0 at its start to 1 at
// its end, eased, unless it has some.
export function setPath(o, fn) {
  const cur = hasPath(o) ? [o.motion.path] : [];
  const ns = fn(cur);
  const sh = ns.length ? ns[ns.length - 1] : null;
  if (!sh) { removePath(o); return; }
  const fresh = !hasPath(o);
  o.motion = { progress: 0, ...(o.motion || {}), path: sh };
  if (fresh && !(o.keys && o.keys['motion.progress'])) {
    const d = +Math.max(0.1, (o.end ?? 0) - (o.start ?? 0)).toFixed(3);
    o.keys = { ...(o.keys || {}), 'motion.progress': [{ t: 0, v: 0, ease: 'ease_in_out' }, { t: d, v: 1 }] };
  }
}
export function removePath(o) {
  delete o.motion;
  if (o.keys && o.keys['motion.progress']) {
    const ks = { ...o.keys };
    delete ks['motion.progress'];
    o.keys = Object.keys(ks).length ? ks : undefined;
    if (!o.keys) delete o.keys;
  }
}

// Moving an object that follows a path moves the path (dx, dy: 0..1 of the frame)
export function movePath(o, dx, dy) {
  o.motion = { ...o.motion, path: { ...o.motion.path, points: o.motion.path.points.map((p) => [+(p[0] + dx).toFixed(5), +(p[1] + dy).toFixed(5), ...p.slice(2)]) } };
}
export { hasPath };

// The section of the inspector
export function MotionSection({ obj, kf, ctx, edit, what = 'clip' }) {
  const on = hasPath(obj);
  return html`<${Section} id="motion" title=${t('Motion path')}>
    ${!on && html`<p class="faint" style="margin:2px 0 8px;font-size:12px">${t(what === 'text'
      ? 'The text can travel along a path drawn on the screen: draw it in the editor, then key its progress (it goes from start to end over the text\'s time).'
      : 'The clip can travel along a path drawn on the screen: draw it in the editor, then key its progress (it goes from start to end over the clip\'s time).')}</p>`}
    ${on && html`<${Form} fields=${MOTION_FIELDS} value=${obj.motion} ctx=${ctx} kf=${kf} prefix="motion"
      onChange=${(nm) => edit((o) => { o.motion = { ...nm, path: o.motion.path }; })} />`}
    <div class="row" style="margin-top:6px">
      <button class="btn sm" onClick=${() => ctx.openEditor('path')}><${Icon} name="route" size=${13} /> ${t(on ? 'Edit the path' : 'Draw a path')}</button>
      ${on && html`<span class="spacer"></span><button class="btn sm ghost danger" title=${t('It stays where it is placed (Position)')}
        onClick=${() => edit((o) => removePath(o))}><${Icon} name="trash-2" size=${13} /> ${t('Remove the path')}</button>`}
    </div>
  </${Section}>`;
}
