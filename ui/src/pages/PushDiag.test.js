import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {JSDOM} from 'jsdom';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';

const playerRoot = resolve(process.cwd(), '../platform/containers/www/players');
const helper = readFileSync(resolve(playerRoot, 'js/pushdiag-hls.js'), 'utf8');
const html = readFileSync(resolve(playerRoot, 'pushdiag.html'), 'utf8');
// The page logic lives in an external file now; loading it here keeps the test honest about what the
// browser actually executes.
const pageScript = readFileSync(resolve(playerRoot, 'js/pushdiag.js'), 'utf8');
let dom, w;
beforeEach(() => {
  dom = new JSDOM(html, {url: 'https://example.test/players/pushdiag.html', runScripts: 'outside-only'});
  w = dom.window;
  w.HTMLMediaElement.prototype.play = vi.fn(() => Promise.resolve());
  w.HTMLMediaElement.prototype.pause = vi.fn();
  w.HTMLMediaElement.prototype.load = vi.fn();
  w.HTMLMediaElement.prototype.canPlayType = vi.fn(() => 'probably');
  w.HTMLCanvasElement.prototype.getContext = vi.fn(() => ({}));
  w.Chart = function (_, options) { this.data = options.data; this.update = vi.fn(); };
  w.setInterval = vi.fn();
  w.eval(helper);
});
afterEach(() => dom.window.close());
function loadPage() {
  w.eval(pageScript);
}

describe('PushDiag deep links', () => {
  it('analyses the stream named in ?stream= instead of the default', () => {
    loadPage();
    const picked = w.applyStreamParam('?app=live&stream=obs-test');
    expect(picked).toEqual({app: 'live', stream: 'obs-test'});
    expect(w.document.getElementById('streamInput').value).toBe('obs-test');
    // The source list must not silently override the link with "follow the newest".
    expect(w.document.getElementById('analysisSelect').value).toBe('manual');
  });

  it('defaults the app to live and ignores a link without a stream', () => {
    loadPage();
    // A link with only ?app= must not overwrite what the operator typed.
    expect(w.applyStreamParam('?app=live')).toBe(null);
    expect(w.document.getElementById('streamInput').value).toBe('livestream');
    expect(w.applyStreamParam('?stream=abc123').app).toBe('live');
  });
});

describe('PushDiag timeline marks and drift history', () => {
  it('records a video DTS rollback as a mark at its time', () => {
    loadPage();
    w.state.startTime = w.performance.now() - 10000;
    w.handleTag(9, 5000, new w.Uint8Array([0x17, 1, 0, 0, 0, 0, 0, 0, 1, 0x65]));
    w.handleTag(9, 5000, new w.Uint8Array([0x17, 1, 0, 0, 0, 0, 0, 0, 1, 0x65]));
    expect(w.state.videoRollbackCount).toBe(1);
    expect(w.state.dtsMarks.length).toBe(1);
    expect(w.state.dtsMarks[0].kind).toBe('video');
    expect(w.state.dtsMarks[0].t).toBeGreaterThan(0);
  });

  it('does not report drift until the window is long enough', () => {
    loadPage();
    w.state.startTime = w.performance.now() - 5000;
    w.state.syncSeries = [
      { i: 1, offset: 0, t: 1 },
      { i: 2, offset: 5, t: 4 },
    ];
    // Two samples over three seconds is too short to mean anything.
    expect(w.syncPpm(30)).toBe(null);
  });

  it('reports drift per million over a sufficient window', () => {
    loadPage();
    w.state.startTime = w.performance.now() - 40000;
    // Six samples inside the 30s window (t >= 10), spanning 28s.
    w.state.syncSeries = [
      { i: 1, offset: 0, t: 12 },
      { i: 2, offset: 8, t: 18 },
      { i: 3, offset: 16, t: 24 },
      { i: 4, offset: 24, t: 30 },
      { i: 5, offset: 32, t: 36 },
      { i: 6, offset: 40, t: 40 },
    ];
    // 40 ms over 28 s = 1.43e-3 -> ~1429 ppm.
    const ppm = w.syncPpm(30);
    expect(ppm).toBeGreaterThan(1300);
    expect(ppm).toBeLessThan(1600);
  });
});

