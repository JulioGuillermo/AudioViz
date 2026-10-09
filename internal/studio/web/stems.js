import { html, useEffect, useState } from './vendor/preact-htm.js';
import { Form, Section } from './fields.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { api } from './util.js';

// The song separated into its instruments (Demucs, a job: the model is downloaded the first time,
// the same one the lyrics use). Once separated, anything that reacts to the music can follow the
// drums, the kick, the snare, the hi-hats, the bass line, the vocals or the rest.

const FULL_FIELD = [{ key: 'stems_full', label: 'Keep the parts in full quality', type: 'bool', anim: false,
  help: 'Stereo 44.1 kHz WAV files, to listen to them or use them elsewhere (about 45 MB each for a 4-minute song). Off: mono, enough for the effects (a quarter of the size)' }];

export function StemsSection({ ctx, hasAudio, audio, onAudio }) {
  const [st, setSt] = useState(null);
  const job = ctx.job && ctx.job.kind === 'stems' ? ctx.job : null;
  const running = job && job.status === 'running';
  const doneKey = job && job.status === 'done' ? job.id : '';
  useEffect(() => {
    api('GET', '/api/stems').then(setSt).catch(() => setSt(null));
    if (doneKey && ctx.reloadAna) ctx.reloadAna(); // the timeline's curves of the instruments
  }, [doneKey, hasAudio]);
  const go = () => api('POST', '/api/stems', {}).catch(ctx.showErr);
  return html`<${Section} id="stems" title=${t('Instruments (the song separated)')} help=${'Separates the song into drums, bass, vocals and the rest: then any effect, emitter or text can react to just one of them (Reacts to → Drums, Kick, Snare, Hi-hats, Bass line, Vocals, The rest).'}>
    <${Form} fields=${FULL_FIELD} value=${audio || {}} onChange=${onAudio} ctx=${ctx} />
    <div class="row" style="flex-wrap:wrap;gap:6px">
      ${st && st.ready && !running ? html`<span class="faint" style="font-size:12px"><${Icon} name="check" size=${12} /> ${t(st.full ? 'Separated in full quality' : 'Separated (mono, for the effects)')}</span>
          <button class="btn sm" onClick=${() => api('POST', '/api/stems/open', {}).catch(ctx.showErr)} title=${st.dir}>
            <${Icon} name="folder-open" size=${13} /> ${t('Open the folder')}</button>
          <button class="btn sm" onClick=${go} title=${t('Separates it again (with the quality chosen above)')}>
            <${Icon} name="rotate-ccw" size=${13} /> ${t(audio && audio.stems_full && !st.full ? 'Separate again in full quality' : 'Separate again')}</button>`
        : html`<button class="btn sm primary" disabled=${running || !hasAudio} onClick=${go}>
          <${Icon} name=${running ? 'loader-circle' : 'audio-waveform'} size=${13} cls=${running ? 'spin' : ''} /> ${t('Separate the song')}</button>`}
      ${running && html`<span class="faint" style="font-size:12px">${t(job.message)} · ${Math.round((job.progress || 0) * 100)} %</span>`}
      ${job && job.status === 'error' && html`<span class="danger" style="font-size:12px">${job.message}</span>`}
    </div>
    ${st && !st.ready && !running && html`<p class="faint" style="margin:6px 0 0;font-size:11.5px">${t(st.model
      ? 'It takes a few minutes on the CPU, once per song.'
      : 'It takes a few minutes on the CPU, once per song (the model, 84 MB, is downloaded the first time).')} ${t('Generating the lyrics separates it too (and then uses its vocals).')}</p>`}
  </${Section}>`;
}
