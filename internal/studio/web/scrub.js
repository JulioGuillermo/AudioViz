// Audio scrubbing: while the playback is stopped, every seek plays a short snippet of the
// song at that point. The song is decoded once (Web Audio) so snippets start instantly.

const RATE = 24000; // enough for scrubbing; halves the memory of a 48 kHz decode
const LEN = 0.12; // snippet length (s)
const FADE = 0.008; // fade in/out against clicks (s)
const MIN_GAP = 60; // ms between snippets while dragging (the last position always plays)

export function createScrubber() {
  let ctx = null, buf = null, url = '', loading = null;
  let cur = null, lastAt = 0, pending = null, timer = 0;

  const context = () => {
    if (!ctx) {
      try { ctx = new AudioContext({ sampleRate: RATE }); } catch (e) { ctx = new AudioContext(); }
    }
    if (ctx.state === 'suspended') ctx.resume();
    return ctx;
  };

  // Decodes the current source (once); concurrent calls share the same promise
  const load = () => {
    if (buf || !url) return Promise.resolve(buf);
    if (!loading) {
      const u = url;
      loading = fetch(u)
        .then((r) => r.arrayBuffer())
        .then((ab) => context().decodeAudioData(ab))
        .then((b) => { if (u === url) buf = b; return buf; })
        .catch(() => null)
        .finally(() => { loading = null; });
    }
    return loading;
  };

  const stop = () => {
    if (!cur) return;
    const { src, gain } = cur, now = ctx.currentTime;
    gain.gain.cancelScheduledValues(now);
    gain.gain.setValueAtTime(gain.gain.value, now);
    gain.gain.linearRampToValueAtTime(0, now + FADE);
    src.stop(now + FADE + 0.005);
    cur = null;
  };

  const fire = (t) => {
    lastAt = performance.now();
    if (!buf || t < 0 || t >= buf.duration) return;
    const c = context();
    stop();
    const src = c.createBufferSource(), gain = c.createGain(), now = c.currentTime;
    src.buffer = buf;
    gain.gain.setValueAtTime(0, now);
    gain.gain.linearRampToValueAtTime(1, now + FADE);
    gain.gain.setValueAtTime(1, now + LEN - FADE);
    gain.gain.linearRampToValueAtTime(0, now + LEN);
    src.connect(gain).connect(c.destination);
    src.start(now, t, LEN);
    const me = { src, gain };
    src.onended = () => { if (cur === me) cur = null; };
    cur = me;
  };

  return {
    // Song URL ('' = none); the decode happens lazily on first use
    setSource(u) {
      if (u === url) return;
      url = u; buf = null; stop();
    },
    // Starts decoding ahead of time (e.g. when the option is switched on)
    preload() { context(); return load(); },
    // Plays a snippet at time t (s), throttled while dragging
    play(t) {
      if (!url) return;
      if (!buf) { load(); return; }
      const wait = MIN_GAP - (performance.now() - lastAt);
      if (wait <= 0) { clearTimeout(timer); timer = 0; pending = null; fire(t); return; }
      pending = t;
      if (!timer) timer = setTimeout(() => { timer = 0; if (pending != null) { const p = pending; pending = null; fire(p); } }, wait);
    },
    stop() { clearTimeout(timer); timer = 0; pending = null; if (ctx) stop(); },
  };
}
