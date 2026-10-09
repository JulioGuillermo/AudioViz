// The project's chat (internal/studio/chat.go): messages in threads, with files, images and
// frames of the song, by the people and agents working on it. Everything stays in the project.
// Links inside messages: av://t/<seconds> (go there), av://text/<i>, av://track/<id>/<seg>,
// av://layer/<id>/<clip> (select it); @name mentions someone (an agent listening wakes up).

import { html, useEffect, useRef, useState } from './vendor/preact-htm.js';
import { createStore, useStore } from './store.js';
import { Icon } from './icons.js';
import { t } from './i18n.js';
import { api, fmtTime } from './util.js';

// the messages and who is writing (fed by the app's event stream)
export const chatStore = createStore({ msgs: [], typing: {} });

export async function loadChat() {
  try {
    const r = await api('GET', '/api/chat');
    chatStore.set({ ...chatStore.get(), msgs: r.messages || [] });
  } catch (e) { chatStore.set({ msgs: [], typing: {} }); }
}

// an event of the stream: chat {kind, msg} or typing {id, name, thread}
export function chatEvent(type, data, me, notify) {
  const st = chatStore.get();
  if (type === 'typing') {
    if (data.id === me.id) return;
    chatStore.set({ ...st, typing: { ...st.typing, [data.id]: { name: data.name, thread: data.thread, at: Date.now() } } });
    setTimeout(() => {
      const s2 = chatStore.get();
      const tp = { ...s2.typing };
      if (tp[data.id] && Date.now() - tp[data.id].at >= 3900) { delete tp[data.id]; chatStore.set({ ...s2, typing: tp }); }
    }, 4000);
    return;
  }
  const m = data.msg;
  let msgs = st.msgs;
  if (data.kind === 'post') {
    if (!msgs.some((x) => x.id === m.id)) msgs = [...msgs, m];
    if (m.author.id !== me.id && notify) notify(m);
  } else msgs = msgs.map((x) => (x.id === m.id ? { ...x, ...m, author: x.author, at: x.at, thread: x.thread } : x));
  const typing = { ...st.typing };
  delete typing[m.author.id];
  chatStore.set({ msgs, typing });
}

const lastReadKey = (path) => 'audioviz.chatRead:' + (path || '');
export function unreadCount(msgs, me, path) {
  let at = 0;
  try { at = +localStorage.getItem(lastReadKey(path)) || 0; } catch (e) { /* none */ }
  return msgs.filter((m) => !m.gone && m.author.id !== me.id && Date.parse(m.at) > at).length;
}
function markRead(path) { try { localStorage.setItem(lastReadKey(path), String(Date.now())); } catch (e) { /* none */ } }

// browser notifications (optional, per browser)
export const notifyOn = () => { try { return localStorage.getItem('audioviz.notify') === '1' && 'Notification' in window && Notification.permission === 'granted'; } catch (e) { return false; } };
export function notifyMsg(m) {
  if (!notifyOn() || !document.hidden) return;
  try { new Notification(m.author.name || t('Someone'), { body: m.text || (m.files && m.files.length ? '📎 ' + m.files[0].title : ''), tag: 'av-chat' }); } catch (e) { /* none */ }
}

