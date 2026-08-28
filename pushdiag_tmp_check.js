
// ── 預覽模式（file:// 直接開啟）──
var isPreview = window.location.protocol === 'file:';
if (isPreview) {
    document.getElementById('preview_banner').textContent = 'Running locally — 診斷需要連線 SRS 伺服器才能分析。';
    document.getElementById('preview_banner').classList.remove('hidden');
}

// ── 模式切換 ──
document.getElementById('tabFlv').addEventListener('click', function () {
    this.classList.add('active');
    document.getElementById('tabRtc').classList.remove('active');
    document.getElementById('flvPanel').classList.remove('hidden');
    document.getElementById('rtcPanel').classList.add('hidden');
});
document.getElementById('tabRtc').addEventListener('click', function () {
    this.classList.add('active');
    document.getElementById('tabFlv').classList.remove('active');
    document.getElementById('rtcPanel').classList.remove('hidden');
    document.getElementById('flvPanel').classList.add('hidden');
});

/* ══════════════ FLV 分析 ══════════════ */

// ── 全域狀態 ──
var state = {
  running: false,
  controller: null,
  buffer: new Uint8Array(0),
  bufOff: 0,
  bytes: 0,
  startTime: 0,
  seq: 0,
  videoTags: 0,
  audioTags: 0,
  scriptTags: 0,
  keyframes: 0,
  lastKeyTs: null,
  keyGaps: [],
  lastTs: null,
  videoLastTs: null,
  audioLastTs: null,
  meta: null,
  sps: null,
  naluLenSize: 4,
  aacCfg: null,
  pendingRows: [],
  audioBlocks: [],
  videoBlocks: [],
  videoTsHistory: [],
  fpsSamples: [],
  offsetSamples: [],
  rawCache: new Map(),
  rawMeta: new Map(),
  lastPktTime: 0,
  perSec: new Map(),
  packetSeries: [],
  syncSeries: [],
  paused: false,
  previewUrl: null,
  _lastVct: null,
  _stallCount: 0,
};

var bitrateChart, packetChart, syncChart, gopChart;

var chartCommon = {
  responsive: true,
  maintainAspectRatio: false,
  animation: false,
  plugins: { legend: { labels: { color: '#cbd5e1', boxWidth: 12 } } },
  scales: {
    x: { ticks: { color: '#94a3b8', maxTicksLimit: 12 }, grid: { color: 'rgba(148,163,184,0.1)' } },
    y: { ticks: { color: '#94a3b8' }, grid: { color: 'rgba(148,163,184,0.1)' } },
  },
};

function buildCharts() {
  bitrateChart = new Chart(document.getElementById('bitrateChart'), {
    type: 'line',
    data: { labels: [], datasets: [
      { label: '影片', data: [], borderColor: 'rgb(192,132,252)', backgroundColor: 'rgb(192,132,252)', borderWidth: 1.5, tension: 0.3, pointRadius: 0 },
      { label: '音訊', data: [], borderColor: 'rgb(34,211,238)', backgroundColor: 'rgb(34,211,238)', borderWidth: 1.5, tension: 0.3, pointRadius: 0 },
    ]},
    options: { ...chartCommon },
  });
  packetChart = new Chart(document.getElementById('packetChart'), {
    type: 'scatter',
    data: { datasets: [
      { label: '封包', data: [], borderColor: 'rgb(251,146,60)', backgroundColor: 'rgb(251,146,60)', pointRadius: 1.5 },
      { label: '關鍵影格', data: [], borderColor: 'rgb(239,68,68)', backgroundColor: 'rgb(239,68,68)', pointRadius: 3 },
    ]},
    options: { ...chartCommon },
  });
  syncChart = new Chart(document.getElementById('syncChart'), {
    type: 'line',
    data: { labels: [], datasets: [
      { label: '偏移', data: [], borderColor: 'rgb(52,211,153)', backgroundColor: 'rgb(52,211,153)', borderWidth: 1.5, tension: 0.3, pointRadius: 0 },
    ]},
    options: { ...chartCommon },
  });
  gopChart = new Chart(document.getElementById('gopChart'), {
    type: 'line',
    data: { labels: [], datasets: [
      { label: 'GOP', data: [], borderColor: 'rgb(251,146,60)', backgroundColor: 'rgb(251,146,60)', borderWidth: 1.5, tension: 0.3, pointRadius: 1.5 },
    ]},
    options: { ...chartCommon },
  });
}

function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, function (c) {
    return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
  });
}

function fmtBytes(n) {
  if (n >= 1048576) return (n / 1048576).toFixed(2) + ' MB';
  if (n >= 1024) return (n / 1024).toFixed(1) + ' KB';
  return n + ' B';
}

function fmtNum(n) {
  return (n ?? 0).toLocaleString('en-US');
}

// ── BitReader (Exp-Golomb) ──
function BitReader(data) { this.data = data; this.pos = 0; }
BitReader.prototype.readBits = function (n) {
  var val = 0;
  for (var i = 0; i < n; i++) {
    val = (val << 1) | ((this.data[this.pos >> 3] >> (7 - (this.pos & 7))) & 1);
    this.pos++;
  }
  return val;
};
BitReader.prototype.ue = function () {
  var z = 0;
  while (this.readBits(1) === 0) z++;
  return z ? ((1 << z) - 1) + this.readBits(z) : 0;
};
BitReader.prototype.se = function () {
  var k = this.ue();
  return k % 2 ? (k + 1) / 2 : -(k / 2);
};

// ── SPS 解析 → 解析度 / profile / level / fps ──
function parseSPS(sps) {
  try {
    var br = new BitReader(sps);
    br.readBits(8);
    var profileIdc = br.readBits(8);
    br.readBits(8);
    var levelIdc = br.readBits(8);
    br.ue();

    var highProfiles = [100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135];
    var chromaFormatIdc = 1;
    if (highProfiles.indexOf(profileIdc) >= 0) {
      chromaFormatIdc = br.ue();
      if (chromaFormatIdc === 3) br.readBits(1);
      br.ue(); br.ue(); br.readBits(1);
      if (br.readBits(1)) {
        var lists = chromaFormatIdc !== 3 ? 8 : 12;
        for (var i = 0; i < lists; i++) {
          if (!br.readBits(1)) continue;
          var size = i < 6 ? 16 : 64;
          var last = 8, next = 8;
          for (var j = 0; j < size; j++) {
            if (next !== 0) {
              var d = br.se();
              next = (last + d + 256) % 256;
            }
            last = next === 0 ? last : next;
          }
        }
      }
    }
    br.ue();
    var pocType = br.ue();
    if (pocType === 0) br.ue();
    else if (pocType === 1) {
      br.readBits(1); br.se(); br.se();
      var n = br.ue();
      for (var k = 0; k < n; k++) br.se();
    }
    br.ue();
    br.readBits(1);
    var wMbs = br.ue() + 1;
    var hMbs = br.ue() + 1;
    var frameMbsOnly = br.readBits(1);
    if (!frameMbsOnly) br.readBits(1);
    br.readBits(1);
    var cropL = 0, cropR = 0, cropT = 0, cropB = 0;
    if (br.readBits(1)) { cropL = br.ue(); cropR = br.ue(); cropT = br.ue(); cropB = br.ue(); }
    var width = wMbs * 16 - cropL * 2 - cropR * 2;
    var height = hMbs * 16 * (frameMbsOnly ? 1 : 2) - cropT * 2 - cropB * 2;

    var fps = null;
    if (br.readBits(1)) {
      if (br.readBits(1)) {
        var idc = br.readBits(8);
        if (idc === 255) { br.readBits(16); br.readBits(16); }
      }
      if (br.readBits(1)) br.readBits(1);
      if (br.readBits(1)) {
        br.readBits(3); br.readBits(1);
        if (br.readBits(1)) { br.readBits(8); br.readBits(8); br.readBits(8); }
      }
      if (br.readBits(1)) { br.ue(); br.ue(); }
      if (br.readBits(1)) {
        var numUnits = br.readBits(32);
        var timeScale = br.readBits(32);
        br.readBits(1);
        if (numUnits) fps = timeScale / (2 * numUnits);
      }
    }
    return { width: width, height: height, profileIdc: profileIdc, levelIdc: levelIdc, fps: fps };
  } catch (e) {
    return null;
  }
}

// ── AAC AudioSpecificConfig 解析 ──
function parseAudioSpecificConfig(data) {
  try {
    var br = new BitReader(data);
    var aot = br.readBits(5);
    if (aot === 31) aot = 32 + br.readBits(6);
    var sampleRate = null;
    var sfIndex = br.readBits(4);
    if (sfIndex === 15) sampleRate = br.readBits(24);
    else sampleRate = [96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350][sfIndex];
    var channels = br.readBits(4);
    return { aot: aot, sampleRate: sampleRate, channels: channels };
  } catch (e) {
    return null;
  }
}

var AOT_NAMES = { 1: 'AAC Main', 2: 'AAC LC', 3: 'AAC SSR', 4: 'AAC LTP', 5: 'SBR (HE-AAC)', 29: 'PS (HE-AACv2)' };

