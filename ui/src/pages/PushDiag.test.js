import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {JSDOM} from 'jsdom';
import {afterEach, beforeEach, describe, expect, it, vi} from 'vitest';

const playerRoot = resolve(process.cwd(), '../platform/containers/www/players');
const helper = readFileSync(resolve(playerRoot, 'js/pushdiag-hls.js'), 'utf8');
const html = readFileSync(resolve(playerRoot, 'pushdiag.html'), 'utf8');
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
  const script = [...w.document.scripts].find(s => !s.src);
  w.eval(script.textContent);
}

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
