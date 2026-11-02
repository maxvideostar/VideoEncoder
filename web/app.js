'use strict';
// ====================== utilitare ======================
const $ = s => document.querySelector(s);
const $$ = s => [...document.querySelectorAll(s)];
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const baseName = p => String(p).split(/[\\/]/).pop();
const fmtSize = b => { b = +b || 0; if (b < 1024) return b + ' B'; const u = ['KB','MB','GB','TB']; let i = -1; do { b /= 1024; i++; } while (b >= 1024 && i < 3); return b.toFixed(b < 10 ? 2 : 1) + ' ' + u[i]; };
const fmtTime = s => { s = Math.max(0, Math.round(+s || 0)); const h = Math.floor(s/3600), m = Math.floor(s%3600/60), x = s%60; return (h ? h + ':' : '') + String(m).padStart(h?2:1,'0') + ':' + String(x).padStart(2,'0'); };

async function api(path, opts = {}) {
  const o = { headers: {}, ...opts };
  if (o.body && typeof o.body !== 'string' && !(o.body instanceof FormData)) { o.body = JSON.stringify(o.body); o.headers['Content-Type'] = 'application/json'; }
  const r = await fetch(path, o);
  if (r.status === 401) { location.href = '/login'; throw new Error('neautentificat'); }
  const ct = r.headers.get('content-type') || '';
  const txt = await r.text();
  let data = txt;
  if (ct.includes('json')) { try { data = JSON.parse(txt); } catch { data = { error: 'Răspuns invalid de la server: ' + txt.slice(0, 200) }; if (r.ok) throw new Error(data.error); } }
  if (!r.ok) throw new Error((data && data.error) || ('HTTP ' + r.status));
  return data;
}
function toast(msg, kind = '') {
  const d = document.createElement('div'); d.className = kind; d.textContent = msg;
  $('#toast').appendChild(d); setTimeout(() => d.remove(), kind === 'e' ? 7000 : 3500);
}

// ====================== presetări ======================
const DEFAULTS = {
  container:'mp4', vcodec:'libx264', rate_mode:'quality', quality:23, vbitrate:'', maxrate:'', speed:5,
  resolution:'keep', width:0, height:0, fps:'keep', deinterlace:false, pix_fmt:'yuv420p', gop:0, tune:'', hw_decode:false,
  acodec:'aac', abitrate:'192k', achannels:'', asamplerate:'', volume:'', loudnorm:'',
  start:'', end:'', subs:'auto', map_all:false, faststart:true, hls_time:6,
  qsv_mode:'auto', deint_mode:'', output_mode:'file', stream_url:'', realtime:true, loop:false, auto_restart:true,
  vfilter:'', afilter:'', extra_args:'', in_args:'', out_dir:'', same_dir:false, suffix:'', overwrite:false,
};
const BUILTIN = [
  { n:'H.264 MP4 – calitate bună (CPU)', o:{} },
  { n:'H.264 MP4 – NVIDIA NVENC (rapid)', o:{ vcodec:'h264_nvenc', quality:23 } },
  { n:'H.264 MP4 – Intel QuickSync', o:{ vcodec:'h264_qsv', quality:23 } },
  { n:'H.264 MP4 – AMD AMF', o:{ vcodec:'h264_amf', quality:23 } },
  { n:'HEVC/H.265 – fișier mic (CPU)', o:{ vcodec:'libx265', quality:26, speed:4, abitrate:'160k', extra_args:'-tag:v hvc1' } },
  { n:'HEVC/H.265 – NVIDIA NVENC', o:{ vcodec:'hevc_nvenc', quality:26, extra_args:'-tag:v hvc1' } },
  { n:'AV1 SVT – foarte mic (MKV)', o:{ container:'mkv', vcodec:'libsvtav1', quality:32, speed:4, acodec:'libopus', abitrate:'128k', pix_fmt:'yuv420p10le', faststart:false } },
  { n:'Web 720p (H.264)', o:{ resolution:'720', quality:23, abitrate:'128k', achannels:'2' } },
  { n:'Broadcast TS 1080p25 CBR 8 Mbps', o:{ container:'ts', rate_mode:'bitrate', vbitrate:'8000k', maxrate:'8000k', resolution:'1080', fps:'25', gop:50, deinterlace:true, acodec:'aac', abitrate:'192k', asamplerate:'48000', achannels:'2', loudnorm:'-23', faststart:false, speed:4 } },
  { n:'HLS VOD 720p (m3u8)', o:{ container:'hls', resolution:'720', gop:50, abitrate:'128k', achannels:'2', hls_time:6, faststart:false } },
  { n:'Remux → MKV (fără re-encodare)', o:{ container:'mkv', vcodec:'copy', acodec:'copy', map_all:true, subs:'copy', faststart:false } },
  { n:'Remux → MP4 (fără re-encodare)', o:{ vcodec:'copy', acodec:'copy' } },
  { n:'Normalizare audio EBU R128 (video copiat)', o:{ vcodec:'copy', acodec:'aac', abitrate:'192k', loudnorm:'-23', asamplerate:'48000' } },
  { n:'Extrage audio – MP3 320k', o:{ container:'mp3', vcodec:'none', acodec:'libmp3lame', abitrate:'320k' } },
  { n:'Extrage audio – WAV 48 kHz', o:{ container:'wav', vcodec:'none', acodec:'pcm_s16le', asamplerate:'48000' } },
  { n:'WebM VP9 + Opus', o:{ container:'webm', vcodec:'libvpx-vp9', quality:32, speed:4, acodec:'libopus', abitrate:'128k', faststart:false } },
  { n:'Stream RTMP 720p – 2,5 Mbps (YouTube/Facebook/nginx)', o:{ output_mode:'stream', resolution:'720', fps:'25', rate_mode:'bitrate', vbitrate:'2500k', maxrate:'2500k', gop:50, speed:3, tune:'zerolatency', acodec:'aac', abitrate:'128k', asamplerate:'44100', achannels:'2', faststart:false } },
  { n:'Stream RTMP 1080p – 5 Mbps', o:{ output_mode:'stream', resolution:'1080', fps:'25', rate_mode:'bitrate', vbitrate:'5000k', maxrate:'5000k', gop:50, speed:3, tune:'zerolatency', acodec:'aac', abitrate:'160k', asamplerate:'48000', achannels:'2', faststart:false } },
  { n:'Stream RTMP – Intel QuickSync 720p (TV 1080i, tot pe GPU)', o:{ output_mode:'stream', vcodec:'h264_qsv', hw_decode:true, deinterlace:true, deint_mode:'fast', resolution:'720', fps:'25', rate_mode:'bitrate', vbitrate:'2500k', maxrate:'2500k', gop:50, speed:4, acodec:'aac', abitrate:'128k', asamplerate:'48000', achannels:'2', faststart:false } },
  { n:'Stream RTMP – Intel QuickSync 1080p (TV 1080i, tot pe GPU)', o:{ output_mode:'stream', vcodec:'h264_qsv', hw_decode:true, deinterlace:true, resolution:'keep', fps:'25', rate_mode:'bitrate', vbitrate:'4500k', maxrate:'4500k', gop:50, speed:4, acodec:'aac', abitrate:'160k', asamplerate:'48000', achannels:'2', faststart:false } },
  { n:'Restream (copiere, fără re-encodare)', o:{ output_mode:'stream', vcodec:'copy', acodec:'copy', faststart:false } },
  { n:'Stream MPEG-TS UDP/SRT 1080i25 → 1080p', o:{ output_mode:'stream', deinterlace:true, resolution:'1080', fps:'25', rate_mode:'bitrate', vbitrate:'6000k', maxrate:'6000k', gop:50, speed:3, tune:'zerolatency', acodec:'aac', abitrate:'192k', asamplerate:'48000', faststart:false } },
  { n:'GIF animat 480px', o:{ container:'gif', fps:'12', resolution:'keep', faststart:false } },
  { n:'ProRes 422 HQ (editare, MOV)', o:{ container:'mov', vcodec:'prores_ks', acodec:'pcm_s16le', pix_fmt:'yuv422p10le', faststart:false } },
  { n:'Deîntrețesere 576i → 576p (H.264)', o:{ deinterlace:true, quality:20, speed:6 } },
];
const AUDIO_CONT = ['mp3','aac','m4a','flac','wav','opus'];