// ── AMF0 解析 ──
function readAMFValue(buf, off) {
  var type = buf[off++];
  if (type === 0) {
    var dv = new DataView(buf.buffer, buf.byteOffset + off, 8);
    return { type: type, value: dv.getFloat64(0, false), next: off + 8 };
  }
  if (type === 1) return { type: type, value: buf[off] !== 0, next: off + 1 };
  if (type === 2 || type === 12) {
    var lenBytes = type === 2 ? 2 : 4;
    var len = lenBytes === 2 ? ((buf[off] << 8) | buf[off + 1]) : new DataView(buf.buffer, buf.byteOffset + off).getUint32(0, false);
    var str = new TextDecoder().decode(buf.subarray(off + lenBytes, off + lenBytes + len));
    return { type: type, value: str, next: off + lenBytes + len };
  }
  return { type: type, value: undefined, next: off };
}

function parseOnMetaData(payload) {
  var meta = {};
  if (!payload || payload.length < 2) return meta;
  var rootType = payload[0];
  var off = 1;
  if (rootType === 8) {
    off += 4;
    for (;;) {
      var len = (payload[off] << 8) | payload[off + 1];
      off += 2;
      if (len === 9 && payload[off] === 0x09) break;
      var key = new TextDecoder().decode(payload.subarray(off, off + len));
      off += len;
      var r = readAMFValue(payload, off);
      off = r.next;
      if (typeof r.value !== 'undefined') meta[key] = r.value;
      if (off >= payload.length) break;
    }
  } else if (rootType === 3) {
    for (;;) {
      if (off + 2 > payload.length) break;
      var len2 = (payload[off] << 8) | payload[off + 1];
      off += 2;
      if (len2 === 0) { off += 1; break; }
      var key2 = new TextDecoder().decode(payload.subarray(off, off + len2));
      off += len2;
      var r2 = readAMFValue(payload, off);
      off = r2.next;
      if (typeof r2.value !== 'undefined') meta[key2] = r2.value;
      if (off >= payload.length) break;
    }
  }
  return meta;
}

// ── FLV tag 解析 ──
function parseTags() {
  var off = state.bufOff;

  if (!state.headerOk) {
    var buf = state.buffer;
    if (buf.length - off < 13) return;
    if (String.fromCharCode.apply(null, buf.subarray(off, off + 3)) !== 'FLV') {
      state.headerErr = true;
      return;
    }
    var headerSize = (buf[off + 5] << 24) | (buf[off + 6] << 16) | (buf[off + 7] << 8) | buf[off + 8];
    off += headerSize + 4;
    state.headerOk = true;
    state.bufOff = off;
  }
  if (state.headerErr) return;

  var buf2 = state.buffer;
  var budgetEnd = performance.now() + 50;
  var guard = 0;
  while (buf2.length - off >= 11 && guard++ < 2000) {
    var tagType = buf2[off];
    var dataSize = (buf2[off + 1] << 16) | (buf2[off + 2] << 8) | buf2[off + 3];
    var timestamp = ((buf2[off + 4] << 16) | (buf2[off + 5] << 8) | buf2[off + 6]) | (buf2[off + 7] << 24);
    var total = 11 + dataSize + 4;
    if (buf2.length - off < total) break;

    var data = buf2.subarray(off + 11, off + 11 + dataSize);
    handleTag(tagType, timestamp, data);
    off += total;
    if (performance.now() > budgetEnd) break;
  }

  if (off > state.bufOff) {
    state.buffer = state.buffer.slice(off);
    state.bufOff = 0;
  }
}

var audioSeqInfo = null;

function handleTag(type, ts, data) {
  state.seq++;
  var now = performance.now();
  var delta = state.lastTs !== null ? ts - state.lastTs : null;
  state.lastTs = ts;

  var secKey = Math.floor((now - state.startTime) / 1000);
  var bucket = state.perSec.get(secKey);
  if (!bucket) { bucket = { video: 0, audio: 0, videoPkts: 0, audioPkts: 0, packets: 0, ts: secKey }; state.perSec.set(secKey, bucket); }
  bucket.packets++;

  var row = { seq: state.seq, type: type, size: data.length, ts: ts, delta: delta, detail: '' };

  if (type === 18) {
    state.scriptTags++;
    state.meta = parseOnMetaData(data);
    row.typeName = 'SCRIPT';
    row.frame = '—';
    row.detail = Object.keys(state.meta).slice(0, 4).map(function (k) { return k + '=' + state.meta[k]; }).join(' ');
  } else if (type === 9) {
    state.videoTags++;
    bucket.video += data.length;
    bucket.videoPkts++;
    var frameType = data[0] >> 4;
    var codecId = data[0] & 0x0f;
    var avcInfo = '';
    var naluType = null;
    if (codecId === 7 && data.length >= 5) {
      var avcPacketType = data[1];
      var cts = (data[2] << 16) | (data[3] << 8) | data[4];
      if (avcPacketType === 0) {
        var spsLen = (data[11] << 8) | data[12];
        var sps = data.subarray(13, 13 + spsLen);
        state.sps = parseSPS(sps);
        state.naluLenSize = (data[9] & 0x03) + 1;
        avcInfo = 'AVC seq header (SPS)';
        row.detail = 'SPS/PPS';
      } else if (avcPacketType === 1) {
        var nalLenSize = state.naluLenSize || 4;
        var nalOff = 5 + nalLenSize;
        naluType = data.length > nalOff ? data[nalOff] & 0x1f : null;
        var nalNames = { 1: '非IDR', 5: 'IDR', 6: 'SEI', 7: 'SPS', 8: 'PPS' };
        avcInfo = 'AVC NALU=' + naluType + '(' + (nalNames[naluType] || '其他') + ') cts=' + cts;
        row.detail = avcInfo;
      }
    } else {
      row.detail = 'codec=' + codecId;
    }
    if (frameType === 1) {
      state.keyframes++;
      if (state.lastKeyTs !== null) {
        var gap = ts - state.lastKeyTs;
        if (gap >= 200 && gap < 600000) {
          state.keyGaps.push(gap);
          if (state.keyGaps.length > 200) state.keyGaps.shift();
        }
      }
      state.lastKeyTs = ts;
      row.frame = 'KEY';
      row.detail = '🔴 ' + (row.detail || '關鍵影格');
    } else {
      row.frame = 'INTER';
    }
    row.typeName = 'VIDEO';
    if (state.videoLastTs !== null) {
      var vdelta = ts - state.videoLastTs;
      if (vdelta > 5000) row.detail = (row.detail ? row.detail + ' | ' : '') + '⚠ 間距 ' + (vdelta / 1000).toFixed(1) + 's';
    }
    state.videoLastTs = ts;
    state.packetSeries.push({ i: state.seq, size: data.length, key: frameType === 1 });
    if (state.packetSeries.length > 400) state.packetSeries.shift();

    state.videoBlocks = state.videoBlocks || [];
    state.videoBlocks.push({
      seq: state.seq, ts: ts, size: data.length,
      frame: frameType === 1 ? 'KEY' : 'INTER',
      codec: codecId === 7 ? 'H.264' : 'codec' + codecId,
      nalu: naluType,
      sps: state.sps ? state.sps.width + '×' + state.sps.height : null,
    });
    if (state.videoBlocks.length > 50) state.videoBlocks.shift();

    state.videoTsHistory.push({ ts: ts, wall: performance.now() });
    if (state.videoTsHistory.length > 600) state.videoTsHistory.shift();
  } else if (type === 8) {
    state.audioTags++;
    bucket.audio += data.length;
    bucket.audioPkts++;
    var soundFormat = data[0] >> 4;
    var soundRateIdx = (data[0] >> 2) & 0x03;
    var channels = (data[0] & 0x01) + 1;
    var rateNames = [5500, 11025, 22050, 44100];
    var rate = soundFormat === 10 ? null : rateNames[soundRateIdx];
    row.typeName = 'AUDIO';
    row.frame = ({ 0: 'LPCM', 2: 'MP3', 10: 'AAC', 14: 'MP3 8kHz', 15: 'AAC' })[soundFormat] || ('FMT' + soundFormat);

    var aacTypeName = '—', aot = null, srate = rate, ch = channels;
    if (soundFormat === 10 && data.length >= 2) {
      var aacPacketType = data[1];
      if (aacPacketType === 0) {
        audioSeqInfo = parseAudioSpecificConfig(data.subarray(2));
        aacTypeName = '序列頭 (ASC)';
        if (audioSeqInfo) {
          aot = audioSeqInfo.aot;
          srate = audioSeqInfo.sampleRate;
          ch = audioSeqInfo.channels;
          state.aacCfg = audioSeqInfo;
        }
        row.detail = 'AAC sequence header';
      } else if (aacPacketType === 1) {
        aacTypeName = 'RAW frame';
        aot = state.aacCfg ? state.aacCfg.aot : null;
        srate = state.aacCfg ? state.aacCfg.sampleRate : null;
        ch = state.aacCfg ? state.aacCfg.channels : null;
        row.detail = 'AAC frame ' + data.length + ' B';
      }
    } else {
      row.detail = 'format=' + soundFormat;
    }

    state.audioLastTs = ts;
    state.audioBlocks = state.audioBlocks || [];
    state.audioBlocks.push({
      seq: state.seq, ts: ts, size: data.length,
      aacType: aacTypeName, srate: srate, channels: ch, aot: aot,
    });
    if (state.audioBlocks.length > 50) state.audioBlocks.shift();

    if (state.videoLastTs !== null) {
      var offset = ts - state.videoLastTs;
      var lastSync = state.syncSeries[state.syncSeries.length - 1];
      if (!lastSync || state.seq - lastSync.i > 10) {
        state.syncSeries.push({ i: state.seq, offset: offset });
        if (state.syncSeries.length > 120) state.syncSeries.shift();
      }
    }
  } else {
    row.typeName = 'TYPE' + type;
    row.frame = '—';
    row.detail = '';
  }

  state.bytes += 11 + data.length + 4;
  state.pendingRows.push(row);
  if (state.pendingRows.length > 800) state.pendingRows.shift();

  state.rawCache.set(state.seq, data.slice(0, 262144));
  state.rawMeta.set(state.seq, { typeName: row.typeName, ts: ts });
  if (state.rawCache.size > 300) {
    state.rawCache.delete(state.rawCache.keys().next().value);
    state.rawMeta.delete(state.rawMeta.keys().next().value);
  }
  var rawBytes = 0;
  for (var b of state.rawCache.values()) rawBytes += b.length;
  while (rawBytes > 4 * 1024 * 1024 && state.rawCache.size > 1) {
    var k = state.rawCache.keys().next().value;
    rawBytes -= state.rawCache.get(k).length;
    state.rawCache.delete(k);
    state.rawMeta.delete(k);
  }
}

