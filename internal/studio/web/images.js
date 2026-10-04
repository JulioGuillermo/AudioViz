import { html, useEffect, useState } from './vendor/preact-htm.js';
import { api, basename } from './util.js';
import { Icon } from './icons.js';
import { Select } from './fields.js';
import { t } from './i18n.js';
import { Modal, GenSteps, fmtDur, fmtSize, fmtDate, thumbURL } from './dialogs.js';

// Image generation: a prompt (and optionally reference images to edit or combine) becomes an
// image in the project's images/ folder, made inside the binary on the GPU. The images can then be
// used as the background, or as references for new ones.

const ASPECTS = [['video', 'Like the video'], ['16:9', 'Horizontal 16:9'], ['1:1', 'Square'], ['9:16', 'Vertical 9:16'], ['4:3', 'Classic 4:3']];
const QUALITY = [['fast', 'Fast (≈0.6 MP)'], ['detail', 'Detailed (≈1 MP)'], ['full', 'Video size (slow)']];
const round32 = (v) => Math.max(256, Math.min(2048, Math.round(v / 32) * 32));
const media = (p, dl) => '/api/media?path=' + encodeURIComponent(p) + (dl ? '&download=1' : '');

function imageSize(aspect, quality, vw, vh) {
  const ar = aspect === 'video' ? vw / vh : (([a, b]) => a / b)(aspect.split(':').map(Number));
  if (quality === 'full') {
    const long = Math.max(vw, vh);
    return ar >= 1 ? [round32(long), round32(long / ar)] : [round32(long * ar), round32(long)];
  }
  const px = quality === 'fast' ? 0.6e6 : 1.0e6;
  const w = Math.sqrt(px * ar);
  return [round32(w), round32(w / ar)];
}

// Advanced settings: empty = the model's default
const ADV0 = { fa: '', cfg: '', sampler: '', scheduler: '', negative: '', init: '', strength: 0.75 };
const loadAdv = () => { try { return { ...ADV0, ...JSON.parse(localStorage.getItem('audioviz.img.adv') || '{}'), init: '' }; } catch (e) { return { ...ADV0 }; } };
const advChanged = (a) => Object.keys(ADV0).filter((k) => k !== 'strength' && a[k] !== ADV0[k]).length;

const remember = (k, v) => { try { localStorage.setItem('audioviz.img.' + k, v); } catch (e) { /* ignore */ } };
const recall = (k, d) => { try { return localStorage.getItem('audioviz.img.' + k) || d; } catch (e) { return d; } };