let opts = { ...DEFAULTS };
let currentPreset = BUILTIN[0].n;
let userPresets = {};
let sources = [];
let selSrc = null;
let sys = {};

// ====================== formular ======================
function readForm() {
  $$('[data-o]').forEach(el => {
    const k = el.dataset.o;
    if (el.type === 'checkbox') opts[k] = el.checked;
    else if (el.dataset.num !== undefined) opts[k] = el.value === '' ? 0 : Number(el.value);
    else opts[k] = el.value;
  });
}
function writeForm() {
  $$('[data-o]').forEach(el => {
    const k = el.dataset.o, v = opts[k];
    if (el.type === 'checkbox') el.checked = !!v;
    else if (el.dataset.num !== undefined && el.type !== 'range') el.value = v ? v : '';
    else {
      el.value = v ?? '';
      if (el.tagName === 'SELECT' && el.value !== String(v ?? '') && v) { // valoare nelistată
        const o = document.createElement('option'); o.value = o.textContent = v; el.appendChild(o); el.value = v;
      }
    }
  });
  updateUI();
}

function codecInfo(c) {
  if (c === 'libx264' || c === 'libx265') return { lbl:'CRF', min:0, max:51, hint:'18–28 uzual · mai mic = calitate mai bună', sp:['ultrafast','veryfast','faster','fast','medium','slow','veryslow'] };
  if (c === 'libsvtav1') return { lbl:'CRF', min:0, max:63, hint:'25–40 uzual', sp:['12','10','8','7','6','4','2'].map(x=>'preset '+x) };
  if (c === 'libvpx-vp9') return { lbl:'CRF', min:0, max:63, hint:'28–36 uzual', sp:['cpu-used 8','5','4','3','2','1','0'] };
  if (/_nvenc$/.test(c)) return { lbl:'CQ', min:0, max:51, hint:'19–28 uzual', sp:['p1','p2','p3','p4','p5','p6','p7'] };
  if (/_qsv$/.test(c)) return { lbl:'Global quality', min:1, max:51, hint:'20–28 uzual', sp:['veryfast','veryfast','faster','fast','medium','slow','veryslow'] };
  if (/_amf$/.test(c)) return { lbl:'QP', min:0, max:51, hint:'20–28 uzual', sp:['speed','speed','balanced','balanced','balanced','quality','quality'] };
  if (c === 'mpeg2video') return { lbl:'Calitate', min:0, max:51, hint:'mai mic = mai bun', sp:null };
  return { lbl:'Calitate', min:0, max:51, hint:'', sp:null };
}

function streamHint(u) {
  u = (u || '').toLowerCase();
  if (!u) return 'Exemple: rtmp://IP/live/cheie · rtmps://live-api-s.facebook.com:443/rtmp/CHEIE · srt://IP:9000?mode=caller · udp://239.1.1.1:1234?pkt_size=1316';
  if (/^rtmps?:/.test(u)) return 'RTMP → format FLV. Video H.264 (sau copiere dacă sursa e H.264), audio AAC. Recomandat: bitrate constant + GOP = 2 × fps.';
  if (/^srt:/.test(u)) return 'SRT → MPEG-TS. Adaugă ?mode=caller (trimite) sau ?mode=listener&latency=200000.';
  if (/^udp:/.test(u)) return 'UDP → MPEG-TS. Pentru multicast adaugă ?pkt_size=1316&ttl=4.';
  if (/^rtsp:/.test(u)) return 'RTSP (TCP) → necesită un server RTSP (ex. MediaMTX).';
  return 'Protocol: ' + u.split(':')[0] + ' → MPEG-TS';
}

function useQsvFull() { return /_qsv$/.test(opts.vcodec) && opts.hw_decode && ['d3d11','dxva2'].includes(opts.qsv_mode) && opts.subs !== 'burn' && !opts.vfilter; }
function updateUI() {
  const stream = opts.output_mode === 'stream';
  const audio = !stream && AUDIO_CONT.includes(opts.container);
  const gif = !stream && opts.container === 'gif';
  const enc = !audio && !gif && !['copy','none'].includes(opts.vcodec);
  const ci = codecInfo(opts.vcodec);
  const show = (sel, on) => $$(sel).forEach(e => e.style.display = on ? '' : 'none');

  $('#vcodecSel').disabled = audio || gif;
  show('.vq', enc && opts.rate_mode === 'quality');
  show('.vb', enc && opts.rate_mode === 'bitrate');
  show('.custres', (enc || gif) && opts.resolution === 'custom');
  $$('#page-encode [data-o=rate_mode],[data-o=speed],[data-o=pix_fmt],[data-o=gop],[data-o=tune],[data-o=deinterlace]').forEach(e => {
    const lab = e.closest('label'); if (lab) lab.style.display = enc ? '' : 'none';
  });
  $('[data-o=resolution]').closest('label').style.display = (enc || gif) ? '' : 'none';
  $('[data-o=fps]').closest('label').style.display = (enc || gif) ? '' : 'none';
  $('[data-o=hls_time]').closest('label').style.display = !stream && opts.container === 'hls' ? '' : 'none';
  $('[data-o=faststart]').closest('label').style.display = !stream && ['mp4','mov','m4a'].includes(opts.container) ? '' : 'none';
  show('.deintmode', enc && opts.deinterlace && !(useQsvFull()));
  show('.qsvmode', enc && /_qsv$/.test(opts.vcodec) && opts.hw_decode);
  show('.fileout', !stream);
  show('.streamout', stream);
  if (stream) $('#streamHint').textContent = streamHint(opts.stream_url);
  if (stream && /\s/.test(opts.stream_url || '')) $('#streamHint').innerHTML = '<span class="err">⚠ URL-ul conține spații – vor fi eliminate automat.</span> ' + esc(streamHint(opts.stream_url));
  const hwl = $('[data-o=hw_decode]').closest('label');
  hwl.title = /_qsv$/.test(opts.vcodec) ? 'Cu Intel QSV: decodare + deîntrețesere + scalare + encodare complet pe placa video (vpp_qsv) – recomandat pe procesoare slabe' : 'Decodare pe placa video (-hwaccel auto)';
  hwl.lastChild.textContent = /_qsv$/.test(opts.vcodec) ? ' Totul pe GPU Intel (decodare + filtre)' : ' Decodare hardware';
  $('#enqueueBtn').textContent = stream ? '● Pornește stream-ul' : '＋ Adaugă în coadă';
  $('[data-o=tune]').closest('label').style.display = enc && /^libx26/.test(opts.vcodec) ? '' : 'none';

  const q = $('#qRange'); q.min = ci.min; q.max = ci.max; q.value = opts.quality;
  $('#qLabel').textContent = 'Calitate (' + ci.lbl + ')'; $('#qVal').textContent = opts.quality; $('#qHint').textContent = ci.hint;
  $('#spVal').textContent = opts.speed;
  $('#spHint').textContent = (ci.sp ? ci.sp[opts.speed - 1] + ' · ' : '') + '1 = rapid, 7 = calitate maximă';

  // acodec pentru containere audio
  const ac = $('[data-o=acodec]');
  [...ac.options].forEach(o => o.disabled = audio && (o.value === 'none'));
  const lossless = ['flac','pcm_s16le','copy','none'].includes(opts.acodec);
  $('[data-o=abitrate]').closest('label').style.display = lossless ? 'none' : '';
  ['achannels','asamplerate','volume','loudnorm'].forEach(k => $(`[data-o=${k}]`).closest('label').style.display = ['copy','none'].includes(opts.acodec) ? 'none' : '');

  // encodere HW indisponibile
  if (sys.hw_tested) $$('#vcodecSel option').forEach(o => {
    if (/_(nvenc|qsv|amf)$/.test(o.value)) {
      const ok = !!sys.hw[o.value];
      o.disabled = !ok && opts.vcodec !== o.value;
      o.textContent = o.textContent.replace(/ \(indisponibil\)$/, '') + (ok ? '' : ' (indisponibil)');
    }
  });
  $('#srcCountLbl').textContent = sources.length ? `${sources.length} fișier(e) selectate` : 'niciun fișier selectat';
  schedulePreview();
}