// ── 指標計算 ──
function median(arr) {
  if (!arr.length) return 0;
  var a = arr.slice().sort(function (x, y) { return x - y; });
  var m = Math.floor(a.length / 2);
  return a.length % 2 ? a[m] : (a[m - 1] + a[m]) / 2;
}

function measureFps() {
  var h = state.videoTsHistory;
  if (h.length < 10) return null;
  var wallSpan = h[h.length - 1].wall - h[0].wall;
  var tsSpan = h[h.length - 1].ts - h[0].ts;
  if (tsSpan <= 0 || wallSpan <= 0) return null;
  return (h.length - 1) / Math.max(tsSpan, 1) * 1000;
}

function recentMediaRates(kind) {
  var h = state.videoTsHistory.filter(function (x) { return x.ts >= state.videoLastTs - 3000; });
  if (h.length < 3) return { rate: 0, sent: 0, ts: 0 };
  var count = 0;
  var lastTs = null;
  for (var i = 0; i < h.length; i++) {
    if (lastTs !== null) {
      var gap = h[i].ts - lastTs;
      if (gap > 0) count += gap;
    }
    lastTs = h[i].ts;
  }
  var rate = count > 0 ? (h.length - 1) / count * 1000 : 0;
  return { rate: rate, sent: h.length - 1, ts: h[h.length - 1].ts };
}

function audioFrameRate() {
  if (!state.aacCfg) return 0;
  var divisor = (state.aacCfg.aot === 5 || state.aacCfg.aot === 29) ? 2048 : 1024;
  return state.aacCfg.sampleRate / divisor;
}

// ── 資訊卡 ──
function renderMetaCard() {
  var el = document.getElementById('metaInfo');
  if (!state.meta) { el.innerHTML = '<span class="text-dim">尚未擷取</span>'; return; }
  var rows = Object.keys(state.meta).map(function (k) {
    return '<div style="display:flex;justify-content:space-between;gap:12px"><span class="text-dim">' + esc(k) + '</span><span>' + esc(state.meta[k]) + '</span></div>';
  }).join('');
  el.innerHTML = rows;
}

function renderVideoCard() {
  var el = document.getElementById('videoInfo');
  if (!state.videoTags) { el.innerHTML = '<span class="text-dim">尚未擷取</span>'; return; }
  var rows = [];
  if (state.sps) {
    rows.push(infoRow('解析度', state.sps.width + ' × ' + state.sps.height));
    rows.push(infoRow('Profile', ({ 66: 'Baseline', 77: 'Main', 100: 'High' })[state.sps.profileIdc] || state.sps.profileIdc));
    rows.push(infoRow('Level', (state.sps.levelIdc / 10).toFixed(1)));
    if (state.sps.fps) rows.push(infoRow('FPS (VUI)', state.sps.fps.toFixed(2)));
  }
  var fps = measureFps();
  if (fps) rows.push(infoRow('量測 FPS', fps.toFixed(1)));
  rows.push(infoRow('關鍵影格', state.keyframes));
  if (state.keyGaps.length) {
    rows.push(infoRow('GOP 平均', (median(state.keyGaps) / 1000).toFixed(2) + 's'));
    rows.push(infoRow('GOP 最大', (Math.max.apply(null, state.keyGaps) / 1000).toFixed(2) + 's'));
  }
  el.innerHTML = rows.join('');
}

function renderAudioCard() {
  var el = document.getElementById('audioInfo');
  if (!state.audioTags) { el.innerHTML = '<span class="text-dim">尚未擷取</span>'; return; }
  var rows = [];
  if (state.aacCfg) {
    rows.push(infoRow('AOT', AOT_NAMES[state.aacCfg.aot] || state.aacCfg.aot));
    rows.push(infoRow('取樣率', state.aacCfg.sampleRate + ' Hz'));
    rows.push(infoRow('頻道', state.aacCfg.channels + ' ch'));
  } else {
    rows.push(infoRow('格式', '未偵測到 AAC 序列頭'));
  }
  el.innerHTML = rows.join('');
}

function infoRow(k, v) {
  return '<div style="display:flex;justify-content:space-between;gap:12px"><span class="text-dim">' + esc(k) + '</span><span>' + esc(v) + '</span></div>';
}

// ── 健康診斷 ──
function badge(cls, text) {
  return '<span class="badge ' + cls + '">' + text + '</span> ';
}

function renderHealth() {
  var el = document.getElementById('healthFlags');
  var elapsed = (performance.now() - state.startTime) / 1000;
  var flags = [];
  var videoOnly = state.videoTags > 0 && state.audioTags === 0;
  var audioOnly = state.audioTags > 0 && state.videoTags === 0;

  if (elapsed > 3) {
    if (state.videoTags === 0 && state.audioTags === 0) flags.push(badge('badge-err', '無媒體封包'));
    else if (videoOnly) flags.push(badge('badge-warn', '只有影片'));
    else if (audioOnly) flags.push(badge('badge-warn', '只有音訊'));
    else flags.push(badge('badge-ok', '影音正常'));
  }

  if (elapsed > 10) {
    if (!state.keyframes) flags.push(badge('badge-err', '10 秒無關鍵影格'));
    else {
      var maxGap = Math.max.apply(null, state.keyGaps);
      if (maxGap > 10000) flags.push(badge('badge-err', 'GOP 最大間隔 ' + (maxGap / 1000).toFixed(1) + 's'));
      else if (maxGap > 5000) flags.push(badge('badge-warn', 'GOP 間隔偏大 ' + (maxGap / 1000).toFixed(1) + 's'));
      else flags.push(badge('badge-ok', 'GOP 間隔正常'));
    }

    var kbps = state.bytes * 8 / 1024 / Math.max(elapsed, 0.001);
    if (kbps < 100) flags.push(badge('badge-warn', '位元率偏低 ' + kbps.toFixed(0) + ' kbps'));
    else if (kbps > 20000) flags.push(badge('badge-warn', '位元率偏高 ' + kbps.toFixed(0) + ' kbps'));
  }

  if (elapsed > 30) {
    var fps = measureFps();
    var recent = recentMediaRates();
    if (fps && state.sps && state.sps.fps) {
      var ratio = fps / state.sps.fps;
      if (ratio < 0.9) flags.push(badge('badge-err', '影片疑似掉幀 (量測 ' + fps.toFixed(1) + '/' + state.sps.fps.toFixed(1) + ')'));
    }
    if (recent.rate && state.aacCfg) {
      var afr = audioFrameRate();
      if (afr > 0 && recent.rate / afr < 0.9) flags.push(badge('badge-err', '音訊疑似掉幀'));
    }
    if (state.offsetSamples.length > 40) {
      var offsets = state.offsetSamples.map(function (o) { return o.offset; });
      var start = median(offsets.slice(0, 20));
      var end = median(offsets.slice(-20));
      var drift = end - start;
      if (drift > 800) flags.push(badge('badge-err', '音訊掉幀 (偏移 +' + drift.toFixed(0) + 'ms)'));
      else if (drift < -800) flags.push(badge('badge-err', '影片掉幀 (偏移 ' + drift.toFixed(0) + 'ms)'));
    }
  }

  if (state.videoLastTs !== null && state.audioLastTs !== null) {
    var sync = state.audioLastTs - state.videoLastTs;
    if (Math.abs(sync) > 3000) flags.push(badge('badge-warn', '影音偏移 ' + sync + 'ms'));
  }

  if (!flags.length) flags.push(badge('badge-ok', '正常'));
  el.innerHTML = flags.join('');
}