export function ImagesDialog({ meta, job, onClose, onCancel, onStart, onUseBackground, pickFile }) {
  const [data, setData] = useState(null);
  const [err, setErr] = useState('');
  const [model, setModel] = useState(() => recall('model', 'zimage'));
  const [prompt, setPrompt] = useState(() => recall('prompt', ''));
  const [refs, setRefs] = useState([]);
  const [aspect, setAspect] = useState(() => recall('aspect', 'video'));
  const [quality, setQuality] = useState(() => recall('quality', 'fast'));
  const [steps, setSteps] = useState('');
  const [seed, setSeed] = useState('');
  const [cur, setCur] = useState(null); // image shown big
  const [picking, setPicking] = useState(false); // choosing images from the project: 'refs' | 'init' | false
  const [adv, setAdvState] = useState(loadAdv);
  const setAdv = (k, v) => setAdvState((a) => { const n = { ...a, [k]: v }; remember('adv', JSON.stringify({ ...n, init: '' })); return n; });
  const mine = job && job.kind === 'image' ? job : null;
  const running = mine && mine.status === 'running';

  const load = () => api('GET', '/api/images').then((d) => { setData(d); return d; }).catch((e) => setErr(e.message));
  useEffect(() => { load(); }, []);
  // When a generation finishes: reload and show the new image
  useEffect(() => {
    if (mine && mine.status === 'done' && mine.output) load().then((d) => { const im = d && d.images.find((i) => i.path === mine.output); if (im) setCur(im); });
  }, [mine && mine.status, mine && mine.output]);
  useEffect(() => { if (running) setCur(null); }, [running]);

  const fam = data && data.models.find((m) => m.id === model);
  const maxRefs = fam ? fam.refs : 0;
  const [w, h] = imageSize(aspect, quality, meta.width || 1920, meta.height || 1080);
  const set = (k, setter) => (v) => { setter(v); remember(k, v); };
  const pickImage = (p) => { if (picking === 'init') { setAdv('init', p); setPicking(false); } else addRef(p); };
  const addRef = (p) => setRefs((r) => (r.includes(p) || r.length >= Math.max(maxRefs, 1) ? r : [...r, p]));
  const start = () => {
    setErr('');
    remember('prompt', prompt);
    onStart({ model, prompt, refs: refs.slice(0, maxRefs), width: w, height: h, steps: +steps || 0, seed: seed === '' ? -1 : +seed,
      flashAttn: adv.fa, cfg: +adv.cfg || 0, sampler: adv.sampler, scheduler: adv.scheduler, negative: adv.negative.trim(),
      init: adv.init, strength: adv.init ? +adv.strength : 0 });
  };
  const reuse = (im) => {
    if (im.model) set('model', setModel)(im.model);
    setPrompt(im.prompt || '');
    setSeed(String(im.seed ?? ''));
    setRefs(im.refs || []);
    setAdvState((a) => ({ ...a, fa: im.flashAttn || '', cfg: im.cfg ? String(im.cfg) : '', sampler: im.sampler || '', scheduler: im.scheduler || '',
      negative: im.negative || '', init: im.init || '', strength: im.strength || 0.75 }));
    setCur(null);
  };
  const del = async (im) => {
    if (!confirm(t('Delete {f}? This cannot be undone.', { f: im.name }))) return;
    try { await api('DELETE', '/api/images?name=' + encodeURIComponent(im.name)); setCur(null); load(); } catch (e) { setErr(e.message); }
  };
  const upload = async (files) => {
    for (const f of files) {
      const fd = new FormData();
      fd.append('file', f);
      try {
        const r = await fetch('/api/upload', { method: 'POST', body: fd });
        const j = await r.json();
        if (!r.ok) throw new Error(j.error || r.statusText);
        pickImage(j.path);
      } catch (e) { setErr(e.message); }
    }
    load();
  };

  const modelOpts = data ? data.models.map((m) => [m.id, m.name + (m.missing > 0 ? ' · ' + t('{s} to download', { s: fmtSize(m.missing) }) : '')]) : [[model, model]];
  const toDownload = fam ? (refs.length && maxRefs ? fam.missingRefs : fam.missing) : 0;

  // ---- left: the request
  const form = html`<div class="ig-form">
    <div class="fld"><label>${t('Model')}</label><div class="ctl"><${Select} value=${model} options=${modelOpts} translate=${false} onChange=${set('model', setModel)} /></div></div>
    ${fam && html`<p class="faint ig-desc">${t(fam.desc)}${fam.heavy ? html` <span class="st-warn">${t('Heavy model.')}</span>` : ''}</p>`}
    <label class="ig-l">${t('Prompt')}</label>
    <textarea class="ig-prompt" rows="6" value=${prompt} placeholder=${t('What the image shows: subject, setting, light, style… (English works best)')}
      onInput=${(e) => setPrompt(e.target.value)} onKeyDown=${(e) => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey) && prompt.trim() && !running) start(); }}></textarea>
    ${maxRefs > 0 && html`<label class="ig-l">${t('Reference images')} <span class="faint">(${refs.length}/${maxRefs})</span></label>
      <div class="ig-refs">
        ${refs.map((p) => html`<div class="ig-ref" key=${p} title=${p}><img src=${thumbURL(p)} alt="" />
          <button class="btn icon sm" title=${t('Remove')} onClick=${() => setRefs(refs.filter((x) => x !== p))}><${Icon} name="x" size=${12} /></button></div>`)}
        ${refs.length < maxRefs && html`<button class="ig-add" title=${t('Add a reference image')} onClick=${() => setPicking(picking === 'refs' ? false : 'refs')}><${Icon} name="plus" size=${18} /></button>`}
      </div>
      <p class="faint ig-hint">${t('Optional. With references, describe the change (“make it night”, “the person from image 1 in the place of image 2”).')}</p>`}
    ${maxRefs === 0 && refs.length > 0 && html`<p class="faint ig-hint st-warn">${t('This model does not use reference images; choose FLUX.2 klein or Qwen Image to edit.')}</p>`}
    <div class="fld"><label>${t('Shape')}</label><div class="ctl"><${Select} value=${aspect} options=${ASPECTS} onChange=${set('aspect', setAspect)} /></div></div>
    <div class="fld"><label>${t('Size')}</label><div class="ctl"><${Select} value=${quality} options=${QUALITY} onChange=${set('quality', setQuality)} />
      <span class="faint mono" style="font-size:11px;white-space:nowrap">${w}×${h}</span></div></div>
    <div class="fld"><label>${t('Steps')}</label><div class="ctl"><input type="number" min="1" max="60" value=${steps} placeholder=${fam ? String(fam.steps) : ''}
      onInput=${(e) => setSteps(e.target.value)} title=${t('Empty = the model default. More steps: more detail, slower.')} /></div></div>
    <div class="fld"><label>${t('Seed')}</label><div class="ctl"><input type="number" min="0" value=${seed} placeholder=${t('Random')}
      onInput=${(e) => setSeed(e.target.value)} title=${t('The same seed and prompt give the same image: change only the prompt to make variations.')} />
      ${seed !== '' && html`<button class="btn icon sm" title=${t('Random')} onClick=${() => setSeed('')}><${Icon} name="shuffle" size=${13} /></button>`}</div></div>
    <details class="ig-adv" open=${recall('advOpen', '') === '1'} onToggle=${(e) => remember('advOpen', e.target.open ? '1' : '')}>
      <summary>${t('Advanced settings')}${advChanged(adv) ? html` <span class="badge">${t('{n} changed', { n: advChanged(adv) })}</span>` : ''}</summary>
      <div class="fld"><label title=${t('Fused attention: faster on most GPUs, slower on Intel ones')}>${t('Flash attention')}</label><div class="ctl">
        <${Select} value=${adv.fa} onChange=${(v) => setAdv('fa', v)} translate=${false}
          options=${[['', t('Auto ({v})', { v: t(data && data.flashAttn ? 'on' : 'off') })], ['on', t('On')], ['off', t('Off')]]} /></div></div>
      <div class="fld"><label title=${t('How strictly the image follows the prompt. The fast models (Turbo, klein) are made for 1.')}>CFG</label><div class="ctl">
        <input type="number" min="1" max="20" step="0.5" value=${adv.cfg} placeholder=${fam ? String(fam.cfg) : ''} onInput=${(e) => setAdv('cfg', e.target.value)} /></div></div>
      <label class="ig-l">${t('Negative prompt')} <span class="faint">${(+adv.cfg || (fam && fam.cfg) || 1) <= 1 ? t('(only with CFG above 1)') : ''}</span></label>
      <textarea class="ig-neg" rows="2" value=${adv.negative} placeholder=${t('What to avoid: blurry, text, watermark…')} onInput=${(e) => setAdv('negative', e.target.value)}></textarea>
      <div class="fld"><label>${t('Sampler')}</label><div class="ctl"><${Select} value=${adv.sampler} onChange=${(v) => setAdv('sampler', v)} translate=${false}
        options=${[['', t('Model default')], ...((data && data.samplers) || []).map((x) => [x, x])]} /></div></div>
      <div class="fld"><label>${t('Scheduler')}</label><div class="ctl"><${Select} value=${adv.scheduler} onChange=${(v) => setAdv('scheduler', v)} translate=${false}
        options=${[['', t('Model default')], ...((data && data.schedulers) || []).map((x) => [x, x])]} /></div></div>
      <label class="ig-l" title=${t('Start from an image instead of noise: the result keeps its composition (img2img)')}>${t('Initial image')}</label>
      <div class="ig-refs">
        ${adv.init ? html`<div class="ig-ref" title=${adv.init}><img src=${thumbURL(adv.init)} alt="" />
          <button class="btn icon sm" title=${t('Remove')} onClick=${() => setAdv('init', '')}><${Icon} name="x" size=${12} /></button></div>`
          : html`<button class="ig-add" title=${t('Choose the initial image')} onClick=${() => setPicking(picking === 'init' ? false : 'init')}><${Icon} name="plus" size=${18} /></button>`}
      </div>
      ${adv.init && html`<div class="fld"><label title=${t('How much the initial image changes: low keeps it, 1 ignores it')}>${t('Denoise')}</label><div class="ctl">
        <input type="range" min="0.05" max="1" step="0.05" value=${adv.strength} onInput=${(e) => setAdv('strength', +e.target.value)} />
        <span class="mono faint" style="font-size:11px;width:32px;text-align:right">${(+adv.strength).toFixed(2)}</span></div></div>`}
      ${advChanged(adv) > 0 && html`<button class="btn sm ghost" style="margin-top:6px" onClick=${() => { setAdvState({ ...ADV0 }); remember('adv', '{}'); }}>
        <${Icon} name="rotate-ccw" size=${13} /> ${t('Reset to the defaults')}</button>`}
    </details>
    ${toDownload > 0 && html`<p class="faint ig-hint"><${Icon} name="download" size=${12} /> ${t('The first time, {s} of models are downloaded (once).', { s: fmtSize(toDownload) })}</p>`}
  </div>`;

  // ---- right: progress / the chosen image / the reference picker / the gallery
  let side;
  if (picking) {
    const all = [...(data ? data.images.map((i) => i.path) : []), ...(data ? data.assets : [])];
    side = html`<div class="ig-side">
      <div class="row" style="margin-bottom:8px"><b>${t(picking === 'init' ? 'Choose the initial image' : 'Choose reference images')}</b><span class="spacer"></span>
        <label class="btn sm"><${Icon} name="upload" size=${13} /> ${t('Upload…')}<input type="file" accept="image/*" multiple=${picking !== 'init'} hidden onChange=${(e) => upload([...e.target.files])} /></label>
        <button class="btn sm" onClick=${() => pickFile('image', (p) => pickImage(p))}><${Icon} name="folder-open" size=${13} /> ${t('Browse…')}</button>
        <button class="btn sm primary" onClick=${() => setPicking(false)}>${t('Done')}</button></div>
      ${all.length === 0 ? html`<p class="faint">${t('No images in the project yet: upload one or browse your files.')}</p>` : html`<div class="tiles ig-tiles">
        ${all.map((p) => html`<div class=${'tile' + ((picking === 'init' ? adv.init === p : refs.includes(p)) ? ' cur' : '')} key=${p} title=${p}
          onClick=${() => (picking !== 'init' && refs.includes(p) ? setRefs(refs.filter((x) => x !== p)) : pickImage(p))}>
          <div class="th"><img loading="lazy" alt="" src=${thumbURL(p)} /></div><div class="nm">${basename(p)}</div></div>`)}
      </div>`}
    </div>`;
  } else if (mine && (running || mine.status === 'error' || mine.status === 'canceled')) {
    const pv = mine.gen && mine.gen.preview;
    side = html`<div class="ig-side">
      <div class="ig-stage">${pv ? html`<img class="pv" src=${pv} alt="" style=${{ aspectRatio: (mine.gen && mine.gen.steps && w / h) || 'auto' }} />` : html`<div class="faint ig-wait"><${Icon} name=${running ? 'loader-circle' : 'image'} size=${28} cls=${running ? 'spin' : ''} />
        ${running ? t('The image appears here as it forms…') : ''}</div>`}</div>
      <${GenSteps} gen=${mine.gen} status=${mine.status} />
      ${mine.status !== 'running' && html`<p class=${'row ' + (mine.status === 'error' ? 'st-err' : 'st-warn')} style="font-size:12.5px">
        <${Icon} name=${mine.status === 'error' ? 'triangle-alert' : 'x'} size=${15} />${mine.message}</p>`}
    </div>`;
  } else if (cur) {
    side = html`<div class="ig-side">
      <div class="ig-stage"><img src=${media(cur.path)} alt="" /></div>
      <div class="ig-meta">
        <p class="ig-ptext">${cur.prompt || html`<span class="faint">${cur.name}</span>`}</p>
        <div class="faint mono" style="font-size:11px">${[cur.model, cur.width && cur.width + '×' + cur.height, cur.steps && t('{n} steps', { n: cur.steps }),
          cur.seed != null && t('seed {n}', { n: cur.seed }), cur.cfg && 'CFG ' + cur.cfg, cur.sampler, cur.scheduler, cur.flashAttn && 'FA ' + cur.flashAttn,
          cur.init && t('from {f} ({s})', { f: basename(cur.init), s: cur.strength }), cur.elapsed && fmtDur(cur.elapsed), fmtDate(cur.created)].filter(Boolean).join(' · ')}</div>
        ${cur.negative && html`<div class="faint" style="font-size:11.5px;margin-top:2px">${t('Negative prompt')}: ${cur.negative}</div>`}
        <div class="row" style="margin-top:10px;flex-wrap:wrap;gap:6px">
          <button class="btn primary sm" onClick=${() => onUseBackground(cur.path)}><${Icon} name="image" size=${13} /> ${t('Use as background')}</button>
          <button class="btn sm" onClick=${() => { if (!maxRefs) set('model', setModel)('klein'); setRefs([cur.path]); setCur(null); }}
            title=${t('Edit this image: it becomes the reference of a new one')}><${Icon} name="wand-sparkles" size=${13} /> ${t('Edit / vary')}</button>
          <button class="btn sm" onClick=${() => reuse(cur)} title=${t('Load its prompt, model and seed')}><${Icon} name="rotate-ccw" size=${13} /> ${t('Reuse settings')}</button>
          <a class="btn sm" href=${media(cur.path, true)} download=${cur.name}><${Icon} name="download" size=${13} /> ${t('Download')}</a>
          <span class="spacer"></span>
          <button class="btn sm ghost danger" onClick=${() => del(cur)}><${Icon} name="trash-2" size=${13} /> ${t('Delete')}</button>
        </div>
      </div>
    </div>`;
  } else {
    side = html`<div class="ig-side">
      ${!data ? html`<p class="faint">${t('Loading…')}</p>` : data.images.length === 0
        ? html`<div class="ig-empty faint"><${Icon} name="image" size=${32} /><p>${t('Write a prompt and press Generate. The images are saved in the project, in:')}</p>
            <div class="mono" style="font-size:11px">${data.dir}</div></div>`
        : html`<div class="tiles ig-tiles">
          ${data.images.map((im) => html`<div class="tile" key=${im.path} title=${im.prompt || im.name} onClick=${() => setCur(im)}>
            <div class="th"><img loading="lazy" alt="" src=${thumbURL(im.path)} /></div>
            <div class="nm">${im.prompt || im.name}</div><div class="sz">${im.model || ''} · ${fmtDate(im.created)}</div></div>`)}
        </div>`}
    </div>`;
  }

  return html`<${Modal} title=${t('Images')} icon="image" onClose=${onClose} width="min(1100px,96vw)"
    footer=${html`${cur && !running && html`<button class="btn ghost" onClick=${() => setCur(null)}><${Icon} name="chevron-left" size=${15} /> ${t('All images')}</button>`}
      <span class="spacer"></span>
      <button class="btn" onClick=${onClose}>${t(running ? 'Keep running in the background' : 'Close')}</button>
      ${running ? html`<button class="btn danger" onClick=${onCancel}><${Icon} name="x" size=${15} /> ${t('Cancel')}</button>`
        : html`<button class="btn primary" disabled=${!prompt.trim() || !data || (job && job.status === 'running')} onClick=${start} title="Ctrl+Enter">
            <${Icon} name="wand-sparkles" size=${15} /> ${t('Generate')}</button>`}`}>
    ${err && html`<div class="alert err" style="margin-bottom:10px"><${Icon} name="triangle-alert" size=${15} />${err}</div>`}
    <div class="ig">${form}${side}</div>
  </${Modal}>`;
}