let pvTimer, stTimer, stateReady = false;
function schedulePreview() { clearTimeout(pvTimer); pvTimer = setTimeout(preview, 250); saveState(); }
// starea formularului se păstrează pe server (supraviețuiește restartului aplicației/serviciului)
function saveState() {
  if (!stateReady) return;
  clearTimeout(stTimer);
  stTimer = setTimeout(() => {
    api('/api/ui-state', { method:'POST', body:{ opts, sources, selSrc, preset: currentPreset } }).catch(() => {});
  }, 600);
}
async function loadState() {
  try {
    const st = await api('/api/ui-state');
    if (st && st.opts) { opts = { ...DEFAULTS, ...st.opts }; }
    if (st && Array.isArray(st.sources)) sources = st.sources.filter(Boolean);
    if (st && st.preset) currentPreset = st.preset;
    if (st && st.selSrc && sources.includes(st.selSrc)) selSrc = st.selSrc; else selSrc = sources[0] || null;
  } catch {}
}
async function preview() {
  try {
    const r = await api('/api/preview', { method:'POST', body:{ input: selSrc || sources[0] || '', options: opts } });
    $('#cmdPreview').textContent = r.cmd + '\n\n→ ' + r.output;
  } catch (e) { $('#cmdPreview').textContent = e.message; }
}

function onFieldChange(e) {
  const k = e.target.dataset.o;
  readForm();
  if (k === 'container') {
    if (opts.container === 'webm' && !['libvpx-vp9','libsvtav1','copy'].includes(opts.vcodec)) { opts.vcodec = 'libvpx-vp9'; opts.quality = 32; opts.acodec = 'libopus'; }
    if (AUDIO_CONT.includes(opts.container)) {
      const m = { mp3:'libmp3lame', aac:'aac', m4a:'aac', flac:'flac', wav:'pcm_s16le', opus:'libopus' };
      if (!['copy', m[opts.container]].includes(opts.acodec)) opts.acodec = m[opts.container];
    }
    writeForm(); return;
  }
  if (k === 'vcodec') {
    const ci = codecInfo(opts.vcodec);
    if (opts.quality < ci.min || opts.quality > ci.max) opts.quality = Math.round((ci.min + ci.max) / 2);
    if (opts.vcodec === 'libx265' || opts.vcodec === 'hevc_nvenc') { if (opts.quality === 23) opts.quality = 26; }
    writeForm(); return;
  }
  if (k === 'out_dir' && opts.out_dir) { opts.same_dir = false; $('[data-o=same_dir]').checked = false; }
  updateUI();
}

// ====================== presetări UI ======================
function fillPresetSelect() {
  const s = $('#presetSel');
  let h = '<optgroup label="Incluse">' + BUILTIN.map(p => `<option value="b:${esc(p.n)}">${esc(p.n)}</option>`).join('') + '</optgroup>';
  const names = Object.keys(userPresets);
  if (names.length) h += '<optgroup label="Ale mele">' + names.map(n => `<option value="u:${esc(n)}">${esc(n)}</option>`).join('') + '</optgroup>';
  s.innerHTML = h;
  const cur = userPresets[currentPreset] ? 'u:' + currentPreset : 'b:' + currentPreset;
  s.value = cur;
}
function applyPreset(val) {
  const [t, ...rest] = val.split(':'); const n = rest.join(':');
  const keepOut = { out_dir: opts.out_dir, same_dir: opts.same_dir, overwrite: opts.overwrite, stream_url: opts.stream_url };
  const p = t === 'u' ? userPresets[n] : (BUILTIN.find(x => x.n === n) || {}).o;
  if (!p) return;
  opts = { ...DEFAULTS, ...p, ...(t === 'b' ? keepOut : {}) };
  currentPreset = n;
  writeForm();
}
function describe(o) {
  o = { ...DEFAULTS, ...o };
  const parts = [o.container.toUpperCase()];
  if (!AUDIO_CONT.includes(o.container) && o.container !== 'gif') parts.push(o.vcodec === 'copy' ? 'video copiat' : o.vcodec === 'none' ? 'fără video' : o.vcodec + (o.rate_mode === 'bitrate' ? ' ' + o.vbitrate : ' q' + o.quality));
  if (o.resolution !== 'keep') parts.push(o.resolution === 'custom' ? `${o.width||'auto'}x${o.height||'auto'}` : o.resolution + 'p');
  if (o.fps !== 'keep') parts.push(o.fps + 'fps');
  parts.push(o.acodec === 'copy' ? 'audio copiat' : o.acodec === 'none' ? 'fără audio' : o.acodec + (['flac','pcm_s16le'].includes(o.acodec) ? '' : ' ' + o.abitrate));
  if (o.loudnorm) parts.push('loudnorm ' + o.loudnorm);
  if (o.output_mode === 'stream') parts[0] = 'STREAM';
  return parts.join(' · ');
}
function renderPresetsPage() {
  const names = Object.keys(userPresets);
  $('#userPresets').innerHTML = names.length ? names.map(n => `
    <div class="pitem"><div class="nm">${esc(n)}</div><div class="ds">${esc(describe(userPresets[n]))}</div>
    <div class="acts"><button class="btn sm primary" data-use="u:${esc(n)}">Folosește</button><button class="btn sm danger" data-del="${esc(n)}">Șterge</button></div></div>`).join('')
    : '<span class="dim">Nu ai salvat încă presetări. Configurează în pagina Encodare și apasă „Salvează ca presetare”.</span>';
  $('#builtinPresets').innerHTML = BUILTIN.map(p => `
    <div class="pitem"><div class="nm">${esc(p.n)}</div><div class="ds">${esc(describe(p.o))}</div>
    <div class="acts"><button class="btn sm primary" data-use="b:${esc(p.n)}">Folosește</button></div></div>`).join('');
}
async function loadPresets() {
  try { userPresets = await api('/api/presets'); } catch { userPresets = {}; }
  fillPresetSelect(); renderPresetsPage();
}