// ── UI 更新 ──
function updateBitrateBuckets() {
  var now = performance.now();
  var elapsedSec = (now - state.startTime) / 1000;
  var cur = Math.floor(elapsedSec);
  for (var s of state.perSec.keys()) {
    if (s < cur - 600) state.perSec.delete(s);
  }
  bitrateChart.data.labels = [];
  bitrateChart.data.datasets[0].data = [];
  bitrateChart.data.datasets[1].data = [];
  var windowStart = Math.max(0, cur - 60);
  for (var s2 = windowStart; s2 <= cur; s2++) {
    var b = state.perSec.get(s2);
    bitrateChart.data.labels.push(String(s2));
    bitrateChart.data.datasets[0].data.push(b ? +(b.video * 8 / 1024).toFixed(1) : 0);
    bitrateChart.data.datasets[1].data.push(b ? +(b.audio * 8 / 1024).toFixed(1) : 0);
  }
  bitrateChart.update('none');

  packetChart.data.datasets[0].data = state.packetSeries.filter(function (p) { return !p.key; }).map(function (p) { return { x: p.i, y: p.size }; });
  packetChart.data.datasets[1].data = state.packetSeries.filter(function (p) { return p.key; }).map(function (p) { return { x: p.i, y: p.size }; });
  packetChart.update('none');

  syncChart.data.labels = state.syncSeries.map(function (s) { return s.i; });
  syncChart.data.datasets[0].data = state.syncSeries.map(function (s) { return s.offset; });
  syncChart.update('none');

  gopChart.data.labels = state.keyGaps.map(function (_, i) { return i; });
  gopChart.data.datasets[0].data = state.keyGaps;
  gopChart.update('none');

  var kbps = state.bytes * 8 / 1024 / Math.max(elapsedSec, 0.001);
  document.getElementById('statBytes').textContent = fmtBytes(state.bytes);
  document.getElementById('statBytesSub').textContent = '歷時 ' + elapsedSec.toFixed(1) + 's';
  document.getElementById('statBitrate').textContent = kbps.toFixed(0) + ' kbps';
  document.getElementById('statBitrateSub').textContent = '即時 ' + (state.videoTags + state.audioTags) + ' pkts';
  document.getElementById('statTags').textContent = fmtNum(state.videoTags);
  document.getElementById('statTagsSub').textContent = 'video ' + fmtNum(state.videoTags) + ' / audio ' + fmtNum(state.audioTags);

  renderMetaCard();
  renderVideoCard();
  renderAudioCard();
  renderHealth();
}

var rowColor = { VIDEO: 'text-info', AUDIO: 'text-ok', SCRIPT: 'text-warn' };

function flushPacketRows() {
  if (!state.pendingRows.length || state.paused) return;
  var tbody = document.getElementById('pktTable');
  var frag = document.createDocumentFragment();
  for (var row of state.pendingRows) {
    var tr = document.createElement('tr');
    tr.className = rowColor[row.typeName] || 'text-dim';
    tr.innerHTML = '<td>' + row.seq + '</td><td>' + row.typeName + '</td><td>' + row.size + '</td><td>' + (row.frame === 'KEY' ? '<span class="text-err">KEY</span>' : row.frame) + '</td><td>' + row.ts + '</td><td>' + (row.delta !== null ? row.delta : '') + '</td><td>' + esc(row.detail) + '</td><td><button data-seq="' + row.seq + '" class="btn btn-secondary btn-sm viewBtn">🔍</button></td>';
    frag.appendChild(tr);
  }
  tbody.appendChild(frag);
  while (tbody.children.length > 500) tbody.removeChild(tbody.firstChild);
  tbody.parentElement.scrollTop = tbody.parentElement.scrollHeight;
  state.pendingRows = [];
  tbody.querySelectorAll('.viewBtn').forEach(function (btn) {
    btn.addEventListener('click', function () { openRawView(Number(btn.dataset.seq)); });
  });
}

function renderVideoBlocks() {
  var tbody = document.getElementById('videoTable');
  var rows = state.videoBlocks.map(function (b) {
    return '<tr><td>' + b.seq + '</td><td>' + b.ts + '</td><td>' + b.size + '</td><td>' + (b.frame === 'KEY' ? '<span class="text-err">KEY</span>' : 'INTER') + '</td><td>' + b.codec + '</td><td>' + (b.nalu != null ? b.nalu : '') + '</td><td>' + (b.sps || '') + '</td></tr>';
  }).join('');
  tbody.innerHTML = rows;
}

function renderAudioBlocks() {
  var tbody = document.getElementById('audioTable');
  var rows = state.audioBlocks.map(function (b) {
    return '<tr><td>' + b.seq + '</td><td>' + b.ts + '</td><td>' + b.size + '</td><td>' + b.aacType + '</td><td>' + (b.srate || '') + '</td><td>' + (b.channels || '') + '</td><td>' + (b.aot || '') + '</td></tr>';
  }).join('');
  tbody.innerHTML = rows;
}

// ── 原文檢視 ──
var rawViewTab = 'hex';

function hexDump(data) {
  var lines = [];
  for (var i = 0; i < data.length; i += 16) {
    var slice = data.subarray(i, i + 16);
    var hex = Array.from(slice).map(function (b) { return b.toString(16).padStart(2, '0'); }).join(' ');
    var ascii = Array.from(slice).map(function (b) { return (b >= 32 && b < 127 ? String.fromCharCode(b) : '.'); }).join('');
    lines.push(i.toString(16).padStart(8, '0') + '  ' + hex.padEnd(47, ' ') + '  |' + ascii + '|');
  }
  return lines.join('\n');
}

function utf8Readable(data) {
  var out = '';
  var i = 0;
  var n = data.length;
  while (i < n) {
    var b = data[i];
    if (b === 0x0a || b === 0x0d || b === 0x09 || (b >= 32 && b <= 126)) {
      out += String.fromCharCode(b);
      i++;
      continue;
    }
    var len = 0, valid = true;
    if (b >= 0xc2 && b <= 0xdf) len = 2;
    else if (b >= 0xe0 && b <= 0xef) {
      len = 3;
      if (b === 0xe0) valid = data[i + 1] >= 0xa0;
      else if (b === 0xed) valid = data[i + 1] <= 0x9f;
    }
    else if (b >= 0xf0 && b <= 0xf4) {
      len = 4;
      if (b === 0xf0) valid = data[i + 1] >= 0x90;
      else if (b === 0xf4) valid = data[i + 1] <= 0x8f;
    }
    if (len > 1 && valid) {
      for (var j = 1; j < len; j++) {
        if (i + j >= n) { valid = false; break; }
        var cb = data[i + j];
        if (cb < 0x80 || cb > 0xbf) { valid = false; break; }
      }
      if (valid) {
        out += new TextDecoder('utf-8').decode(data.subarray(i, i + len));
        i += len;
        continue;
      }
    }
    out += '·';
    i++;
  }
  return out;
}

function openRawView(seq) {
  var data = state.rawCache.get(seq);
  var modal = document.getElementById('rawModal');
  var title = document.getElementById('rawModalTitle');
  var hex = document.getElementById('rawModalHex');
  var text = document.getElementById('rawModalText');
  var parsed = document.getElementById('rawModalParsed');
  var meta = document.getElementById('rawModalMeta');
  if (!data) {
    title.textContent = '序號 ' + seq + '：原始資料已過期（僅保留最近 300 個封包）';
    hex.textContent = '';
    text.textContent = '';
    parsed.innerHTML = '';
    meta.textContent = '';
  } else {
    title.textContent = '序號 ' + seq + ' — 原始資料 ' + data.length + ' bytes';
    hex.textContent = hexDump(data);
    text.textContent = utf8Readable(data);
    parsed.innerHTML = parseTagStructure(seq, data);
    var rm = state.rawMeta.get(seq);
    meta.textContent = '型別 ' + (rm ? rm.typeName : '?') + ' | timestamp ' + (rm ? rm.ts : '?') + ' | 前 16 bytes 為 tag payload 內容（不含 FLV 檔頭）';
  }
  setRawTab(rawViewTab);
  modal.classList.add('open');
}

