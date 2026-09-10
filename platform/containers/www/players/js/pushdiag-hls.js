// HLS compatibility playback for pushdiag. Native HLS is preferred on Safari.
(function (root) {
  'use strict';
  function httpUrl(value) {
    var url = new URL(value, root.location.href);
    if (!/^https?:$/.test(url.protocol)) throw new Error('請使用 HTTP 或 HTTPS 網址');
    if (root.location.protocol === 'https:' && url.protocol === 'http:') {
      throw new Error('HTTPS 頁面不能播放 HTTP 串流，請使用 HTTPS 串流網址');
    }
    return url;
  }
  function derive(value) {
    try {
      var url = httpUrl(value);
      if (/\.flv$/i.test(url.pathname)) url.pathname = url.pathname.replace(/\.flv$/i, '.m3u8');
      else if (/\/rtc\/v1\/whep\/?$/.test(url.pathname)) {
        var app = url.searchParams.get('app');
        var stream = url.searchParams.get('stream');
        if (!app || !stream) return '';
        url.pathname = '/' + app.split('/').map(encodeURIComponent).join('/') + '/' + encodeURIComponent(stream) + '.m3u8';
        url.searchParams.delete('app');
        url.searchParams.delete('stream');
      } else if (!/\.m3u8$/i.test(url.pathname)) return '';
      url.hash = '';
      return url.href;
    } catch (_) { return ''; }
  }
  function create(video, status) {
    var player = null, generation = 0, timeout = null, playing = false;
    function stop() {
      generation++;
      clearTimeout(timeout);
      timeout = null;
      video.onplaying = video.onerror = video.onwaiting = null;
      if (player) { player.destroy(); player = null; }
      video.pause();
      video.removeAttribute('src');
      video.load();
      playing = false;
    }
    function start(value) {
      stop();
      var current = generation, method = '';
      function report(message, error) { if (current === generation) status(message, !!error); }
      function fail(message) { if (current !== generation) return; stop(); status(message, true); }
      function play() {
        var promise;
        try { promise = video.play(); } catch (err) { fail(err.message); return; }
        if (promise && promise.catch) promise.catch(function (err) {
          if (current !== generation || err.name === 'AbortError') return;
          if (err.name === 'NotAllowedError') {
            clearTimeout(timeout);
            report('已載入，請點影片播放按鈕（行動裝置播放限制）');
          } else fail('HLS 無法播放：請確認編碼、網址與伺服器 HLS 設定');
        });
      }
      try {
        var url = httpUrl(value);
        if (!/\.m3u8$/i.test(url.pathname)) throw new Error('請輸入完整 .m3u8 播放網址');
        video.muted = true;
        video.playsInline = true;
        video.controls = true;
        video.onplaying = function () { playing = true; clearTimeout(timeout); report(method + '：播放中'); };
        video.onwaiting = function () { if (playing) report(method + '：緩衝中'); };
        video.onerror = function () { fail('HLS 載入失敗：請確認 HLS 已啟用、串流在線、編碼及跨域設定'); };
        timeout = setTimeout(function () { fail('HLS 等待逾時，請確認串流在線並重試'); }, 20000);
        if (video.canPlayType('application/vnd.apple.mpegurl')) {
          method = '原生 HLS';
          report(method + '：載入中');
          video.src = url.href;
          play();
        } else if (root.Hls && root.Hls.isSupported()) {
          method = 'HLS.js';
          report(method + '：載入中');
          player = new root.Hls();
          player.on(root.Hls.Events.ERROR, function (_, data) {
            if (data.fatal) fail('HLS 載入失敗：請確認串流、編碼及跨域設定');
          });
          player.on(root.Hls.Events.MANIFEST_PARSED, function () { if (current === generation) play(); });
          player.loadSource(url.href);
          player.attachMedia(video);
        } else throw new Error('此環境沒有原生 HLS 或可用的 HLS.js，請改用支援 HLS 的瀏覽器');
      } catch (err) { fail(err.message); }
    }
    return {start: start, stop: stop};
  }
  root.PushDiagHls = {derive: derive, create: create};
})(window);