// ====================== surse ======================
function renderSources() {
  $('#srclist').innerHTML = sources.map((p, i) => `
    <li class="${p === selSrc ? 'sel' : ''}" data-i="${i}" title="${esc(p)}"><span>🎞</span><span class="nm">${esc(baseName(p) || p)}</span><button class="x" data-rm="${i}" title="Elimină">×</button></li>`).join('')
    || '<li class="dim" style="cursor:default">Niciun fișier. Răsfoiește sau încarcă.</li>';
  updateUI();
}
function addSources(list) {
  let added = 0;
  list.forEach(p => { p = String(p).trim().replace(/^"|"$/g, ''); if (p && !sources.includes(p)) { sources.push(p); added++; } });
  if (!selSrc && sources.length) selectSource(sources[0]);
  renderSources();
  return added;
}
async function selectSource(p) {
  selSrc = p; renderSources();
  const box = $('#probe'); box.innerHTML = '<span class="dim">Analizez…</span>';
  try {
    const d = await api('/api/probe?path=' + encodeURIComponent(p));
    const f = d.format || {};
    let h = `<div class="st"><b>${esc(baseName(p))}</b><br><span class="muted">${esc(f.format_long_name || f.format_name || '')}</span><br>
      Durată: <b>${fmtTime(f.duration)}</b> · Mărime: <b>${fmtSize(f.size)}</b> · Bitrate: <b>${f.bit_rate ? Math.round(f.bit_rate/1000) + ' kb/s' : '—'}</b></div>`;
    (d.streams || []).forEach(s => {
      const t = s.codec_type, cls = t === 'video' ? 'v' : t === 'audio' ? 'a' : 's';
      let info = esc(s.codec_name || '');
      if (t === 'video') {
        const fr = s.avg_frame_rate && s.avg_frame_rate.includes('/') ? (s.avg_frame_rate.split('/')[0] / (s.avg_frame_rate.split('/')[1] || 1)) : 0;
        info += ` · ${s.width}x${s.height} · ${fr ? fr.toFixed(fr % 1 ? 3 : 0) + ' fps' : ''} · ${esc(s.pix_fmt || '')}${s.field_order && s.field_order !== 'progressive' ? ' · <b style="color:var(--warn)">întrețesut</b>' : ''}`;
      } else if (t === 'audio') {
        info += ` · ${s.channels || ''}ch ${esc(s.channel_layout || '')} · ${s.sample_rate || ''} Hz${s.bit_rate ? ' · ' + Math.round(s.bit_rate/1000) + ' kb/s' : ''}`;
      }
      const lang = s.tags && s.tags.language ? ` · ${esc(s.tags.language)}` : '';
      h += `<div class="st"><span class="tag ${cls}">#${s.index} ${esc(t)}</span>${info}${lang}</div>`;
    });
    box.innerHTML = h;
  } catch (e) { box.innerHTML = `<span class="err">${esc(e.message)}</span>`; }
  schedulePreview();
}

function uploadFiles(files) {
  if (!files.length) return;
  const fd = new FormData(); [...files].forEach(f => fd.append('file', f, f.name));
  const bar = $('#upbar'), fill = bar.querySelector('i'); bar.style.display = 'block'; fill.style.width = '0';
  const xhr = new XMLHttpRequest();
  xhr.open('POST', '/api/upload');
  xhr.upload.onprogress = e => { if (e.lengthComputable) fill.style.width = (e.loaded / e.total * 100) + '%'; };
  xhr.onload = () => {
    bar.style.display = 'none';
    if (xhr.status === 401) return location.href = '/login';
    try { const r = JSON.parse(xhr.responseText); if (xhr.status !== 200) throw new Error(r.error); addSources(r.files || []); toast(`${(r.files||[]).length} fișier(e) încărcate`, 'o'); }
    catch (e) { toast('Upload eșuat: ' + e.message, 'e'); }
  };
  xhr.onerror = () => { bar.style.display = 'none'; toast('Upload eșuat', 'e'); };
  xhr.send(fd);
}

// ====================== browser fișiere ======================
const fb = { mode:'files', path:'', sel:new Set(), cb:null, entries:[] };
function openBrowser(mode, cb, start) {
  fb.mode = mode; fb.cb = cb; fb.sel.clear();
  $('#fbTitle').textContent = mode === 'dir' ? 'Alege folderul' : 'Alege fișiere';
  $('#fbOk').textContent = mode === 'dir' ? 'Alege acest folder' : 'Adaugă';
  $('#fbSelAll').style.display = mode === 'dir' ? 'none' : '';
  $('#fbModal').classList.add('on');
  let last = ''; try { last = localStorage.getItem('ve_lastdir') || ''; } catch {}
  fbLoad(start || last || '');
}
async function fbLoad(p) {
  try {
    const d = await api('/api/fs?path=' + encodeURIComponent(p) + ($('#fbAll').checked ? '&all=1' : ''));
    fb.path = d.path; fb.parent = d.parent; fb.entries = d.entries || []; fb.sel.clear();
    if (d.path) try { localStorage.setItem('ve_lastdir', d.path); } catch {}
    $('#fbPath').value = d.path;
    $('#fbShort').innerHTML = '<button class="btn sm" data-go="">💻 Unități</button>' + (d.shortcuts || []).map(s => `<button class="btn sm" data-go="${esc(s.path)}">${esc(s.name)}</button>`).join('');
    fbRender();
  } catch (e) { toast(e.message, 'e'); }
}
function fbRender() {
  const rows = fb.entries.filter(e => fb.mode !== 'dir' || e.dir);
  $('#fbList').innerHTML = rows.map(e => `
    <div class="fb-row ${e.dir ? 'dir' : ''} ${fb.sel.has(e.path) ? 'sel' : ''}" data-p="${esc(e.path)}" data-d="${e.dir ? 1 : 0}">
      <span class="ic">${e.dir ? '📁' : e.media ? '🎞' : '·'}</span><span class="nm">${esc(e.name)}</span><span class="sz">${e.dir ? '' : fmtSize(e.size)}</span></div>`).join('')
    || '<div class="fb-row dim">Gol</div>';
  $('#fbSel').textContent = fb.mode === 'dir' ? fb.path : (fb.sel.size ? fb.sel.size + ' selectate' : '');
}