// ---- a message's text: escaped, then **bold**, `code`, [links](…), @mentions, line breaks
const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
function renderText(text) {
  let h = esc(text || '');
  h = h.replace(/`([^`\n]+)`/g, '<code>$1</code>');
  h = h.replace(/\*\*([^*\n]+)\*\*/g, '<b>$1</b>');
  h = h.replace(/\[([^\]\n]+)\]\(((?:av|https?):\/\/[^\s)]+)\)/g, (m, txt, url) => url.startsWith('av://')
    ? `<a class="avlink" data-av="${url}">${txt}</a>` : `<a href="${url}" target="_blank" rel="noopener">${txt}</a>`);
  h = h.replace(/(^|[\s(])@([\p{L}\p{N}_.-]+)/gu, '$1<span class="mention">@$2</span>');
  return h.replace(/\n/g, '<br>');
}

const initials = (p) => (p.agent ? '🤖' : ((p.name || '?').trim().split(/\s+/).map((w) => w[0]).join('').slice(0, 2).toUpperCase() || '?'));
const fileURL = (f, dl) => '/api/chat/file?name=' + encodeURIComponent(f.name) + (dl ? '&download=1' : '');
const clock = (iso) => { const d = new Date(iso); return d.toLocaleString(undefined, { hour: '2-digit', minute: '2-digit', day: 'numeric', month: 'short' }); };

function Files({ files, onSeek }) {
  return html`<div class="cfiles">${files.map((f) => (f.type || '').startsWith('image/')
    ? html`<div class=${'cimg' + (f.frame ? ' frame' : '')}>
        <a href=${fileURL(f)} target="_blank" rel="noopener"><img src=${fileURL(f)} loading="lazy" /></a>
        ${f.frame && html`<button class="btn sm" onClick=${() => onSeek(f.time)} title=${t('Go to that moment')}><${Icon} name="play" size=${12} /> ${fmtTime(f.time)}</button>`}
      </div>`
    : html`<a class="cfile" href=${fileURL(f, true)}><${Icon} name="paperclip" size=${13} /> ${f.title || f.name}<span class="faint"> · ${Math.ceil(f.size / 1024)} KB</span></a>`)}</div>`;
}

function Msg({ m, me, answers, onThread, onEdit, onSeek, inThread }) {
  if (m.gone) return html`<div class="cmsg gone"><span class="faint">${t('Message removed')}</span>
    ${!inThread && answers > 0 && html`<button class="btn sm ghost cthr" onClick=${() => onThread(m.id)}>${(answers === 1 ? t('1 answer') : t('{n} answers', { n: answers }))}</button>`}</div>`;
  const a = m.author;
  return html`<div class=${'cmsg' + (a.id === me.id ? ' mine' : '')}>
    <span class="cav" style=${{ '--c': a.color || '#888' }}>${initials(a)}</span>
    <div class="cbody">
      <div class="chead"><b>${a.name || t('Someone')}</b><span class="faint">${clock(m.at)}${m.edited ? ' · ' + t('edited') : ''}</span>
        <span class="cact">
          ${!inThread && html`<button class="btn icon ghost" title=${t('Answer in a thread')} onClick=${() => onThread(m.id)}><${Icon} name="reply" size=${13} /></button>`}
          ${a.id === me.id && html`<button class="btn icon ghost" title=${t('Edit')} onClick=${() => onEdit(m, false)}><${Icon} name="pencil" size=${13} /></button>
            <button class="btn icon ghost" title=${t('Remove')} onClick=${() => onEdit(m, true)}><${Icon} name="trash-2" size=${13} /></button>`}
        </span></div>
      ${m.text && html`<div class="ctext" dangerouslySetInnerHTML=${{ __html: renderText(m.text) }}></div>`}
      ${m.files && m.files.length > 0 && html`<${Files} files=${m.files} onSeek=${onSeek} />`}
      ${!inThread && answers > 0 && html`<button class="btn sm ghost cthr" onClick=${() => onThread(m.id)}><${Icon} name="message-square" size=${12} /> ${(answers === 1 ? t('1 answer') : t('{n} answers', { n: answers }))}</button>`}
    </div>
  </div>`;
}

// the composer: text (Enter sends, Shift+Enter a new line), @ completion, files (button, paste,
// drop), the current frame, a link to what is selected and the time
function Composer({ me, thread, names, linkNow, timeStore, onSent }) {
  const [text, setText] = useState('');
  const [files, setFiles] = useState([]);
  const [busy, setBusy] = useState(false);
  const [at, setAt] = useState(null); // @ completion: {start, word}
  const ta = useRef();
  const fileIn = useRef();
  const lastTyping = useRef(0);
  const upload = async (list) => {
    for (const f of list) {
      const fd = new FormData();
      fd.append('file', f);
      try {
        const r = await fetch('/api/chat/upload', { method: 'POST', body: fd });
        const d = await r.json();
        if (!r.ok) throw new Error(d.error);
        setFiles((x) => [...x, { ...d, type: f.type || d.type }]);
      } catch (e) { alert(e.message || e); }
    }
  };
  const frame = async () => {
    setBusy(true);
    try { const f = await api('POST', '/api/chat/frame', { t: timeStore.get(), scale: 2 }); setFiles((x) => [...x, f]); } catch (e) { alert(e.message || e); }
    setBusy(false);
  };
  const insert = (s) => {
    const el = ta.current;
    const p = el ? el.selectionStart : text.length;
    setText(text.slice(0, p) + s + text.slice(p));
    setTimeout(() => { if (el) { el.focus(); el.selectionStart = el.selectionEnd = p + s.length; } }, 0);
  };
  const send = async () => {
    if (busy || (!text.trim() && !files.length)) return;
    setBusy(true);
    try {
      await api('POST', '/api/chat', { client: me.id, name: me.name, color: me.color, thread: thread || '', text, files });
      setText(''); setFiles([]); setAt(null);
      onSent && onSent();
    } catch (e) { alert(e.message || e); }
    setBusy(false);
  };
  const onInput = (e) => {
    const v = e.target.value;
    setText(v);
    const p = e.target.selectionStart;
    const m = v.slice(0, p).match(/(^|\s)@([\p{L}\p{N}_.-]*)$/u);
    setAt(m ? { start: p - m[2].length, word: m[2].toLowerCase() } : null);
    if (Date.now() - lastTyping.current > 2500) {
      lastTyping.current = Date.now();
      fetch('/api/chat/typing', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ client: me.id, thread: thread || '' }) }).catch(() => {});
    }
  };
  const options = at ? names.filter((n) => n.toLowerCase().startsWith(at.word)).slice(0, 6) : [];
  const pick = (n) => {
    const v = text.slice(0, at.start) + n.replace(/\s+/g, '_') + ' ' + text.slice(ta.current.selectionStart);
    setText(v); setAt(null);
    setTimeout(() => ta.current && ta.current.focus(), 0);
  };
  return html`<div class="ccomp" onDragOver=${(e) => e.preventDefault()} onDrop=${(e) => { e.preventDefault(); upload([...e.dataTransfer.files]); }}>
    ${files.length > 0 && html`<div class="cpend">${files.map((f, i) => html`<span class="chip">
      ${(f.type || '').startsWith('image/') ? html`<img src=${fileURL(f)} />` : html`<${Icon} name="paperclip" size=${12} />`}
      ${f.frame ? fmtTime(f.time) : f.title}<button class="btn icon ghost" onClick=${() => setFiles(files.filter((_, j) => j !== i))}><${Icon} name="x" size=${11} /></button></span>`)}</div>`}
    ${options.length > 0 && html`<div class="cat">${options.map((n) => html`<div onMouseDown=${(e) => { e.preventDefault(); pick(n); }}>@${n}</div>`)}</div>`}
    <textarea ref=${ta} rows="2" value=${text} placeholder=${thread ? t('Answer in the thread…') : t('Write a message… (@ to mention)')}
      onInput=${onInput} onPaste=${(e) => { const fs = [...(e.clipboardData && e.clipboardData.files) || []]; if (fs.length) { e.preventDefault(); upload(fs); } }}
      onKeyDown=${(e) => {
        if (e.key === 'Enter' && !e.shiftKey && !(options.length && at)) { e.preventDefault(); send(); }
        if (e.key === 'Tab' && options.length) { e.preventDefault(); pick(options[0]); }
        if (e.key === 'Escape') setAt(null);
        e.stopPropagation();
      }}></textarea>
    <div class="cbar">
      <button class="btn icon ghost" title=${t('Attach files (or paste / drop them)')} onClick=${() => fileIn.current.click()}><${Icon} name="paperclip" size=${15} /></button>
      <button class="btn icon ghost" title=${t('Attach the current frame (with its time)')} disabled=${busy} onClick=${frame}><${Icon} name="camera" size=${15} /></button>
      <button class="btn icon ghost" title=${t('Link to what is selected, at this moment')} onClick=${() => insert(linkNow())}><${Icon} name="link" size=${15} /></button>
      <span class="spacer"></span>
      <button class="btn primary sm" disabled=${busy || (!text.trim() && !files.length)} onClick=${send}><${Icon} name="send" size=${13} /> ${t('Send')}</button>
      <input ref=${fileIn} type="file" multiple style="display:none" onChange=${(e) => { upload([...e.target.files]); e.target.value = ''; }} />
    </div>
  </div>`;
}

// ChatPanel: the conversation (or a thread of it)
export function ChatPanel({ open, onClose, me, peers, path, linkNow, timeStore, onSeek, onAvLink }) {
  const { msgs, typing } = useStore(chatStore);
  const [thread, setThread] = useState(null);
  const [notify, setNotify] = useState(notifyOn());
  const list = useRef();
  const visible = msgs.filter((m) => (thread ? m.id === thread || m.thread === thread : !m.thread));
  const answers = {};
  for (const m of msgs) if (m.thread && !m.gone) answers[m.thread] = (answers[m.thread] || 0) + 1;
  useEffect(() => { if (open) markRead(path); }, [open, msgs.length, path]);
  useEffect(() => { const el = list.current; if (el) el.scrollTop = el.scrollHeight; }, [visible.length, thread, open]);
  if (!open) return null;
  const names = [...new Set([...(peers || []).map((p) => (p.name || '').replace(/^🤖\s*/, '')), ...msgs.map((m) => (m.author.name || '').replace(/^🤖\s*/, ''))])]
    .filter((n) => n && n !== me.name);
  const toggleNotify = async () => {
    if (notify) { try { localStorage.setItem('audioviz.notify', '0'); } catch (e) { /* none */ } setNotify(false); return; }
    if (!('Notification' in window)) { alert(t('This browser has no notifications')); return; }
    const p = Notification.permission === 'granted' ? 'granted' : await Notification.requestPermission();
    if (p === 'granted') { try { localStorage.setItem('audioviz.notify', '1'); } catch (e) { /* none */ } setNotify(true); }
  };
  const edit = async (m, remove) => {
    if (remove && !confirm(t('Remove this message?'))) return;
    let text = m.text;
    if (!remove) { text = prompt(t('Edit the message:'), m.text); if (text == null) return; }
    try { await api('POST', '/api/chat/edit', { client: me.id, id: m.id, text, remove }); } catch (e) { alert(e.message || e); }
  };
  const who = Object.entries(typing).filter(([, v]) => (v.thread || null) === (thread || null) || !thread).map(([, v]) => v.name).filter(Boolean);
  const head = thread && msgs.find((m) => m.id === thread);
  return html`<aside class="chat" onClick=${(e) => { const a = e.target.closest('a.avlink'); if (a) { e.preventDefault(); onAvLink(a.dataset.av); } }}>
    <div class="chat-h">
      ${thread ? html`<button class="btn icon ghost" title=${t('Back')} onClick=${() => setThread(null)}><${Icon} name="arrow-left" size=${15} /></button><b>${t('Thread')}</b>`
        : html`<${Icon} name="message-square" size=${15} cls="muted" /><b>${t('Chat')}</b><span class="faint">${t('in the project')}</span>`}
      <span class="spacer"></span>
      <button class=${'btn icon ghost' + (notify ? '' : ' off')} title=${notify ? t('Notifications on (when this tab is not visible)') : t('Notify me of new messages')} onClick=${toggleNotify}>
        <${Icon} name=${notify ? 'bell' : 'bell-off'} size=${15} /></button>
      <button class="btn icon ghost" title=${t('Close')} onClick=${onClose}><${Icon} name="x" size=${15} /></button>
    </div>
    <div class="chat-list" ref=${list}>
      ${!visible.length && html`<p class="faint cempty">${t('No messages yet. Everything written here stays in the project.')}</p>`}
      ${visible.map((m) => html`<${Msg} key=${m.id} m=${m} me=${me} answers=${answers[m.id] || 0} inThread=${!!thread} onThread=${setThread} onEdit=${edit} onSeek=${onSeek} />`)}
      ${thread && head && head.gone && html`<p class="faint">${t('(its first message was removed)')}</p>`}
    </div>
    ${who.length > 0 && html`<div class="ctyping faint">${t('{who} writing…', { who: who.join(', ') })}</div>`}
    <${Composer} me=${me} thread=${thread} names=${names} linkNow=${linkNow} timeStore=${timeStore} />
  </aside>`;
}