describe('PushDiag GOP diagnostics', () => {
  it('excludes AVC configuration tags and keeps short real GOP intervals', () => {
    loadPage();
    w.handleTag(9, 0, new w.Uint8Array([0x17, 1, 0, 0, 0, 0, 0, 0, 1, 0x65]));
    w.handleTag(9, 1500, new w.Uint8Array([0x17, 0, 0, 0, 0]));
    w.handleTag(9, 2000, new w.Uint8Array([0x17, 1, 0, 0, 0, 0, 0, 0, 1, 0x65]));
    w.handleTag(9, 2100, new w.Uint8Array([0x17, 1, 0, 0, 0, 0, 0, 0, 1, 0x65]));
    expect(w.state.keyframes).toBe(3);
    expect(Array.from(w.state.keyGaps)).toEqual([2000, 100]);
  });

  it('identifies retained maximum rather than claiming current GOP or overall health', () => {
    loadPage();
    w.state.startTime = w.performance.now() - 15000;
    w.state.videoTags = w.state.audioTags = 10;
    w.state.keyframes = 3;
    w.state.keyGaps = [5900, 2450];
    w.renderHealth();
    const text = w.document.getElementById('healthFlags').textContent;
    expect(text).toContain('最近 2 個 GOP 最大 5.9s');
    expect(text).toContain('已收到影音封包');
    expect(text).not.toContain('影音正常');
    w.state.keyGaps = [];
    w.renderHealth();
    expect(w.document.getElementById('healthFlags').textContent).toContain('GOP 樣本不足');
  });
});