// ====================== coadă ======================
let jobsCache = [];
let lastSys = null;
const lvl = v => v >= 85 ? 'hot' : v >= 65 ? 'warn' : '';
const GPU_ENG_RO = { VideoDecode:'decodare', VideoEncode:'encodare', VideoProcessing:'procesare', '3D':'3D', Copy:'copiere', Compute:'calcul' };
function renderSys(st) {
  if (!st) return;
  lastSys = st;
  const memPct = st.mem_total ? st.mem_used / st.mem_total * 100 : 0;
  const m = (lbl, v) => `<span class="m ${lvl(v)}">${lbl} ${v.toFixed(0)}%<i><b style="width:${v.toFixed(0)}%"></b></i></span>`;
  $('#sysbadge').innerHTML = m('CPU', st.cpu) + m('RAM', memPct) + (st.has_gpu ? m('GPU', st.gpu) : '');
  const setBar = (id, v) => { const e = $(id); e.style.width = v.toFixed(0) + '%'; e.className = lvl(v); };
  $('#spCpu').textContent = st.cpu.toFixed(0) + '%'; setBar('#spCpuBar', st.cpu);
  $('#spMem').textContent = memPct.toFixed(0) + '%'; setBar('#spMemBar', memPct);
  $('#spMemTxt').textContent = `${fmtSize(st.mem_used)} / ${fmtSize(st.mem_total)} · ${st.cores} nuclee`;
  $('#spGpuBox').style.display = st.has_gpu ? '' : 'none';
  if (st.has_gpu) {
    $('#spGpu').textContent = st.gpu.toFixed(0) + '%'; setBar('#spGpuBar', st.gpu);
    const e = Object.entries(st.gpu_eng || {}).filter(([k, v]) => v >= 0.5 || /Video/.test(k)).sort((a, b) => b[1] - a[1]);
    $('#spGpuEng').textContent = e.map(([k, v]) => `${GPU_ENG_RO[k] || k} ${v.toFixed(0)}%`).join(' · ');
  }
  const h = st.history || [];
  if (h.length > 1) {
    const pts = h.map((v, i) => `${(i / (h.length - 1) * 120).toFixed(1)},${(28 - v / 100 * 27).toFixed(1)}`).join(' ');
    $('#spSpark').innerHTML = `<polygon points="0,28 ${pts} 120,28"/><polyline points="${pts}"/>`;
  }
}
const jobHTML = new Map();
const STATUS_RO = { queued:'în așteptare', running:'rulează', paused:'pauză', done:'gata', error:'eroare', canceled:'anulat' };
function jobCard(j) {
  const p = j.progress || 0;
  const live = j.status === 'running' || j.status === 'paused';
  const isLive = j.stream && !j.duration;
  const acts = [];
  if (j.status === 'running') acts.push(['pause', '⏸', 'Pauză']);
  if (j.status === 'paused') acts.push(['resume', '▶', 'Continuă']);
  if (j.status === 'queued') acts.push(['top', '⤒', 'Primul în coadă'], ['up', '↑', 'Sus'], ['down', '↓', 'Jos']);
  if (live || j.status === 'queued') acts.push(['cancel', '■', 'Anulează']);
  if (['done','error','canceled'].includes(j.status)) acts.push(['retry', '↻', 'Reia']);
  let extra = `<button class="btn sm icon" data-log="${j.id}" title="Jurnal">📄</button>`;
  if (j.status === 'done' && !j.stream) {
    extra += `<a class="btn sm" href="/api/jobs/${j.id}/download" title="Descarcă">⬇</a>`;
    if (sys.local) extra += `<button class="btn sm icon" data-reveal="${esc(j.output)}" title="Arată în Explorer">📂</button>`;
  }
  if (!live) extra += `<button class="btn sm icon danger" data-jdel="${j.id}" title="Șterge din listă">🗑</button>`;
  return `<div class="job ${j.status}">
    <div class="top">
      <span class="st-pill ${j.status}">${STATUS_RO[j.status] || j.status}</span>${j.stream ? '<span class="st-pill live">● LIVE</span>' : ''}
      <div class="title" title="${esc(j.input)}">${esc(baseName(j.input))}</div>
      <div class="acts">${acts.map(a => `<button class="btn sm icon" data-ja="${a[0]}" data-id="${j.id}" title="${a[2]}">${a[1]}</button>`).join('')}${extra}</div>
    </div>
    <div class="sub" title="${esc(j.output)}">→ ${esc(j.output)}${j.preset ? ' · <span class="dim">' + esc(j.preset) + '</span>' : ''}</div>
    <div class="bar ${isLive && live ? 'indet' : ''}"><i style="width:${isLive ? (live ? 100 : 0) : j.status === 'queued' ? 0 : p.toFixed(1)}%"></i></div>
    <div class="stats">
      ${isLive ? (live ? `<span>în emisie <b>${fmtTime(j.out_time)}</b></span>` : '') : `<span><b>${p.toFixed(1)}%</b></span>`}
      ${j.restarts ? `<span>reporniri <b>${j.restarts}</b></span>` : ''}
      ${j.duration ? `<span>timp <b>${fmtTime(j.out_time)}</b> / ${fmtTime(j.duration)}</span>` : ''}
      ${live && !isLive ? '' : ''}${live ? `<span>fps <b>${(j.fps||0).toFixed(0)}</b></span><span>viteză <b>${esc(j.speed || '—')}</b></span><span>rămas <b>${j.eta ? fmtTime(j.eta) : '—'}</b></span>` : ''}
      ${j.size ? `<span>mărime <b>${fmtSize(j.size)}</b></span>` : ''}
      ${live && j.pid && lastSys && lastSys.procs && lastSys.procs[j.pid] ? (p => `<span class="jload">CPU <b>${p.cpu.toFixed(0)}%</b></span>${lastSys.has_gpu ? `<span class="jload">GPU <b>${p.gpu.toFixed(0)}%</b></span>` : ''}<span class="jload">RAM <b>${fmtSize(p.mem)}</b></span>`)(lastSys.procs[j.pid]) : ''}
      ${j.bitrate && live ? `<span>bitrate <b>${esc(j.bitrate)}</b></span>` : ''}
      ${j.finished && j.started ? `<span>durată encodare <b>${fmtTime((new Date(j.finished) - new Date(j.started)) / 1000)}</b></span>` : ''}
    </div>
    ${j.error ? `<div class="errmsg">${esc(j.error)}</div>` : ''}
  </div>`;
}
async function refreshJobs() {
  try {
    const d = await api('/api/jobs');
    renderSys(d.sys);
    jobsCache = d.jobs || [];
    const active = jobsCache.filter(j => ['queued','running','paused'].includes(j.status)).length;
    const running = jobsCache.filter(j => j.status === 'running').length;
    const c = $('#qcount'); c.textContent = active; c.classList.toggle('live', running > 0);
    $('#qPauseBtn').textContent = d.paused ? '▶ Pornește coada' : '⏸ Oprește pornirea joburilor';
    $('#qPauseBtn').dataset.paused = d.paused ? '1' : '';
    const done = jobsCache.filter(j => j.status === 'done').length, err = jobsCache.filter(j => j.status === 'error').length;
    $('#qSummary').textContent = `${running} rulează · ${active - running} în așteptare · ${done} gata · ${err} erori` + (d.paused ? ' · COADA ESTE OPRITĂ' : '');
    const list = $('#joblist');
    if (!jobsCache.length) { list.innerHTML = '<div class="empty">Coada este goală. Adaugă fișiere din pagina Encodare.</div>'; jobHTML.clear(); return; }
    // re-randare doar pentru joburile schimbate
    const ids = jobsCache.map(j => j.id);
    if ([...list.children].map(e => e.dataset.id).join() !== ids.join()) { list.innerHTML = ''; jobHTML.clear(); }
    jobsCache.forEach((j, i) => {
      const h = jobCard(j);
      let el = list.children[i];
      if (!el) { el = document.createElement('div'); el.dataset.id = j.id; list.appendChild(el); }
      if (jobHTML.get(j.id) !== h) { el.innerHTML = h; jobHTML.set(j.id, h); }
    });
    // notificare la final
    jobsCache.forEach(j => {
      const prev = lastStatus[j.id];
      if (prev && prev !== j.status && (j.status === 'done' || j.status === 'error')) toast(`${baseName(j.input)}: ${STATUS_RO[j.status]}`, j.status === 'done' ? 'o' : 'e');
      lastStatus[j.id] = j.status;
    });
  } catch (e) { /* ignorăm */ }
}
const lastStatus = {};