function parseTagStructure(seq, data) {
  var rm = state.rawMeta.get(seq);
  var type = rm ? rm.typeName : '?';
  var box = function (title, rows) {
    return '<div class="parse-box"><div class="text-dim" style="font-size:12px;margin-bottom:4px">' + title + '</div>' + rows.join('') + '</div>';
  };
  var row = function (k, v, cls) {
    return '<div class="parse-row"><span class="parse-key">' + esc(k) + '</span><span class="parse-val ' + (cls || '') + '">' + v + '</span></div>';
  };

  if (type === 'VIDEO') {
    var rows = [];
    var frameType = data[0] >> 4;
    var codecId = data[0] & 0x0f;
    var frameNames = { 1: '關鍵影格 (Key)', 2: '中間影格 (Inter)', 3: '可丟棄中間影格', 4: '產生的影格' };
    rows.push(row('幀型別', frameType + ' — ' + (frameNames[frameType] || '未知'), frameType === 1 ? 'text-err' : ''));
    rows.push(row('編碼 ID', codecId === 7 ? '7 — H.264 (AVC)' : codecId === 12 ? '12 — HEVC (H.265)' : codecId + ' — 未知', codecId === 7 ? 'text-ok' : ''));

    if (codecId === 7 && data.length >= 5) {
      var avcPacketType = data[1];
      var cts = (data[2] << 16) | (data[3] << 8) | data[4];
      var ctsSigned = cts > 0x7fffff ? cts - 0x1000000 : cts;
      rows.push(row('AVC 封包型別', avcPacketType === 0 ? '0 — 序列頭 (SPS/PPS)' : avcPacketType === 1 ? '1 — NALU' : avcPacketType + ' — 未知'));
      rows.push(row('合成時間 (cts)', ctsSigned + ' ms', ctsSigned !== 0 ? 'text-warn' : ''));

      if (avcPacketType === 0) {
        rows.push(row('組態版本', String(data[5])));
        rows.push(row('Profile', data[6] + ' (' + (({ 66: 'Baseline', 77: 'Main', 100: 'High', 110: 'High 10' })[data[6]] || '未知') + ')'));
        rows.push(row('Level', (data[8] / 10).toFixed(1)));
        rows.push(row('NAL 長度大小', ((data[9] & 0x03) + 1) + ' bytes'));
        var numSps = data[10] & 0x1f;
        var spsLen = (data[11] << 8) | data[12];
        rows.push(row('SPS 數量 / 長度', numSps + ' / ' + spsLen + ' bytes'));
        if (data.length >= 13 + spsLen) {
          var sps = parseSPS(data.subarray(13, 13 + spsLen));
          if (sps) {
            rows.push(row('→ 解析度', sps.width + ' × ' + sps.height, 'text-info'));
            rows.push(row('→ Profile/Level', (({ 66: 'Baseline', 77: 'Main', 100: 'High' })[sps.profileIdc] || sps.profileIdc) + ' / ' + (sps.levelIdc / 10).toFixed(1)));
            rows.push(row('→ FPS (VUI)', sps.fps ? sps.fps.toFixed(2) : '無 (由量測估算)'));
          }
        }
      } else if (avcPacketType === 1) {
        var off = 5;
        var nals = [];
        var naluLenSize = state.naluLenSize || 4;
        while (off + naluLenSize <= data.length) {
          var len = 0;
          for (var li = 0; li < naluLenSize; li++) len = (len << 8) | data[off + li];
          off += naluLenSize;
          if (off + len > data.length) break;
          var nt = data[off] & 0x1f;
          var ntNames = { 1: 'Slice 非IDR', 5: 'IDR', 6: 'SEI', 7: 'SPS', 8: 'PPS', 9: 'AUD', 24: 'Stap-A' };
          nals.push({ type: nt, name: ntNames[nt] || '其他', len: len });
          off += len;
          if (nals.length > 8) break;
        }
        rows.push(row('NAL 單位', nals.map(function (n) { return n.name + '(' + n.type + ')' + n.len + 'B'; }).join(' + ') || '無法解析'));
        var counts = {};
        for (var ni = 0; ni < nals.length; ni++) counts[nals[ni].name] = (counts[nals[ni].name] || 0) + 1;
        rows.push(row('NAL 統計', Object.keys(counts).map(function (k) { return k + '×' + counts[k]; }).join(' ')));
      }
    }
    return box('影片 Tag 解析', rows);
  }

  if (type === 'AUDIO') {
    var rows2 = [];
    var soundFormat = data[0] >> 4;
    var soundRate = (data[0] >> 2) & 0x03;
    var soundSize = (data[0] >> 1) & 0x01;
    var soundType = data[0] & 0x01;
    var fmtNames = { 0: 'LPCM', 2: 'MP3', 10: 'AAC', 14: 'MP3 8kHz', 15: 'AAC (無位元率)' };
    rows2.push(row('音訊格式', soundFormat + ' — ' + (fmtNames[soundFormat] || '未知'), soundFormat === 10 ? 'text-ok' : ''));
    rows2.push(row('取樣率索引', soundRate + ' — ' + [5500, 11025, 22050, 44100][soundRate] + ' Hz'));
    rows2.push(row('位元深度', soundSize ? '16-bit' : '8-bit'));
    rows2.push(row('頻道', soundType ? '立體聲 (stereo)' : '單聲道 (mono)'));

    if (soundFormat === 10 && data.length >= 2) {
      var aacPacketType2 = data[1];
      rows2.push(row('AAC 封包型別', aacPacketType2 === 0 ? '0 — 序列頭 (AudioSpecificConfig)' : aacPacketType2 === 1 ? '1 — RAW AAC frame' : aacPacketType2 + ' — 未知'));
      if (aacPacketType2 === 0) {
        var asc = parseAudioSpecificConfig(data.subarray(2));
        if (asc) {
          rows2.push(row('→ AOT', asc.aot + ' — ' + (AOT_NAMES[asc.aot] || '未知'), 'text-info'));
          rows2.push(row('→ 取樣率', asc.sampleRate + ' Hz'));
          rows2.push(row('→ 頻道', asc.channels + ' ch'));
        }
      } else if (aacPacketType2 === 1) {
        rows2.push(row('AAC frame 大小', (data.length - 2) + ' bytes', 'text-info'));
        rows2.push(row('推估取樣數', '~' + Math.round((data.length - 2) / 2) + ' samples/ch (AAC LC @44.1k 約 1024)'));
      }
    }
    return box('音訊 Tag 解析', rows2);
  }

  if (type === 'SCRIPT') {
    var meta = parseOnMetaData(data);
    var keys = Object.keys(meta);
    if (keys.length) {
      var rows3 = keys.map(function (k) { return row(k, typeof meta[k] === 'object' ? JSON.stringify(meta[k]) : String(meta[k])); });
      return box('onMetaData 解析', rows3);
    }
    return box('SCRIPT Tag', [row('內容', '無法解析為 AMF0 metadata，請看 UTF-8 / Hex 分頁')]);
  }

  return box('Tag 型別 ' + type, [row('內容', '無結構化解析，請看 Hex / UTF-8 分頁')]);
}

function setRawTab(tab) {
  rawViewTab = tab;
  var hex = document.getElementById('rawModalHex');
  var text = document.getElementById('rawModalText');
  var parsed = document.getElementById('rawModalParsed');
  var tabHex = document.getElementById('rawTabHex');
  var tabText = document.getElementById('rawTabText');
  var tabParsed = document.getElementById('rawTabParsed');
  tabHex.classList.remove('active');
  tabText.classList.remove('active');
  tabParsed.classList.remove('active');
  if (tab === 'text') {
    hex.classList.add('hidden'); text.classList.remove('hidden'); parsed.classList.add('hidden');
    tabText.classList.add('active');
  } else if (tab === 'parsed') {
    hex.classList.add('hidden'); text.classList.add('hidden'); parsed.classList.remove('hidden');
    tabParsed.classList.add('active');
  } else {
    hex.classList.remove('hidden'); text.classList.add('hidden'); parsed.classList.add('hidden');
    tabHex.classList.add('active');
  }
}

function closeRawView() {
  document.getElementById('rawModal').classList.remove('open');
}

// ── 即時預覽 ──
var previewPlayer = null;

function startPreview(url) {
  var video = document.getElementById('previewVideo');
  document.getElementById('previewOverlay').style.display = 'none';
  document.getElementById('previewStatus').textContent = '播放中';
  document.getElementById('previewStatus').className = 'text-ok';
  if (previewPlayer) stopPreview();
  video.classList.remove('hidden');
  if (typeof mpegts !== 'undefined' && mpegts.isSupported()) {
    previewPlayer = mpegts.createPlayer({ type: 'flv', isLive: true, url: url }, { enableStashBuffer: false });
    previewPlayer.attachMediaElement(video);
    previewPlayer.load();
    previewPlayer.play();
  } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
    video.src = url;
    video.play();
  }
}

function stopPreview() {
  if (previewPlayer) {
    try { previewPlayer.destroy(); } catch (e) {}
    previewPlayer = null;
  }
  var video = document.getElementById('previewVideo');
  video.pause();
  video.removeAttribute('src');
  video.load();
  document.getElementById('previewStatus').textContent = '已停止';
  document.getElementById('previewStatus').className = 'text-dim';
}

function checkPreviewStall() {
  if (!previewPlayer || !state.running) return;
  var video = document.getElementById('previewVideo');
  if (!video) return;
  if (document.getElementById('hudEnabled').checked) updatePreviewHud();
}

