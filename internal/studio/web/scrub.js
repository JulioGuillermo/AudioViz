// The song decoded once (Web Audio): the playback, exact to the sample (an <audio> element lands
// off the point asked when it seeks in a compressed song: a VBR MP3, a FLAC; it then plays ahead
// of the cursor) and, while it is stopped, a short snippet at every seek (scrubbing).

const LEN = 0.12; // snippet length (s)
const FADE = 0.008; // fade in/out against clicks (s)
const MIN_GAP = 60; // ms between snippets while dragging (the last position always plays)

export function createScrubber() {
  let ctx = null, buf = null, url = '', loading = null;
  let cur = null, lastAt = 0, pending = null, timer = 0;
  let playing = null; // { src, at: the context's time it started, t0: the song's time there }

  const context = () => {
    if (!ctx) {
      ctx = new AudioContext();
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

  const stopPlay = () => {
    if (!playing) return;
    try { playing.src.stop(); } catch (e) { /* ended */ }
    playing = null;
  };
  return {
    // The playback from time t (s): false while the song is not decoded yet (the caller plays it
    // another way)
    startPlay(t) {
      if (!buf) { load(); return false; }
      const c = context();
      stop(); stopPlay();
      const src = c.createBufferSource();
      src.buffer = buf;
      src.connect(c.destination);
      const at = c.currentTime;
      src.start(at, Math.max(0, Math.min(t, buf.duration)));
      playing = { src, at, t0: t };
      return true;
    },
    // Where the playback is (s), or null when it is not playing from here
    position() { return playing ? playing.t0 + (ctx.currentTime - playing.at) : null; },
    stopPlay,
    // Song URL ('' = none); the decode happens lazily on first use
    setSource(u) {
      if (u === url) return;
      url = u; buf = null; stop(); stopPlay();
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