let logJob = null, logTimer = null;
async function openLog(id) {
  logJob = id; $('#logModal').classList.add('on');
  const tick = async () => {
    if (!logJob) return;
    try {
      const j = await api('/api/jobs/' + logJob);
      $('#logTitle').textContent = 'Jurnal – ' + baseName(j.input);
      $('#logCmd').textContent = j.cmd;
      $('#logBody').textContent = (j.log || []).join('\n') || '(gol)';
      if ($('#logFollow').checked) { const c = $('#logModal .content'); c.scrollTop = c.scrollHeight; }
    } catch {}
  };
  await tick(); clearInterval(logTimer); logTimer = setInterval(tick, 1200);
}

// ====================== setări / sistem ======================
async function loadSystem() {
  try { sys = await api('/api/system'); } catch { return; }
  const fb = $('#ffbadge');
  if (sys.ffmpeg_version) { fb.className = 'badge ok hide-sm'; fb.textContent = 'FFmpeg OK'; fb.title = sys.ffmpeg_version; }
  else { fb.className = 'badge err hide-sm'; fb.textContent = 'FFmpeg lipsă'; fb.title = 'Mergi la Setări → Descarcă FFmpeg'; }
  const mb = $('#modebadge');
  mb.textContent = (sys.local ? 'Local' : 'Extern · ' + sys.user) + (sys.is_service ? ' · serviciu' : '');
  mb.className = 'badge ' + (sys.local ? '' : 'warn');
  $('#logoutBtn').style.display = sys.local && sys.user === 'local' ? 'none' : '';

  const hw = Object.entries(sys.hw || {});
  $('#sysinfo').innerHTML = `
    <div>Versiune</div><div>${esc(sys.app)} ${esc(sys.version)} · ${esc(sys.os)} · ${sys.cpus} nuclee</div>
    <div>FFmpeg</div><div class="mono" style="word-break:break-all">${sys.ffmpeg_version ? esc(sys.ffmpeg_version) : '<span class="err">negăsit</span>'}<br><span class="dim">${esc(sys.ffmpeg)}</span></div>
    <div>Encodere CPU</div><div class="hwgrid">${(sys.encoders || []).map(e => `<span class="tag">${esc(e)}</span>`).join('') || '—'}</div>
    <div>Encodere hardware</div><div class="hwgrid">${!sys.hw_tested ? '<span class="dim">se testează…</span>' : hw.length ? hw.map(([k, v]) => `<span class="tag ${v ? 'a' : ''}" style="${v ? '' : 'text-decoration:line-through;opacity:.6'}">${esc(k)}</span>`).join('') : '<span class="dim">niciunul</span>'}</div>
    <div>Director date</div><div class="mono">${esc(sys.base_dir)}</div>`;
  const urls = (sys.lan_ips || []).map(ip => `http://${ip}:${sys.port}/`);
  $('#accessInfo').innerHTML = `
    <div class="kv"><div>Local</div><div class="mono">http://127.0.0.1:${sys.port}/</div>
    <div>Rețea (LAN)</div><div class="mono">${urls.map(u => `<a href="${u}" target="_blank">${u}</a>`).join('<br>') || '—'}</div>
    <div>Internet</div><div>Redirecționează portul <b class="mono">${sys.port}</b> TCP din router către acest PC și accesează <span class="mono">http://IP-public:${sys.port}/</span></div></div>
    ${sys.default_pass ? '<p class="err" style="margin:12px 0 0">⚠ Parola externă este încă cea implicită (admin/admin). Schimb-o înainte de a deschide portul!</p>' : ''}
    <p class="hint">Din exterior se cere utilizator + parolă. Pe acest calculator accesul e direct (dacă opțiunea e activă).</p>`;
  updateUI();
}
async function loadSettings() {
  try {
    const s = await api('/api/settings');
    $$('#settingsForm [name]').forEach(el => {
      const v = s[el.name];
      if (el.type === 'checkbox') el.checked = !!v; else if (el.name !== 'new_password') el.value = v ?? '';
    });
  } catch {}
}
async function saveSettings() {
  const body = {};
  $$('#settingsForm [name]').forEach(el => {
    if (el.type === 'checkbox') body[el.name] = el.checked;
    else if (el.type === 'number') body[el.name] = Number(el.value);
    else body[el.name] = el.value;
  });
  if (!body.new_password) delete body.new_password;
  $('#setErr').textContent = '';
  try {
    const r = await api('/api/settings', { method:'POST', body });
    toast(r.restart ? 'Salvat. Repornește aplicația pentru noul port/interfață.' : 'Setări salvate', 'o');
    $('[name=new_password]').value = '';
    loadSettings(); setTimeout(loadSystem, 800);
  } catch (e) { $('#setErr').textContent = e.message; }
}
let dlTimer;
async function pollDL() {
  try {
    const s = await api('/api/ffmpeg/download');
    const el = $('#dlStatus');
    if (s.state === 'downloading') el.textContent = `Descarc FFmpeg… ${fmtSize(s.done)}${s.total > 0 ? ' / ' + fmtSize(s.total) + ` (${(s.done / s.total * 100).toFixed(0)}%)` : ''}`;
    else if (s.state === 'extracting') el.textContent = 'Extrag arhiva…';
    else if (s.state === 'done') { el.textContent = '✓ FFmpeg instalat.'; clearInterval(dlTimer); setTimeout(loadSystem, 4000); setTimeout(loadSystem, 15000); }
    else if (s.state === 'error') { el.innerHTML = `<span class="err">Eroare: ${esc(s.error)}</span>`; clearInterval(dlTimer); }
  } catch {}
}