function updatePreviewHud() {
  if (!state.running) return;
  var fps = measureFps();
  var sps = state.sps;
  var kbps = state.bytes * 8 / 1024 / Math.max((performance.now() - state.startTime) / 1000, 0.001);
  var gop = state.keyGaps.length ? (median(state.keyGaps) / 1000).toFixed(2) + 's' : '—';
  var sync = (state.videoLastTs !== null && state.audioLastTs !== null) ? (state.audioLastTs - state.videoLastTs) + 'ms' : '—';
  document.getElementById('hudFps').textContent = fps ? fps.toFixed(1) : '—';
  document.getElementById('hudRes').textContent = sps ? sps.width + '×' + sps.height : '—';
  document.getElementById('hudBitrate').textContent = kbps.toFixed(0) + 'k';
  document.getElementById('hudGop').textContent = gop;
  document.getElementById('hudPkts').textContent = state.videoTags + 'v/' + state.audioTags + 'a';
  document.getElementById('hudSync').textContent = sync;
}

// ── 分析流程 ──
async function startAnalysis() {
  var host = document.getElementById('hostInput').value.trim().replace(/\/+$/, '');
  var stream = document.getElementById('streamInput').value.trim();
  var url = host + '/live/' + stream + '.flv';
  document.getElementById('flvUrl').textContent = url;

  stopAnalysis();
  resetState();

  document.getElementById('startBtn').classList.add('hidden');
  document.getElementById('stopBtn').classList.remove('hidden');
  var cs = document.getElementById('connStatus');
  cs.textContent = '連線中…';
  cs.className = 'stat-value text-warn';
  document.getElementById('connDetail').textContent = url;

  if (document.getElementById('previewEnabled').checked) {
    startPreview(url);
  }

  state.running = true;
  state.controller = new AbortController();
  state.startTime = performance.now();

  try {
    var res = await fetch(url, { signal: state.controller.signal });
    if (!res.ok) {
      cs.textContent = 'HTTP ' + res.status;
      cs.className = 'stat-value text-err';
      document.getElementById('connDetail').textContent = res.statusText || '';
      stopAnalysis();
      return;
    }
    cs.textContent = '✓ 已連線';
    cs.className = 'stat-value text-ok';
    document.getElementById('connDetail').textContent = res.headers.get('content-type') || '';

    var reader = res.body.getReader();
    while (state.running) {
      var chunk = await reader.read();
      if (chunk.done) break;
      var value = chunk.value;
      if (state.buffer.length === 0) {
        state.buffer = value;
      } else {
        var merged = new Uint8Array(state.buffer.length + value.length);
        merged.set(state.buffer);
        merged.set(value, state.buffer.length);
        state.buffer = merged;
      }
      try { parseTags(); } catch (e) { console.warn('parseTags error:', e); }
      if (state.buffer.length > 16 * 1024 * 1024) {
        console.warn('buffer overflow, dropping to resync');
        state.buffer = state.buffer.slice(-(1 << 16));
        state.bufOff = 0;
        state.headerOk = false;
      }
    }
    if (state.running) {
      cs.textContent = '串流結束';
      cs.className = 'stat-value text-dim';
    }
  } catch (err) {
    if (err.name === 'AbortError') return;
    cs.textContent = '連線失敗';
    cs.className = 'stat-value text-err';
    document.getElementById('connDetail').textContent = err.message;
  } finally {
    stopAnalysis();
  }
}

function stopAnalysis() {
  state.running = false;
  if (state.controller) state.controller.abort();
  state.controller = null;
  stopPreview();
  document.getElementById('startBtn').classList.remove('hidden');
  document.getElementById('stopBtn').classList.add('hidden');
}

function resetState() {
  state.buffer = new Uint8Array(0);
  state.bufOff = 0;
  state.headerOk = false;
  state.headerErr = false;
  state.bytes = 0;
  state.seq = 0;
  state.videoTags = 0;
  state.audioTags = 0;
  state.scriptTags = 0;
  state.keyframes = 0;
  state.lastKeyTs = null;
  state.keyGaps = [];
  state.lastTs = null;
  state.videoLastTs = null;
  state.audioLastTs = null;
  state.meta = null;
  state.sps = null;
  state.naluLenSize = 4;
  state.aacCfg = null;
  audioSeqInfo = null;
  state.perSec = new Map();
  state.packetSeries = [];
  state.syncSeries = [];
  state.audioBlocks = [];
  state.videoBlocks = [];
  state.videoTsHistory = [];
  state.fpsSamples = [];
  state.offsetSamples = [];
  state._lastVct = null;
  state._stallCount = 0;
  state.pendingRows = [];
  state.rawCache = new Map();
  state.rawMeta = new Map();
  document.getElementById('pktTable').innerHTML = '';
  document.getElementById('audioTable').innerHTML = '';
  document.getElementById('videoTable').innerHTML = '';
  document.getElementById('healthFlags').innerHTML = '<span class="text-dim">尚未開始分析</span>';
  document.getElementById('metaInfo').innerHTML = '<span class="text-dim">尚未擷取</span>';
  document.getElementById('videoInfo').innerHTML = '<span class="text-dim">尚未擷取</span>';
  document.getElementById('audioInfo').innerHTML = '<span class="text-dim">尚未擷取</span>';
  document.getElementById('statBytes').textContent = '0';
  document.getElementById('statBytesSub').textContent = '';
  document.getElementById('statBitrate').textContent = '0';
  document.getElementById('statBitrateSub').textContent = '';
  document.getElementById('statTags').textContent = '0';
  document.getElementById('statTagsSub').textContent = '';
  var cs = document.getElementById('connStatus');
  cs.textContent = '未連線';
  cs.className = 'stat-value text-dim';
  document.getElementById('connDetail').textContent = '';
  bitrateChart && bitrateChart.data.datasets.forEach(function (d) { d.data = []; });
  packetChart && packetChart.data.datasets.forEach(function (d) { d.data = []; });
  syncChart && syncChart.data.datasets.forEach(function (d) { d.data = []; });
  gopChart && gopChart.data.datasets.forEach(function (d) { d.data = []; });
  bitrateChart && bitrateChart.update('none');
  packetChart && packetChart.update('none');
  syncChart && syncChart.update('none');
  gopChart && gopChart.update('none');
}

// ── stream key 保護 ──
var streamRevealed = false;

function maskStreamKey(key) {
  if (!key) return '';
  if (key.length <= 2) return '*'.repeat(key.length);
  var head = Math.min(2, key.length);
  var tail = Math.min(2, key.length - head);
  return key.slice(0, head) + '*'.repeat(Math.max(0, key.length - head - tail)) + key.slice(key.length - tail);
}

function getFlvUrl() {
  var host = document.getElementById('hostInput').value.trim().replace(/\/+$/, '');
  return host + '/live/' + getStreamKey() + '.flv';
}

function getStreamKey() {
  return document.getElementById('streamInput').value.trim();
}

function renderStreamVisibility() {
  var stream = getStreamKey();
  var masked = maskStreamKey(stream);
  document.getElementById('streamInput').type = streamRevealed ? 'text' : 'password';
  document.getElementById('streamEye').textContent = streamRevealed ? '隱藏' : '顯示';
  var host = document.getElementById('hostInput').value.trim().replace(/\/+$/, '');
  document.getElementById('connSummary').textContent = streamRevealed ? host + '/live/' + stream + '.flv' : host + '/live/' + masked + '.flv';
  var flvEl = document.getElementById('flvUrl');
  flvEl.textContent = streamRevealed ? getFlvUrl() : host + '/live/' + masked + '.flv';
}

function toggleStreamReveal() {
  streamRevealed = !streamRevealed;
  renderStreamVisibility();
}

/* ══════════════ WebRTC 分析 ══════════════ */

var rtcState = {
  running: false,
  connected: false,
  sdk: null,
  prevStats: null,
  prevTime: 0,
  prevVFrame: null,
  rttSeries: [],
  bitrateSeries: [],
  jitterSeries: [],
  vStats: null,
  aStats: null,
};

var rtcRttChart, rtcBitrateChart, rtcJitterChart;

function buildRtcCharts() {
  rtcRttChart = new Chart(document.getElementById('rtcRttChart'), {
    type: 'line',
    data: { labels: [], datasets: [
      { label: 'RTT', data: [], borderColor: 'rgb(192,132,252)', backgroundColor: 'rgb(192,132,252)', borderWidth: 1.5, tension: 0.3, pointRadius: 0 },
    ]},
    options: { ...chartCommon },
  });
  rtcBitrateChart = new Chart(document.getElementById('rtcBitrateChart'), {
    type: 'line',
    data: { labels: [], datasets: [
      { label: '影片', data: [], borderColor: 'rgb(192,132,252)', backgroundColor: 'rgb(192,132,252)', borderWidth: 1.5, tension: 0.3, pointRadius: 0 },
      { label: '音訊', data: [], borderColor: 'rgb(34,211,238)', backgroundColor: 'rgb(34,211,238)', borderWidth: 1.5, tension: 0.3, pointRadius: 0 },
    ]},
    options: { ...chartCommon },
  });
  rtcJitterChart = new Chart(document.getElementById('rtcJitterChart'), {
    type: 'line',
    data: { labels: [], datasets: [
      { label: 'jitter', data: [], borderColor: 'rgb(52,211,153)', backgroundColor: 'rgb(52,211,153)', borderWidth: 1.5, tension: 0.3, pointRadius: 0 },
    ]},
    options: { ...chartCommon },
  });
}

