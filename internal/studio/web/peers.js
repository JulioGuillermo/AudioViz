// Presence: who else has the project open (people, agents through MCP), their cursors on the
// preview and the timeline, and what they have selected (a mark on that row of the timeline)

import { html } from './vendor/preact-htm.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';

const initials = (p) => {
  const n = (p.name || '').replace(/^🤖\s*/, '').trim();
  if (p.agent) return '🤖';
  if (!n) return '?';
  const w = n.split(/\s+/);
  return (w[0][0] + (w[1] ? w[1][0] : '')).toUpperCase();
};

// PeersBar: those connected (me last, click: my name) and showing their cursors or not
export function PeersBar({ peers, me, show, setShow, onRename }) {
  const others = (peers || []).filter((p) => p.id !== me.id);
  return html`<div class="peers">
    ${others.map((p) => html`<span key=${p.id} class=${'peer' + (p.agent ? ' agent' : '')} style=${{ '--c': p.color || '#888' }}
      title=${p.name || t('Someone')}>${initials(p)}</span>`)}
    <span class="peer me" style=${{ '--c': me.color }} title=${t('You: {n} (click to change your name)', { n: me.name || '—' })}
      onClick=${onRename}>${initials(me)}</span>
    ${others.length > 0 && html`<button class=${'btn icon ghost' + (show ? '' : ' off')} onClick=${() => setShow(!show)}
      title=${show ? t('Hide the others’ cursors') : t('Show the others’ cursors')}><${Icon} name=${show ? 'mouse-pointer-2' : 'eye-off'} size=${14} /></button>`}
  </div>`;
}

// peerCursors: the others' cursors in an area ('preview', 'timeline') with their peer
export function peerCursors(peers, cursors, area, me) {
  const out = [];
  for (const p of peers || []) {
    if (p.id === me.id) continue;
    const c = cursors[p.id] && cursors[p.id].cursor;
    if (c && c.area === area) out.push({ p, c });
  }
  return out;
}

// RemoteCursors: arrows with names, placed by place(cursor) → {x, y} (px in their box); scale:
// their size there (a zoomed view: 1 / its zoom)
export function RemoteCursors({ list, place, scale = 1, rot = 0 }) {
  return list.map(({ p, c }) => {
    const pos = place(c);
    if (!pos) return null;
    const tf = scale !== 1 || rot ? { transform: `scale(${scale}) rotate(${-rot}deg)`, transformOrigin: '0 0' } : {};
    return html`<div key=${p.id} class="rcursor" style=${{ left: pos.x + 'px', top: pos.y + 'px', '--c': p.color || '#888', ...tf }}>
      <svg width="14" height="18" viewBox="0 0 14 18"><path d="M1 1 L1 15 L5 11 L8 17 L10 16 L7 10 L13 10 Z" /></svg>
      <span>${p.name || t('Someone')}</span></div>`;
  });
}

// peerRowKey: the timeline row of what a peer has selected (its sel, or an agent's last path)
export function peerRowKey(sel, proj) {
  if (!sel || !proj) return null;
  if (sel.path) {
    const ts = sel.path.split('/');
    if (ts[1] === 'texts') return 'texts';
    if (ts[1] === 'tracks') return ts[2] || null;
    if (ts[1] === 'audio') return 'audio';
    if (ts[1] === 'layers') { const l = proj.layers[+ts[2]]; return l ? 'layer-' + l.id : null; }
    return null;
  }
  switch (sel.kind) {
    case 'track': return sel.track;
    case 'text': return 'texts';
    case 'layer': return 'layer-' + sel.id;
    case 'audio': return 'audio';
    case 'line': return 'lines';
  }
  return null;
}

// peerMarks: per timeline row, the others who have something on it selected
export function peerMarks(peers, cursors, proj, me) {
  const m = {};
  for (const p of peers || []) {
    if (p.id === me.id) continue;
    const sel = (cursors[p.id] && cursors[p.id].sel) || p.sel;
    const k = peerRowKey(sel, proj);
    if (k) (m[k] = m[k] || []).push(p);
  }
  return m;
}