// ====================== serviciu Windows ======================
const SVC_RO = { absent:'neinstalat', stopped:'oprit', starting:'pornește', stopping:'se oprește', running:'rulează', unknown:'necunoscut' };
const SVC_CLS = { running:'done', stopped:'canceled', absent:'queued', starting:'running', stopping:'paused' };
async function loadService() {
  let s; try { s = await api('/api/service'); } catch { return; }
  const card = $('#svcCard');
  if (!s.supported) { card.style.display = 'none'; return; }
  const pill = $('#svcPill'); pill.textContent = SVC_RO[s.state] || s.state; pill.className = 'st-pill ' + (SVC_CLS[s.state] || '');
  const info = s.is_service
    ? 'Aplicația <b>rulează acum ca serviciu</b>: pornește automat cu Windows, înainte de logare, și nu se închide la delogare.'
    : s.state === 'absent'
      ? 'Instalează VideoEncoder ca serviciu ca să pornească automat cu Windows, înainte de logare, fără fereastră de consolă. Fereastra curentă se va închide, iar serviciul preia același port.'
      : 'Serviciul este instalat, dar acum rulează aplicația normală.';
  $('#svcInfo').innerHTML = info + '<br><span class="dim">Ca serviciu rulează sub contul LocalSystem: unitățile de rețea mapate (Z:) nu sunt vizibile — folosește căi \\\\server\\share. Interfața o deschizi pornind VideoEncoder.exe sau din browser.</span>';
  const b = [];
  if (!sys.local) { $('#svcBtns').innerHTML = '<span class="dim">Serviciul se gestionează doar de pe acest calculator.</span>'; return; }
  if (s.state === 'absent') b.push(['install', '⚙ Instalează ca serviciu', 'primary']);
  if (s.state === 'stopped' && !s.is_service) b.push(['start', '▶ Pornește serviciul', 'primary']);
  if (s.state === 'running' && s.is_service) b.push(['stop', '■ Oprește serviciul', '']);
  if (s.state !== 'absent') b.push(['uninstall', '🗑 Dezinstalează serviciul', 'danger']);
  $('#svcBtns').innerHTML = b.map(x => `<button class="btn ${x[2]}" data-svc="${x[0]}">${x[1]}</button>`).join('');
}
async function serviceAction(a) {
  const conf = {
    install: 'Instalez VideoEncoder ca serviciu Windows?\n\nVa apărea promptul de administrator (UAC). Joburile care rulează acum vor fi întrerupte, iar serviciul preia interfața.',
    start: 'Pornesc serviciul? Aplicația curentă se va închide, iar serviciul preia interfața.',
    stop: 'Oprești serviciul? Interfața nu va mai fi disponibilă până pornești din nou VideoEncoder.exe.',
    uninstall: 'Dezinstalezi serviciul VideoEncoder?' + (sys.is_service ? '\n\nInterfața se va închide. Pornește apoi VideoEncoder.exe normal.' : ''),
  }[a];
  if (!confirm(conf)) return;
  const msg = $('#svcMsg');
  msg.textContent = sys.is_service ? 'Se execută…' : 'Confirmă promptul de administrator (UAC) de pe ecran…';
  try {
    const r = await api('/api/service/' + a, { method:'POST' });
    if (r.handover) { msg.textContent = 'Serviciul pornește, se preia interfața…'; waitReconnect(true); }
    else if (r.stopping) { msg.textContent = 'Serviciul se oprește…'; setTimeout(() => { document.body.innerHTML = '<div class="empty" style="margin:60px">Serviciul VideoEncoder a fost oprit' + (a === 'uninstall' ? ' și dezinstalat' : '') + '. Pornește VideoEncoder.exe pentru a-l folosi din nou.</div>'; }, 1500); }
    else { msg.textContent = 'Gata.'; setTimeout(loadService, 2500); setTimeout(loadService, 6000); }
  } catch (e) { msg.innerHTML = '<span class="err">' + esc(e.message) + '</span>'; }
}
function waitReconnect(expectService) {
  let down = false, tries = 0;
  const t = setInterval(async () => {
    tries++;
    try {
      const s = await fetch('/api/system', { cache:'no-store' }).then(r => r.json());
      if (down || (expectService && s.is_service)) { clearInterval(t); location.reload(); }
    } catch { down = true; }
    if (tries > 120) { clearInterval(t); $('#svcMsg').textContent = 'Serviciul nu a răspuns. Verifică services.msc și videoencoder.log.'; }
  }, 1500);
}

// ====================== evenimente ======================
function showPage(p) {
  $$('nav button').forEach(b => b.classList.toggle('on', b.dataset.page === p));
  $$('.page').forEach(s => s.classList.toggle('on', s.id === 'page-' + p));
  if (p === 'settings') { loadSettings(); loadSystem(); loadService(); }
  if (p === 'presets') renderPresetsPage();
  if (p === 'queue') refreshJobs();
  try { localStorage.setItem('ve_page', p); } catch {}
}