describe('PushDiag HLS compatibility', () => {
  it('converts only the stream path while preserving authentication parameters', () => {
    expect(w.PushDiagHls.derive('https://example.test/live/key.flv?token=a%2Bb')).toBe('https://example.test/live/key.m3u8?token=a%2Bb');
    expect(w.PushDiagHls.derive('https://example.test/rtc/v1/whep/?app=live&stream=test&token=secret')).toBe('https://example.test/live/test.m3u8?token=secret');
    expect(w.PushDiagHls.derive('javascript:alert(1)')).toBe('');
    expect(w.PushDiagHls.derive('http://example.test/live/key.flv')).toBe('');
    expect(w.PushDiagHls.derive('https://example.test/custom-whep')).toBe('');
  });
  it('prefers native HLS without depending on MSE or HLS.js', () => {
    const video = w.document.getElementById('hlsVideo');
    const status = vi.fn();
    const check = w.PushDiagHls.create(video, status);
    check.start('https://example.test/live/test.m3u8');
    expect(video.src).toBe('https://example.test/live/test.m3u8');
    expect(video.muted && video.playsInline).toBe(true);
    video.dispatchEvent(new w.Event('playing'));
    expect(status).toHaveBeenLastCalledWith('原生 HLS：播放中', false);
    check.stop();
    expect(video.hasAttribute('src')).toBe(false);
  });
  it('handles autoplay rejection without an unhandled promise or false codec failure', async () => {
    const video = w.document.getElementById('hlsVideo');
    video.play = vi.fn(() => Promise.reject(Object.assign(new Error(), {name: 'NotAllowedError'})));
    const status = vi.fn();
    const check = w.PushDiagHls.create(video, status);
    check.start('https://example.test/live/test.m3u8');
    await vi.waitFor(() => expect(status.mock.lastCall[0]).toContain('請點影片播放按鈕'));
    expect(status.mock.lastCall[1]).toBe(false);
  });
  it('uses HLS.js when native HLS is absent and releases it on fatal errors', () => {
    const video = w.document.getElementById('hlsVideo');
    video.canPlayType = () => '';
    const handlers = {}, destroy = vi.fn(), attachMedia = vi.fn();
    w.Hls = function () { this.on = (e, cb) => { handlers[e] = cb; }; this.loadSource = vi.fn(); this.attachMedia = attachMedia; this.destroy = destroy; };
    w.Hls.isSupported = () => true;
    w.Hls.Events = {ERROR: 'error', MANIFEST_PARSED: 'manifest'};
    const status = vi.fn();
    w.PushDiagHls.create(video, status).start('https://example.test/live/test.m3u8');
    expect(attachMedia).toHaveBeenCalledWith(video);
    handlers.error(null, {fatal: true});
    expect(destroy).toHaveBeenCalledOnce();
    expect(status.mock.lastCall[1]).toBe(true);
  });
  it('keeps FLV diagnostics as the primary route when preview is unavailable', () => {
    loadPage();
    w.startPreview('https://example.test/live/test.flv');
    expect(w.document.getElementById('previewVideo').hasAttribute('src')).toBe(false);
    expect(w.document.getElementById('fallbackNotice').classList.contains('hidden')).toBe(true);
    expect(w.document.getElementById('previewStatus').textContent).toBe('僅分析 FLV 封包');
    expect(w.document.getElementById('flvPanel').classList.contains('hidden')).toBe(false);
  });
  it('parses fragmented raw FLV packets without any media playback capability', async () => {
    loadPage();
    // FLV header followed by an MP3 audio tag, split across network reads.
    const bytes = new w.Uint8Array([70,76,86,1,4,0,0,0,9,0,0,0,0,
      8,0,0,2,0,0,40,0,0,0,0,47,0,0,0,0,13]);
    let rejectRead;
    const read = vi.fn().mockResolvedValueOnce({value: bytes.slice(0, 18), done: false})
      .mockResolvedValueOnce({value: bytes.slice(18), done: false})
      .mockImplementation(() => new Promise((_, reject) => { rejectRead = reject; }));
    w.fetch = vi.fn(async (_, options) => {
      options.signal.addEventListener('abort', () => rejectRead?.(Object.assign(new Error(), {name: 'AbortError'})));
      return {ok: true, headers: {get: () => 'video/x-flv'}, body: {getReader: () => ({read})}};
    });
    const analysis = w.startAnalysis();
    await vi.waitFor(() => expect(w.state.audioTags).toBe(1));
    expect(w.state.running).toBe(true);
    expect(w.state.audioLastTs).toBe(40);
    expect(w.document.getElementById('previewStatus').textContent).toBe('僅分析 FLV 封包');
    expect(w.document.getElementById('hlsPanel').classList.contains('hidden')).toBe(true);
    w.stopAnalysis();
    await analysis;
  });
  it('enables the managed media preview path and isolates player errors', () => {
    loadPage();
    let onError;
    const player = {on: (_, cb) => { onError = cb; }, attachMediaElement: vi.fn(),
      load: vi.fn(), play: vi.fn(() => Promise.resolve()), destroy: vi.fn()};
    w.mpegts = {getFeatureList: () => ({mseLivePlayback: true}), createPlayer: () => player, Events: {ERROR: 'error'}};
    w.state.running = true;
    w.startPreview('https://example.test/live/test.flv');
    expect(w.document.getElementById('previewVideo').disableRemotePlayback).toBe(true);
    expect(player.play).toHaveBeenCalledOnce();
    onError();
    expect(player.destroy).toHaveBeenCalledOnce();
    expect(w.state.running).toBe(true);
    expect(w.document.getElementById('previewStatus').textContent).toBe('僅分析 FLV 封包');
  });
  it('populates the source selectors from the SRS stream API', async () => {
    w.localStorage.setItem('SRS_TERRAFORM_TOKEN', JSON.stringify({bearer: 'test-bearer'}));
    w.fetch = vi.fn(async () => ({json: async () => ({code: 0, streams: [
      {vhost: '__defaultVhost__', app: 'live', name: 'streamA', kbps: {recv_30s: 1000, send_30s: 900}, publish: {active: true}, clients: 2},
    ]})}));
    loadPage();
    await vi.waitFor(() => {
      expect([...w.document.getElementById('analysisSelect').options].some(o => o.value.includes('streamA'))).toBe(true);
    });
    expect(w.fetch).toHaveBeenCalledWith('/api/v1/streams/', expect.objectContaining({headers: {Authorization: 'Bearer test-bearer'}}));
    expect(w.document.getElementById('streamInput').value).toBe('streamA');
    expect(w.document.getElementById('appInput').value).toBe('live');
  });
  it('smooths the WebRTC frame-drop rate over a rolling window', () => {
    loadPage();
    const fn = w.computeFrameDropRates;
    expect(typeof fn).toBe('function');
    // Below the minimum window: no recent rate yet, cumulative still reported.
    let r = fn([{t: 0, decoded: 0, dropped: 0}, {t: 1000, decoded: 5, dropped: 5}], 1000, 3000, 1500);
    expect(r.recent).toBeNull();
    expect(r.cumulative).toBeCloseTo(50);
    // The 3s window excludes the early spike (t=0); the cumulative keeps it.
    const samples = [
      {t: 0, decoded: 0, dropped: 10},
      {t: 2000, decoded: 100, dropped: 10},
      {t: 3000, decoded: 150, dropped: 11},
      {t: 5000, decoded: 250, dropped: 12},
    ];
    r = fn(samples, 5000, 3000, 1500);
    expect(r.recent).toBeCloseTo(2 / 152 * 100);
    expect(r.cumulative).toBeCloseTo(12 / 262 * 100);
  });
  it('restores RTC controls and offers HLS when WebRTC is unavailable', async () => {
    loadPage();
    w.console.error = vi.fn();
    w.document.getElementById('rtcUrlInput').value = 'https://example.test/rtc/v1/whep/?app=live&stream=test';
    await w.startRtcAnalysis();
    expect(w.document.getElementById('rtcStartBtn').classList.contains('hidden')).toBe(false);
    w.document.getElementById('fallbackBtn').click();
    expect(w.document.getElementById('hlsUrlInput').value).toBe('https://example.test/live/test.m3u8');
    expect(w.rtcState.running).toBe(false);
  });
});
