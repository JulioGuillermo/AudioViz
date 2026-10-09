import { html } from './vendor/preact-htm.js';
import { Form, Section, Edit, adjusting } from './fields.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { EASES } from './keys.js';
import { nodeVals, setVals, depthTint } from './masks.js';

// Motion along a path (config.Motion) of a clip or a fixed text: the path is drawn in the editor
// (its path tool), the progress along it (0 start … 1 end; a closed path loops past 1) is keyed
// like any property.

export const MOTION_FIELDS = [
  { key: 'progress', label: 'Progress along the path', type: 'num', min: 0, max: 4, step: 0.001,
    help: '0 = start, 1 = end (a closed path goes round again past 1). Key it: the curves give the motion.' },
  { key: 'orient', label: 'Turn with the path', type: 'bool', anim: false, help: 'Its rotation follows the direction of the path' },
  { key: 'flip', label: 'Going back', type: 'enum', def: '', anim: false, showIf: (c) => c.orient || c.bend,
    options: [['', 'Upside down'], ['letters', 'Each letter turns round (mirror)'], ['twist', 'Twists like a ribbon (mirror)'],
      ['wrap', 'Goes round like a ribbon on a cylinder (mirror)'], ['readable', 'Turns like a card (readable)']],
    help: 'When the path goes back (to the left): it goes upside down as the path is; each letter turns round like a card, seen from behind; it twists round like a ribbon, narrowing little by little and showing its back after; it goes round like a ribbon on a cylinder: its letters stay upright and follow the path, seen from behind going back and slanted where the path goes up or down, the far side going behind (by the path\'s depth, if its nodes have one); or it turns round like a card and stays readable' },
];
// a fixed text can also bend its letters along the path
const TEXT_MOTION_FIELDS = [...MOTION_FIELDS.filter((f) => f.key !== 'flip'),
  { key: 'bend', label: 'Bend along the path', type: 'bool', anim: false,
    help: 'Its letters follow the shape of the path, each turned with it (the progress places its middle)' },
  MOTION_FIELDS.find((f) => f.key === 'flip'),
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

// The particles' own route of an emitter clip (o.emitter.route), set like the path
const hasRoute = (o) => !!(o && o.emitter && o.emitter.route && (o.emitter.route.points || []).length >= 2);
export function setRoute(o, fn) {
  const ns = fn(hasRoute(o) ? [o.emitter.route] : []);
  const sh = ns.length ? ns[ns.length - 1] : null;
  if (!sh) { removeRoute(o); return; }
  o.emitter = { ...o.emitter, route: sh };
}
export function removeRoute(o) {
  if (!o.emitter) return;
  const e = { ...o.emitter };
  delete e.route;
  delete e.route_fixed;
  o.emitter = e;
}

// The inspector's section of an emitter's route
export function RouteSection({ obj, ctx, edit, kf }) {
  const on = hasRoute(obj);
  return html`<${Section} id="route" title=${t('Particles\' route')} help=${'The particles can follow a route of their own while they live (flames licking along a line, a stream that turns): draw it in the editor. They go along it at their speed.'}>
    ${on && html`<${Form} fields=${ROUTE_FIELDS} value=${obj.emitter} ctx=${ctx} kf=${kf} prefix="emitter"
      onChange=${(ne) => edit((o) => { o.emitter = { ...o.emitter, route_strength: ne.route_strength, route_fixed: ne.route_fixed || undefined }; })} />`}
    <div class="row" style="margin-top:6px">
      <button class="btn sm" onClick=${() => ctx.openEditor('path', 'route')}><${Icon} name="route" size=${13} /> ${t(on ? 'Edit the route' : 'Draw the route')}</button>
      ${on && html`<span class="spacer"></span><button class="btn sm ghost danger"
        onClick=${() => edit((o) => removeRoute(o))}><${Icon} name="trash-2" size=${13} /> ${t('Remove the route')}</button>`}
    </div>
  </${Section}>`;
}
const ROUTE_FIELDS = [
  { key: 'route_strength', label: 'How much they follow it', type: 'num', min: 0, max: 1, step: 0.01, def: 1,
    help: '0: as if there were no route (their launch, gravity, wind). 1: they go along the route as far as their speed and the forces would take them (gravity pushes them along it, not down; turbulence stays). Between: a mix' },
  { key: 'route_fixed', label: 'Where it is drawn', type: 'bool', anim: false,
    help: 'Off: each particle starts the route where it is born (it moves with the emitter). On: they all go along it where it is drawn' },
];

// Moving an object that follows a path moves the path (dx, dy: 0..1 of the frame)
export function movePath(o, dx, dy) {
  o.motion = { ...o.motion, path: { ...o.motion.path, points: o.motion.path.points.map((p) => [+(p[0] + dx).toFixed(5), +(p[1] + dy).toFixed(5), ...p.slice(2)]) } };
}
export { hasPath, hasRoute };

// The section of the inspector
export function MotionSection({ obj, kf, ctx, edit, what = 'clip' }) {
  const on = hasPath(obj);
  return html`<${Section} id="motion" title=${t('Motion path')} help=${what === 'text'
    ? 'The text can travel along a path drawn on the screen: draw it in the editor, then key its progress (it goes from start to end over the text\'s time).'
    : 'The clip can travel along a path drawn on the screen: draw it in the editor, then key its progress (it goes from start to end over the clip\'s time).'}>
    ${on && html`<${Form} fields=${what === 'text' ? TEXT_MOTION_FIELDS : MOTION_FIELDS} value=${obj.motion} ctx=${ctx} kf=${kf} prefix="motion"
      onChange=${(nm) => edit((o) => { o.motion = { ...nm, path: o.motion.path }; })} />`}
    <div class="row" style="margin-top:6px">
      <button class="btn sm" onClick=${() => ctx.openEditor('path')}><${Icon} name="route" size=${13} /> ${t(on ? 'Edit the path' : 'Draw a path')}</button>
      ${on && html`<span class="spacer"></span><button class="btn sm ghost danger" title=${t('It stays where it is placed (Position)')}
        onClick=${() => edit((o) => removePath(o))}><${Icon} name="trash-2" size=${13} /> ${t('Remove the path')}</button>`}
    </div>
  </${Section}>`;
}

const NODE_HELP = 'Values on this node (optional). Each one goes from node to node only between the nodes that define it; before the first it starts from 0 at the start of the path, after the last it stays.\n'
  + 'Depth: 0 far … 1 near. What goes along the path takes the depth of the point it is on (its start, middle and end, each its own).\n'
  + 'Named values are for the advanced settings (expressions) of any property: path.name.';

// The inspector's section of the path node selected in the editor: its depth and its named
// values (config.NodeVals), each with its easing towards the next node that defines it. what:
// 'motion' (the motion path) or 'route' (an emitter's route)
export function NodeSection({ obj, what, pi, edit }) {
  const sh = what === 'route' ? obj.emitter && obj.emitter.route : obj.motion && obj.motion.path;
  if (!sh || pi == null || pi >= (sh.points || []).length) return null;
  const all = nodeVals(sh), v = all[pi];
  const names = [...new Set(all.flatMap((x) => Object.keys(x.keys || {})))].sort();
  const set = (fn) => edit((o) => {
    const cur = what === 'route' ? o.emitter.route : o.motion.path;
    const vals = nodeVals(cur);
    fn(vals[pi]);
    const path = { ...cur };
    setVals(path, vals);
    if (what === 'route') o.emitter = { ...o.emitter, route: path };
    else o.motion = { ...o.motion, path };
  });
  const easeSel = (val, on) => html`<select value=${val || 'linear'} onChange=${(e) => on(e.target.value === 'linear' ? undefined : e.target.value)}
    title=${t('How it goes to the next node that defines it')}>
    ${EASES.map(([k, l]) => html`<option value=${k}>${t(l)}</option>`)}</select>`;
  const keys = v.keys || {};
  // while its depth is adjusted the preview shows the scene's depth, what is behind it in red
  const zAdjust = {
    onPointerDown: () => {
      adjusting.set({ kind: 'nodedepth' });
      const up = () => { removeEventListener('pointerup', up); if (!(document.activeElement && document.activeElement.type === 'number')) adjusting.set(null); };
      addEventListener('pointerup', up);
    },
    onFocusIn: (e) => { if (e.target.type === 'number') adjusting.set({ kind: 'nodedepth' }); },
    onFocusOut: (e) => { if (e.target.type === 'number') adjusting.set(null); },
  };
  return html`<div class="node-sec"><${Section} id="pathnode" defOpen=${true} title=${t('Path node {n}', { n: pi + 1 })}
    help=${NODE_HELP}>
    <div class="node-zrow" ...${v.z != null ? zAdjust : {}}>
      <label><input type="checkbox" checked=${v.z != null} onChange=${(e) => set((n) => { if (e.target.checked) n.z = 0.5; else { delete n.z; delete n.z_ease; } })} />
        ${t('Depth')}</label>
      ${v.z != null && html`
        <span class="node-z" style=${{ background: depthTint(v.z) }}></span>
        <${Edit} type="number" class="mono" style="width:64px" min="0" max="1" step="0.01" value=${v.z}
          onChange=${(e) => set((n) => { n.z = Math.max(0, Math.min(1, +e.target.value || 0)); })} />
        ${easeSel(v.z_ease, (ez) => set((n) => { if (ez) n.z_ease = ez; else delete n.z_ease; }))}
        <input type="range" min="0" max="1" step="0.01" value=${v.z} onInput=${(e) => set((n) => { n.z = +e.target.value; })} />`}
    </div>
    <hr class="node-sep" />
    <div class="node-keys">
      ${Object.keys(keys).sort().map((k) => html`<div class="row node-key" key=${k}>
        <code class="node-kname">${k}</code>
        <${Edit} type="number" class="mono" style="width:80px" step="any" value=${keys[k].v}
          onChange=${(e) => set((n) => { n.keys[k] = { ...n.keys[k], v: +e.target.value || 0 }; })} />
        ${easeSel(keys[k].ease, (ez) => set((n) => { n.keys[k] = { v: n.keys[k].v, ...(ez ? { ease: ez } : {}) }; }))}
        <button class="btn sm ghost danger" title=${t('Remove this value from the node')}
          onClick=${() => set((n) => { delete n.keys[k]; if (!Object.keys(n.keys).length) delete n.keys; })}><${Icon} name="x" size=${12} /></button>
      </div>`)}
      ${Object.keys(keys).length > 0 && html`<hr class="node-sep" />`}
      <div class="row">
        <${Edit} type="text" class="mono grow" list="node-key-names" placeholder=${t('name of a value (e.g. glow)')}
          onChange=${(e) => {
            const k = e.target.value.trim().replace(/[^A-Za-z0-9_]/g, '_');
            e.target.value = '';
            if (k && !/^[0-9]/.test(k)) set((n) => { n.keys = { ...(n.keys || {}), [k]: n.keys && n.keys[k] ? n.keys[k] : { v: 1 } }; });
          }} />
        <datalist id="node-key-names">${names.filter((k) => !keys[k]).map((k) => html`<option value=${k} />`)}</datalist>
      </div>
    </div>
  </${Section}></div>`;
}