function bind() {
  $$('nav button').forEach(b => b.onclick = () => showPage(b.dataset.page));
  $$('[data-o]').forEach(el => { el.addEventListener('input', onFieldChange); el.addEventListener('change', onFieldChange); });
  $('#presetSel').onchange = e => applyPreset(e.target.value);
  $('#resetBtn').onclick = () => applyPreset($('#presetSel').value);
  $('#savePresetBtn').onclick = async () => {
    const n = prompt('Numele presetării:', userPresets[currentPreset] ? currentPreset : '');
    if (!n) return;
    const o = { ...opts }; delete o.out_dir; delete o.same_dir; delete o.overwrite;
    try { await api('/api/presets', { method:'POST', body:{ name:n, options:o } }); currentPreset = n; await loadPresets(); toast('Presetare salvată', 'o'); }
    catch (e) { toast(e.message, 'e'); }
  };
  $('#page-presets').onclick = async e => {
    const u = e.target.closest('[data-use]'), d = e.target.closest('[data-del]');
    if (u) { $('#presetSel').value = u.dataset.use; applyPreset(u.dataset.use); showPage('encode'); }
    if (d && confirm('Ștergi presetarea „' + d.dataset.del + '”?')) { await api('/api/presets/' + encodeURIComponent(d.dataset.del), { method:'DELETE' }); loadPresets(); }
  };

  // surse
  $('#browseBtn').onclick = () => openBrowser('files', paths => { const n = addSources(paths); toast(n + ' fișier(e) adăugate'); });
  $('#uploadBtn').onclick = () => $('#fileInput').click();
  $('#fileInput').onchange = e => { uploadFiles(e.target.files); e.target.value = ''; };
  $('#addManual').onclick = () => { const v = $('#manualPath').value; if (v.trim()) { addSources(v.split(/\r?\n/)); $('#manualPath').value = ''; } };
  $('#manualPath').onkeydown = e => { if (e.key === 'Enter') $('#addManual').click(); };
  $('#clearSrc').onclick = () => { sources = []; selSrc = null; $('#probe').innerHTML = '<span class="dim">Selectează un fișier din listă.</span>'; renderSources(); };
  $('#srclist').onclick = e => {
    const rm = e.target.closest('[data-rm]');
    if (rm) { const p = sources.splice(+rm.dataset.rm, 1)[0]; if (p === selSrc) selSrc = sources[0] || null; renderSources(); if (selSrc) selectSource(selSrc); return; }
    const li = e.target.closest('li[data-i]'); if (li) selectSource(sources[+li.dataset.i]);
  };
  const drop = $('#drop');
  ['dragenter','dragover'].forEach(ev => document.addEventListener(ev, e => { if (e.dataTransfer && [...e.dataTransfer.types].includes('Files')) { e.preventDefault(); drop.classList.add('over'); } }));
  ['dragleave','drop'].forEach(ev => document.addEventListener(ev, e => { if (ev === 'dragleave' && e.relatedTarget) return; drop.classList.remove('over'); }));
  document.addEventListener('drop', e => { if (e.dataTransfer && e.dataTransfer.files.length) { e.preventDefault(); showPage('encode'); uploadFiles(e.dataTransfer.files); } });

  $('#pickOut').onclick = () => openBrowser('dir', p => { opts.out_dir = p; opts.same_dir = false; writeForm(); }, opts.out_dir);
  $('#pickOut2').onclick = () => openBrowser('dir', p => { $('[name=output_dir]').value = p; }, $('[name=output_dir]').value);
  $('#copyCmd').onclick = () => { navigator.clipboard?.writeText($('#cmdPreview').textContent.split('\n\n→')[0]).then(() => toast('Comandă copiată')); };
  $('#enqueueBtn').onclick = async () => {
    readForm();
    if (!sources.length) return toast('Adaugă cel puțin un fișier sursă', 'e');
    try {
      const inputs = opts.output_mode === 'stream' ? [selSrc || sources[0]] : sources;
      const r = await api('/api/jobs', { method:'POST', body:{ inputs, options: opts, preset: currentPreset } });
      toast(`${r.jobs.length} job(uri) adăugate în coadă`, 'o'); refreshJobs();
    } catch (e) { toast(e.message, 'e'); }
  };

  // browser fișiere
  $('#fbList').onclick = e => {
    const r = e.target.closest('.fb-row[data-p]'); if (!r) return;
    if (r.dataset.d === '1') return fbLoad(r.dataset.p);
    if (fb.mode === 'dir') return;
    fb.sel.has(r.dataset.p) ? fb.sel.delete(r.dataset.p) : fb.sel.add(r.dataset.p); fbRender();
  };
  $('#fbList').ondblclick = e => {
    const r = e.target.closest('.fb-row[data-p]');
    if (r && r.dataset.d !== '1' && fb.mode === 'files') { fb.sel.add(r.dataset.p); $('#fbOk').click(); }
  };
  $('#fbShort').onclick = e => { const b = e.target.closest('[data-go]'); if (b) fbLoad(b.dataset.go); };
  $('#fbUp').onclick = () => fbLoad(fb.parent || '');
  $('#fbGo').onclick = () => fbLoad($('#fbPath').value);
  $('#fbPath').onkeydown = e => { if (e.key === 'Enter') fbLoad($('#fbPath').value); };
  $('#fbAll').onchange = () => fbLoad(fb.path);
  $('#fbSelAll').onclick = () => { fb.entries.filter(e => !e.dir).forEach(e => fb.sel.add(e.path)); fbRender(); };
  $('#fbOk').onclick = () => {
    if (fb.mode === 'dir') { if (!fb.path) return toast('Intră într-un folder', 'e'); fb.cb && fb.cb(fb.path); }
    else { if (!fb.sel.size) return toast('Selectează fișiere (click pe ele)', 'e'); fb.cb && fb.cb([...fb.sel]); }
    $('#fbModal').classList.remove('on');
  };

  // modale
  $$('.modal').forEach(m => m.addEventListener('click', e => {
    if (e.target === m || e.target.closest('[data-close]')) { m.classList.remove('on'); if (m.id === 'logModal') { logJob = null; clearInterval(logTimer); } }
  }));
  document.addEventListener('keydown', e => { if (e.key === 'Escape') $$('.modal.on').forEach(m => m.click()); });

  // coadă
  $('#joblist').onclick = async e => {
    const a = e.target.closest('[data-ja]'), l = e.target.closest('[data-log]'), d = e.target.closest('[data-jdel]'), rv = e.target.closest('[data-reveal]');
    try {
      if (a) { if (a.dataset.ja === 'cancel' && !confirm('Anulezi jobul?')) return; await api(`/api/jobs/${a.dataset.id}/${a.dataset.ja}`, { method:'POST' }); }
      if (l) openLog(l.dataset.log);
      if (d) await api('/api/jobs/' + d.dataset.jdel, { method:'DELETE' });
      if (rv) await api('/api/reveal', { method:'POST', body:{ path: rv.dataset.reveal } });
      refreshJobs();
    } catch (err) { toast(err.message, 'e'); }
  };
  $('#qPauseBtn').onclick = async () => { await api('/api/queue/pause', { method:'POST', body:{ paused: !$('#qPauseBtn').dataset.paused } }); refreshJobs(); };
  $('#qClearBtn').onclick = async () => { await api('/api/jobs/clear', { method:'POST' }); refreshJobs(); };

  // setări
  $('#saveSettings').onclick = saveSettings;
  $('#svcBtns').onclick = e => { const b = e.target.closest('[data-svc]'); if (b) serviceAction(b.dataset.svc); };
  $('#testHwBtn').onclick = async () => { $('#testHwBtn').disabled = true; toast('Testez encoderele…'); try { sys = await api('/api/system/test-hw', { method:'POST' }); } catch {} $('#testHwBtn').disabled = false; loadSystem(); };
  $('#dlFFBtn').onclick = async () => {
    try { await api('/api/ffmpeg/download', { method:'POST' }); clearInterval(dlTimer); dlTimer = setInterval(pollDL, 700); pollDL(); }
    catch (e) { toast(e.message, 'e'); }
  };
  $('#shutdownBtn').onclick = async () => { if (confirm('Oprești VideoEncoder? Joburile care rulează vor fi întrerupte.')) { await api('/api/system/shutdown', { method:'POST' }).catch(() => {}); document.body.innerHTML = '<div class="empty" style="margin:60px">VideoEncoder a fost oprit. Poți închide fereastra.</div>'; } };
  $('#logoutBtn').onclick = async () => { await api('/api/logout', { method:'POST' }).catch(() => {}); location.href = '/login'; };
}

// ====================== pornire ======================
(async function init() {
  bind();
  await loadState();
  writeForm();
  renderSources();
  await loadPresets();
  stateReady = true;
  if (selSrc) selectSource(selSrc);
  await loadSystem();
  if (!sys.ffmpeg_version) toast('FFmpeg nu a fost găsit. Mergi la Setări → „Descarcă FFmpeg”.', 'e');
  else if (!sys.hw_tested) setTimeout(loadSystem, 4000);
  let p = 'encode'; try { p = localStorage.getItem('ve_page') || 'encode'; } catch {}
  showPage(p);
  refreshJobs();
  setInterval(() => {
    const busy = jobsCache.some(j => j.status === 'running' || j.status === 'queued');
    if (busy || $('#page-queue').classList.contains('on')) refreshJobs();
  }, 1000);
  setInterval(refreshJobs, 2500);
})();