async function pollRtcStats() {
  if (!rtcState.running || !rtcState.sdk || !rtcState.sdk.pc) return;
  try {
    var stats = await rtcState.sdk.pc.getStats();
    var v = null, a = null, cp = null, transport = null, localCand = null, remoteCand = null;
    var candidates = {};
    var codecs = {};
    stats.forEach(function (r) {
      if (r.type === 'inbound-rtp') {
        if (r.kind === 'video' && !v) v = r;
        else if (r.kind === 'audio' && !a) a = r;
      } else if (r.type === 'candidate-pair' && r.nominated && r.state === 'succeeded') {
        cp = r;
      } else if (r.type === 'transport') {
        transport = r;
      } else if (r.type === 'local-candidate') {
        candidates[r.id] = r;
      } else if (r.type === 'remote-candidate') {
        candidates[r.id] = r;
      } else if (r.type === 'codec') {
        codecs[r.id] = r;
      }
    });
    if (cp && candidates[cp.localCandidateId]) localCand = candidates[cp.localCandidateId];
    if (cp && candidates[cp.remoteCandidateId]) remoteCand = candidates[cp.remoteCandidateId];

    var now = performance.now();
    var dt = (now - rtcState.prevTime) / 1000;
    rtcState.prevTime = now;
    var prev = rtcState.prevStats || {};
    rtcState.prevStats = { vBytes: v ? v.bytesReceived : 0, aBytes: a ? a.bytesReceived : 0 };

    var rtt = cp && cp.currentRoundTripTime ? cp.currentRoundTripTime * 1000 : null;
    if (rtt !== null) {
      rtcState.rttSeries.push(rtt);
      if (rtcState.rttSeries.length > 120) rtcState.rttSeries.shift();
    }

    var vBitrate = v && prev && dt > 0 ? (v.bytesReceived - prev.vBytes) * 8 / 1024 / dt : 0;
    var aBitrate = a && prev && dt > 0 ? (a.bytesReceived - prev.aBytes) * 8 / 1024 / dt : 0;
    if (dt > 0) {
      rtcState.bitrateSeries.push({ v: Math.max(0, vBitrate), a: Math.max(0, aBitrate) });
      if (rtcState.bitrateSeries.length > 120) rtcState.bitrateSeries.shift();
    }

    var jitter = v && v.jitter ? v.jitter * 1000 : null;
    if (jitter !== null) {
      rtcState.jitterSeries.push(jitter);
      if (rtcState.jitterSeries.length > 120) rtcState.jitterSeries.shift();
    }

    renderRtcCharts();
    var dropRate = null;
    if (v && rtcState.prevVFrame) {
      var dDec = v.framesDecoded - rtcState.prevVFrame.decoded;
      var dDrop = v.framesDropped - rtcState.prevVFrame.dropped;
      if (dDec + dDrop > 0) dropRate = dDrop / (dDec + dDrop) * 100;
    }
    rtcState.prevVFrame = { decoded: v ? v.framesDecoded : 0, dropped: v ? v.framesDropped : 0 };
    renderRtcStats(v, a, cp, transport, rtt, vBitrate, aBitrate, localCand, remoteCand, codecs, dropRate);
    renderRtcHealth(v, a, cp, rtt, dropRate);
  } catch (e) {
    console.warn('getStats error:', e);
  }
}

function renderRtcCharts() {
  rtcRttChart.data.labels = rtcState.rttSeries.map(function (_, i) { return i; });
  rtcRttChart.data.datasets[0].data = rtcState.rttSeries;
  rtcRttChart.update('none');

  rtcBitrateChart.data.labels = rtcState.bitrateSeries.map(function (_, i) { return i; });
  rtcBitrateChart.data.datasets[0].data = rtcState.bitrateSeries.map(function (b) { return b.v; });
  rtcBitrateChart.data.datasets[1].data = rtcState.bitrateSeries.map(function (b) { return b.a; });
  rtcBitrateChart.update('none');

  rtcJitterChart.data.labels = rtcState.jitterSeries.map(function (_, i) { return i; });
  rtcJitterChart.data.datasets[0].data = rtcState.jitterSeries;
  rtcJitterChart.update('none');
}

function renderRtcStats(v, a, cp, transport, rtt, vBitrate, aBitrate, localCand, remoteCand, codecs, dropRate) {
  var conn = document.getElementById('rtcConnState');
  if (rtcState.connected) {
    conn.textContent = transport ? (transport.iceState || '—') : '—';
    conn.className = 'stat-value ' + (transport && transport.iceState === 'connected' ? 'text-ok' : 'text-warn');
  }

  var candTypeNames = { host: 'host', srflx: 'srflx', prflx: 'prflx', relay: 'relay' };
  var localType = localCand ? candTypeNames[localCand.candidateType] || localCand.candidateType : null;
  var remoteType = remoteCand ? candTypeNames[remoteCand.candidateType] || remoteCand.candidateType : null;
  document.getElementById('rtcIceCandidate').textContent = localType && remoteType ? localType + ' ⇄ ' + remoteType : (localType || remoteType || '—');
  document.getElementById('rtcIcePair').textContent = (localCand && remoteCand)
    ? (localCand.protocol || '') + ' ' + (remoteCand.address || remoteCand.ip || '') + ':' + (remoteCand.port || '')
    : '';

  document.getElementById('rtcRtt').textContent = rtt !== null ? rtt.toFixed(0) : '—';
  document.getElementById('rtcRttSub').textContent = cp ? 'via ' + (cp.nominated ? 'nominated' : '') + ' pair' : '';

  var lossV = v && v.packetsLost != null ? v.packetsLost : 0;
  var pktsV = v ? v.packetsReceived : 0;
  var lossRate = pktsV > 0 ? (lossV / (pktsV + lossV) * 100) : 0;
  document.getElementById('rtcLoss').textContent = lossRate.toFixed(2) + '%';
  document.getElementById('rtcLossSub').textContent = lossV + ' / ' + pktsV + ' pkts';

  document.getElementById('rtcBitrate').textContent = vBitrate.toFixed(0) + ' kbps';
  document.getElementById('rtcBitrateSub').textContent = 'audio ' + aBitrate.toFixed(0) + ' kbps';

  var res = v ? ((v.frameWidth || '?') + '×' + (v.frameHeight || '?')) : '—';
  var fps = v && v.framesPerSecond ? v.framesPerSecond.toFixed(0) : '—';
  document.getElementById('rtcRes').textContent = res;
  document.getElementById('rtcResSub').textContent = fps + ' fps' + (v && v.framesDecoded != null ? ' | decoded ' + v.framesDecoded : '');

  var tbody = document.getElementById('rtcStatsTable');
  var rows = [];
  var codecName = function (id) {
    var c = codecs && codecs[id];
    return c ? (c.mimeType || '—') : (id || '—');
  };
  if (v) {
    var dropCell = dropRate !== null ? '<span class="' + (dropRate > 5 ? 'text-err' : (dropRate > 1 ? 'text-warn' : 'text-ok')) + '">' + dropRate.toFixed(1) + '%</span>' : '—';
    rows.push('<tr><td>video</td><td>' + v.packetsReceived + '</td><td>' + v.packetsLost + '</td><td>' + lossRate.toFixed(2) + '%</td><td>' + (v.jitter ? (v.jitter * 1000).toFixed(1) : '—') + '</td><td>' + fmtBytes(v.bytesReceived) + '</td><td>' + (v.framesDecoded != null ? v.framesDecoded : '—') + '</td><td>' + (v.framesDropped != null ? v.framesDropped : '—') + '</td><td>' + dropCell + '</td><td>' + res + '</td><td>' + codecName(v.codecId) + '</td></tr>');
  }
  if (a) {
    var aLoss = a.packetsLost || 0;
    var aPkts = a.packetsReceived || 0;
    var aLossRate = aPkts > 0 ? (aLoss / (aPkts + aLoss) * 100) : 0;
    rows.push('<tr><td>audio</td><td>' + a.packetsReceived + '</td><td>' + a.packetsLost + '</td><td>' + aLossRate.toFixed(2) + '%</td><td>' + (a.jitter ? (a.jitter * 1000).toFixed(1) : '—') + '</td><td>' + fmtBytes(a.bytesReceived) + '</td><td>—</td><td>—</td><td>—</td><td>—</td><td>' + codecName(a.codecId) + '</td></tr>');
  }
  if (!v && !a) rows.push('<tr><td colspan="11" class="text-dim">等待接收媒體…</td></tr>');
  tbody.innerHTML = rows.join('');

  // HUD
  document.getElementById('rtcHudFps').textContent = fps;
  document.getElementById('rtcHudRes').textContent = res;
  document.getElementById('rtcHudBitrate').textContent = vBitrate.toFixed(0) + 'k';
  document.getElementById('rtcHudLoss').textContent = lossRate.toFixed(1) + '%';
  document.getElementById('rtcHudJitter').textContent = v && v.jitter ? (v.jitter * 1000).toFixed(1) + 'ms' : '—';
  document.getElementById('rtcHudRtt').textContent = rtt !== null ? rtt.toFixed(0) + 'ms' : '—';
}

