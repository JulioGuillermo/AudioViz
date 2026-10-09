// Reviewing what agents changed (internal/studio/review.go): the list of their changes, each to
// accept or to revert (only it: what came after stays), with what it changed in words; clicking
// one selects what it touched.

import { html, useEffect, useState } from './vendor/preact-htm.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { api } from './util.js';

// a place of the project in words (Text 1 · color)
export function placeName(path, proj, labelOf) {
  const ts = path.split('/').slice(1);
  const out = [];
  if (ts[0] === 'texts') {
    const x = proj && proj.texts[+ts[1]];
    out.push(t('Text') + ' ' + (+ts[1] + 1) + (x && (x.label || x.text) ? ' “' + (x.label || x.text).split('\n')[0].slice(0, 24) + '”' : ''));
    ts.splice(0, 2);
  } else if (ts[0] === 'layers') {
    const l = proj && proj.layers[+ts[1]];
    out.push((l && l.name) || t('Layer'));
    ts.splice(0, 2);
    if (ts[0] === 'clips') { out.push('#' + (+ts[1] + 1)); ts.splice(0, 2); }
  } else if (ts[0] === 'tracks') {
    out.push(labelOf(ts[1]) + (+ts[2] ? ' #' + (+ts[2] + 1) : ''));
    ts.splice(0, 3);
    if (ts[0] === 'cfg') ts.splice(0, 1);
  }
  if (ts.length) out.push(ts.join('.'));
  return out.join(' · ');
}

const short = (v) => {
  if (v === undefined || v === null) return '';
  const s = typeof v === 'object' ? JSON.stringify(v) : String(v);
  return s.length > 40 ? s.slice(0, 38) + '…' : s;
};

const opLine = (o, proj, labelOf) => {
  const where = placeName(o.path, proj, labelOf);
  if (o.op === 'remove') return t('removed') + ' ' + where;
  if (o.op === 'insert') return t('added') + ' ' + where;
  return where + ' → ' + short(o.value);
};

// pathSel: what to select for a path (as peers.js's peerRowKey, the other way)
export function pathSel(path, proj) {
  const ts = path.split('/');
  if (ts[1] === 'texts') return { kind: 'text', i: +ts[2] || 0 };
  if (ts[1] === 'tracks' && ts[2]) return { kind: 'track', track: ts[2], seg: +ts[3] || 0 };
  if (ts[1] === 'layers') { const l = proj && proj.layers[+ts[2]]; if (l) return { kind: 'layer', id: l.id, clip: ts[3] === 'clips' ? +ts[4] || 0 : null }; }
  if (ts[1] === 'audio') return { kind: 'audio' };
  if (ts[1] === 'output' || ts[1] === 'expressions') return { kind: 'output' };
  return null;
}

// AgentChanges: the button (with how many wait for review) and its panel
export function AgentChanges({ me, proj, rev, labelOf, onSelect, showErr }) {
  const [list, setList] = useState([]);
  const [open, setOpen] = useState(false);
  const [all, setAll] = useState(false); // also the reviewed ones
  const load = () => api('GET', '/api/project/changes').then((r) => setList(r.changes || [])).catch(() => {});
  useEffect(() => { load(); }, [rev]);
  const pending = list.filter((c) => !c.status);
  if (!pending.length && !open) return null;
  const act = async (c, action) => {
    if (action === 'revert' && c.conflict && c.conflict.length &&
      !confirm(t('{who} changed the same since: undo it anyway? (their change there goes back too)', { who: c.conflict.join(', ') }))) return;
    try { await api('POST', '/api/project/review', { client: me.id, v: c.v, action }); } catch (e) { showErr(e); }
    load();
  };
  const actAll = async (action) => {
    if (action === 'revert' && !confirm(t('Undo all the agents’ changes waiting for review?'))) return;
    for (const c of pending) { // newest first
      try { await api('POST', '/api/project/review', { client: me.id, v: c.v, action }); } catch (e) { showErr(e); break; }
    }
    load();
  };
  const shown = all ? list : pending;
  return html`<div class="agentchg">
    <button class=${'btn sm' + (pending.length ? ' warn' : ' ghost')} onClick=${() => setOpen(!open)} title=${t('What the agents changed: accept or undo it')}>
      🤖 ${pending.length ? t('{n} to review', { n: pending.length }) : t('Changes')}</button>
    ${open && html`<div class="agpanel">
      <div class="agh"><b>${t('Agents’ changes')}</b><span class="spacer"></span>
        <label class="faint"><input type="checkbox" checked=${all} onChange=${(e) => setAll(e.target.checked)} /> ${t('reviewed too')}</label>
        <button class="btn icon ghost" onClick=${() => setOpen(false)}><${Icon} name="x" size=${14} /></button></div>
      ${pending.length > 1 && html`<div class="agall">
        <button class="btn sm" onClick=${() => actAll('accept')}><${Icon} name="check" size=${13} /> ${t('Accept all')}</button>
        <button class="btn sm danger" onClick=${() => actAll('revert')}><${Icon} name="undo-2" size=${13} /> ${t('Undo all')}</button></div>`}
      <div class="aglist">
        ${!shown.length && html`<p class="faint">${t('Nothing to review.')}</p>`}
        ${shown.map((c) => html`<div key=${c.v} class=${'agitem' + (c.status ? ' done' : '')}>
          <div class="agmeta"><b>${c.name || '🤖'}</b><span class="faint">${new Date(c.t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })}</span>
            ${c.status && html`<span class=${'agst ' + c.status}>${t(c.status === 'accepted' ? 'accepted' : 'undone')}</span>`}</div>
          <ul>${c.ops.slice(0, 6).map((o) => html`<li onClick=${() => { const s = pathSel(o.path, proj); if (s) onSelect(s); }}>${opLine(o, proj, labelOf)}</li>`)}
            ${c.ops.length > 6 && html`<li class="faint">${t('and {n} more', { n: c.ops.length - 6 })}</li>`}</ul>
          ${!c.status && c.conflict && c.conflict.length > 0 && html`<div class="agwarn"><${Icon} name="triangle-alert" size=${12} /> ${t('{who} changed the same since', { who: c.conflict.join(', ') })}</div>`}
          ${!c.status && html`<div class="agbtns">
            <button class="btn sm" onClick=${() => act(c, 'accept')}><${Icon} name="check" size=${13} /> ${t('Accept')}</button>
            <button class="btn sm ghost" onClick=${() => act(c, 'revert')}><${Icon} name="undo-2" size=${13} /> ${t('Undo')}</button></div>`}
        </div>`)}
      </div>
    </div>`}
  </div>`;
}