function renderRtcHealth(v, a, cp, rtt, dropRate) {
  var el = document.getElementById('rtcHealthFlags');
  var flags = [];

  var conn = document.getElementById('rtcConnState').textContent;
  if (conn === 'connected' || conn === 'connected') flags.push(badge('badge-ok', '已連線'));
  else if (conn === 'checking' || conn === 'connecting') flags.push(badge('badge-warn', 'ICE 檢查中'));
  else if (conn !== '—') flags.push(badge('badge-err', 'ICE ' + conn));

  if (rtt !== null) {
    if (rtt > 500) flags.push(badge('badge-err', 'RTT 過高 ' + rtt.toFixed(0) + 'ms'));
    else if (rtt > 200) flags.push(badge('badge-warn', 'RTT 偏高 ' + rtt.toFixed(0) + 'ms'));
    else flags.push(badge('badge-ok', 'RTT 正常 ' + rtt.toFixed(0) + 'ms'));
  }

  if (v) {
    var loss = v.packetsLost || 0;
    var pkts = v.packetsReceived || 0;
    var rate = pkts > 0 ? (loss / (pkts + loss) * 100) : 0;
    if (rate > 5) flags.push(badge('badge-err', '封包遺失 ' + rate.toFixed(1) + '%'));
    else if (rate > 1) flags.push(badge('badge-warn', '封包遺失 ' + rate.toFixed(1) + '%'));
    else flags.push(badge('badge-ok', '封包遺失正常'));

    if (v.jitter) {
      var j = v.jitter * 1000;
      if (j > 100) flags.push(badge('badge-err', 'jitter 過高 ' + j.toFixed(0) + 'ms'));
      else if (j > 30) flags.push(badge('badge-warn', 'jitter 偏高 ' + j.toFixed(0) + 'ms'));
      else flags.push(badge('badge-ok', 'jitter 正常'));
    }

    if (dropRate !== null) {
      if (dropRate > 10) flags.push(badge('badge-err', '近期掉幀 ' + dropRate.toFixed(1) + '%'));
      else if (dropRate > 2) flags.push(badge('badge-warn', '近期掉幀 ' + dropRate.toFixed(1) + '%'));
      else flags.push(badge('badge-ok', '掉幀正常'));
    } else if (v.freezeCount != null && v.freezeCount > 0) {
      flags.push(badge('badge-warn', '卡頓 ' + v.freezeCount + ' 次'));
    }
  }

  if (!flags.length) flags.push(badge('badge-info', '等待統計…'));
  el.innerHTML = flags.join('');
}


async function startRtcAnalysis() {
  stopRtcAnalysis();
  resetRtcState();

  document.getElementById('rtcStartBtn').classList.add('hidden');
  document.getElementById('rtcStopBtn').classList.remove('hidden');
  var cs = document.getElementById('rtcConnState');
  cs.textContent = '連線中…';
  cs.className = 'stat-value text-warn';

  var url = document.getElementById('rtcUrlInput').value.trim();
  var video = document.getElementById('rtcPreviewVideo');
  var overlay = document.getElementById('rtcPreviewOverlay');

  try {
    rtcState.sdk = new SrsRtcWhipWhepAsync();
    rtcState.running = true;
    rtcState.prevTime = performance.now();

    var options = {};
    if (document.getElementById('rtcVideoOnly').checked) options.videoOnly = true;
    if (document.getElementById('rtcAudioOnly').checked) options.audioOnly = true;

    await Promise.race([
      rtcState.sdk.play(url, options),
      new Promise(function (_, reject) {
        setTimeout(function () { reject(new Error('連線逾時 (10s)')); }, 10000);
      })
    ]);

    video.srcObject = rtcState.sdk.stream;
    video.classList.remove('hidden');
    overlay.style.display = 'none';
    video.play();
    document.getElementById('rtcPreviewStatus').textContent = '播放中';
    document.getElementById('rtcPreviewStatus').className = 'text-ok';
    cs.textContent = '已連線';
    cs.className = 'stat-value text-ok';
    rtcState.connected = true;
    document.getElementById('rtcIceCandidate').textContent = 'candidate 彙整中…';
  } catch (err) {
    rtcState.running = false;
    cs.textContent = '連線失敗';
    cs.className = 'stat-value text-err';
    document.getElementById('rtcPreviewStatus').textContent = '失敗';
    document.getElementById('rtcPreviewStatus').className = 'text-err';
    console.error('RTC connect error:', err);
  }
}

function stopRtcAnalysis() {
  rtcState.running = false;
  rtcState.connected = false;
  if (rtcState.sdk) {
    try { rtcState.sdk.close(); } catch (e) {}
    rtcState.sdk = null;
  }
  var video = document.getElementById('rtcPreviewVideo');
  video.pause();
  video.srcObject = null;
  document.getElementById('rtcStartBtn').classList.remove('hidden');
  document.getElementById('rtcStopBtn').classList.add('hidden');
}

function resetRtcState() {
  rtcState.prevStats = null;
  rtcState.prevTime = 0;
  rtcState.rttSeries = [];
  rtcState.bitrateSeries = [];
  rtcState.jitterSeries = [];
  rtcState.vStats = null;
  rtcState.aStats = null;
  document.getElementById('rtcStatsTable').innerHTML = '';
  document.getElementById('rtcHealthFlags').innerHTML = '<span class="text-dim">尚未開始分析</span>';
  document.getElementById('rtcConnState').textContent = '未連線';
  document.getElementById('rtcConnState').className = 'stat-value text-dim';
  document.getElementById('rtcIceCandidate').textContent = '—';
  document.getElementById('rtcRtt').textContent = '—';
  document.getElementById('rtcRttSub').textContent = '';
  document.getElementById('rtcLoss').textContent = '—';
  document.getElementById('rtcLossSub').textContent = '';
  document.getElementById('rtcBitrate').textContent = '—';
  document.getElementById('rtcBitrateSub').textContent = '';
  document.getElementById('rtcRes').textContent = '—';
  document.getElementById('rtcResSub').textContent = '';
  rtcRttChart && rtcRttChart.data.datasets.forEach(function (d) { d.data = []; });
  rtcBitrateChart && rtcBitrateChart.data.datasets.forEach(function (d) { d.data = []; });
  rtcJitterChart && rtcJitterChart.data.datasets.forEach(function (d) { d.data = []; });
  rtcRttChart && rtcRttChart.update('none');
  rtcBitrateChart && rtcBitrateChart.update('none');
  rtcJitterChart && rtcJitterChart.update('none');
}

// ── 事件綁定 ──
document.getElementById('startBtn').addEventListener('click', startAnalysis);
document.getElementById('stopBtn').addEventListener('click', stopAnalysis);
document.getElementById('pauseBtn').addEventListener('click', function () {
  state.paused = !state.paused;
  this.textContent = state.paused ? '▶ 繼續列表' : '暫停列表';
});
document.getElementById('clearPktsBtn').addEventListener('click', function () {
  document.getElementById('pktTable').innerHTML = '';
});

var connCollapsed = false;
document.getElementById('connToggle').addEventListener('click', function () {
  connCollapsed = !connCollapsed;
  var body = document.getElementById('connBody');
  body.classList.toggle('hidden');
  this.textContent = connCollapsed ? '▸ 連線設定' : '▾ 連線設定';
});

document.getElementById('streamEye').addEventListener('click', toggleStreamReveal);
document.getElementById('flvUrl').addEventListener('click', toggleStreamReveal);
document.getElementById('hostInput').addEventListener('input', renderStreamVisibility);
document.getElementById('streamInput').addEventListener('input', renderStreamVisibility);
renderStreamVisibility();

document.getElementById('hudEnabled').addEventListener('change', function () {
  var hud = document.getElementById('previewHud');
  if (this.checked) {
    hud.classList.remove('hidden');
    updatePreviewHud();
  } else {
    hud.classList.add('hidden');
  }
});

// raw modal
document.getElementById('rawTabHex').addEventListener('click', function () { setRawTab('hex'); });
document.getElementById('rawTabText').addEventListener('click', function () { setRawTab('text'); });
document.getElementById('rawTabParsed').addEventListener('click', function () { setRawTab('parsed'); });
document.getElementById('rawModalClose').addEventListener('click', closeRawView);
document.getElementById('rawModal').addEventListener('click', function (e) {
  if (e.target === this) closeRawView();
});

// WebRTC
document.getElementById('rtcStartBtn').addEventListener('click', startRtcAnalysis);
document.getElementById('rtcStopBtn').addEventListener('click', stopRtcAnalysis);

// ── 定期更新 ──
setInterval(function () {
  if (state.running) {
    flushPacketRows();
    renderAudioBlocks();
    renderVideoBlocks();
    updateBitrateBuckets();
    checkPreviewStall();
  }
  if (rtcState.running) {
    pollRtcStats();
  }
}, 250);

buildCharts();
buildRtcCharts();
