// 用於播放器 播放使用
var PREVIEW_VIDEO = 'https://test-streams.mux.dev/x36xhzz/x36xhzz.m3u8';
var isPreview = window.location.protocol === 'file:';
var flvPlayer = null, tsPlayer = null, hlsPlayer = null, dashPlayer = null;
var videoEl = document.getElementById('video_player');
var audioEl = document.getElementById('audio_player');
var urlInput = document.getElementById('txt_url');
var shareSection = document.getElementById('share_section');
var linkUrl = document.getElementById('link_url');
var errorAlert = document.getElementById('error_alert');
var errorMsg = document.getElementById('error_msg');
var infoAlert = document.getElementById('info_alert');
var previewBanner = document.getElementById('preview_banner');

if (isPreview) {
    previewBanner.textContent = 'Running locally — using placeholder video for layout testing.';
    previewBanner.classList.remove('hidden');
    infoAlert.classList.add('hidden');
    videoEl.classList.remove('hidden');
    audioEl.classList.add('hidden');
    if (typeof Hls !== 'undefined' && Hls.isSupported()) {
        hlsPlayer = new Hls({ enableWorker: true });
        hlsPlayer.loadSource(PREVIEW_VIDEO);
        hlsPlayer.attachMedia(videoEl);
    } else {
        videoEl.src = PREVIEW_VIDEO;
    }
}

function stopPlayers() {
    [flvPlayer, tsPlayer, hlsPlayer, dashPlayer].forEach(function (p) {
        if (p) try { p.destroy(); } catch (e) { /* ignore */ }
    });
    flvPlayer = tsPlayer = hlsPlayer = dashPlayer = null;
}

function showError(msg) {
    stopPlayers();
    errorMsg.textContent = msg;
    errorAlert.classList.remove('hidden');
    infoAlert.classList.add('hidden');
    videoEl.classList.add('hidden');
    audioEl.classList.add('hidden');
    shareSection.classList.add('hidden');
}

function showVideo() {
    errorAlert.classList.add('hidden');
    infoAlert.classList.remove('hidden');
    videoEl.classList.remove('hidden');
    audioEl.classList.add('hidden');
}

function showAudio() {
    errorAlert.classList.add('hidden');
    infoAlert.classList.remove('hidden');
    videoEl.classList.add('hidden');
    audioEl.classList.remove('hidden');
}

function buildShareUrl(r) {
    var q = parse_query_string();
    // host/pathname keep slashes (encodeURI); query values are fully
    // encoded (encodeURIComponent) so user input can't break out of the
    // URL into a javascript: scheme or an attribute boundary.
    var url = window.location.protocol + '//' + encodeURI(q.host || '') + encodeURI(q.pathname || '')
        + '?autostart=true'
        + '&app=' + encodeURIComponent(r.app) + '&stream=' + encodeURIComponent(r.stream)
        + '&server=' + encodeURIComponent(r.server) + '&port=' + encodeURIComponent(r.port);
    if (r.vhost !== '__defaultVhost__') url += '&vhost=' + encodeURIComponent(r.vhost);
    if (r.schema !== 'rtmp') url += '&schema=' + encodeURIComponent(r.schema);
    return url;
}

/** 安全打包URL */
function sanitizeUrl(url) {
    try {
        const u = new URL(url, window.location.origin);
        if (['http:', 'https:', 'blob:', 'data:'].includes(u.protocol)) {
            return u.href;
        }
    } catch (e) { }
    return '';
}

/** 安全播放原生 audio/video */
function safePlay(el, url, type = 'video') {
    try {
        el.src = url;
        el.play().catch(err => showError(`${type} play failed: ${err.message}`));
    } catch (err) {
        showError(`${type} element error: ${err.message}`);
    }
}

/** 初始化播放器並加錯誤處理 */
function initPlayer(player, el, url, type = 'generic') {
    try {
        // mpegts.js (flv/ts) uses attachMediaElement/load/play; hls.js and
        // dash.js attach+load the source in startPlay, so here we only start
        // playback on the media element.
        if (typeof player.attachMediaElement === 'function') {
            player.attachMediaElement(el);
            player.load();
            player.play().catch(err => {
                showError(`${type} play failed: ${err.message}`);
            });
        } else {
            el.play().catch(err => {
                showError(`${type} play failed: ${err.message}`);
            });
        }

        // 綁定錯誤事件
        if (type === 'hls' && player.on) {
            player.on(Hls.Events.ERROR, (event, data) => {
                showError(`HLS error: ${data.type} - ${data.details}`);
            });
        } else if (type === 'flv' && player.on) {
            player.on(mpegts.Events.ERROR, e => {
                showError(`FLV error: ${e}`);
            });
        } else if (type === 'dash' && player.on) {
            player.on('error', e => {
                showError(`DASH error: ${e}`);
            });
        } else if (type === 'ts' && player.on) {
            player.on(mpegts.Events.ERROR, e => {
                showError(`TS error: ${e}`);
            });
        }
    } catch (err) {
        showError(`${type} init error: ${err.message}`);
    }
}

/** 主播放函數 */
function startPlay(r) {
    stopPlayers();
    const safeUrl = sanitizeUrl(r.url);
    if (!safeUrl) return showError('Invalid or unsafe URL');

    const stream = r.stream.toLowerCase();

    // Audio
    if (stream.endsWith('.mp3') || stream.endsWith('.aac')) {
        showAudio();
        safePlay(audioEl, safeUrl, 'audio');
        return;
    }

    // MP4
    if (stream.endsWith('.mp4')) {
        showVideo();
        safePlay(videoEl, safeUrl, 'video');
        return;
    }

    // TS
    if (stream.endsWith('.ts')) {
        if (!mpegts || !mpegts.getFeatureList().mseLivePlayback) {
            return showError('HTTP-TS not supported.');
        }
        showVideo();
        tsPlayer = mpegts.createPlayer({ type: 'mpegts', url: safeUrl, isLive: true, enableStashBuffer: false });
        initPlayer(tsPlayer, videoEl, safeUrl, 'ts');
        return;
    }

    // HLS
    if (stream.endsWith('.m3u8')) {
        if (typeof Hls === 'undefined' || !Hls.isSupported()) {
            if (videoEl.canPlayType('application/vnd.apple.mpegURL')) {
                showVideo();
                safePlay(videoEl, safeUrl, 'video');
                return;
            }
            return showError('HLS not supported.');
        }
        showVideo();
        hlsPlayer = new Hls({ enableWorker: true, lowLatencyMode: true });
        hlsPlayer.loadSource(safeUrl);
        hlsPlayer.attachMedia(videoEl);
        initPlayer(hlsPlayer, videoEl, safeUrl, 'hls');
        return;
    }

    // DASH
    if (stream.endsWith('.mpd')) {
        showVideo();
        dashPlayer = dashjs.MediaPlayer().create();
        dashPlayer.initialize(videoEl, safeUrl, true);
        initPlayer(dashPlayer, videoEl, safeUrl, 'dash');
        return;
    }

    // FLV (default)
    if (stream.endsWith('.flv') || safeUrl.startsWith('http')) {
        if (!mpegts || !mpegts.getFeatureList().mseLivePlayback) {
            return showError('HTTP-FLV not supported.');
        }
        showVideo();
        flvPlayer = mpegts.createPlayer({ type: 'flv', url: safeUrl, isLive: true, enableStashBuffer: false });
        initPlayer(flvPlayer, videoEl, safeUrl, 'flv');
        return;
    }

    showError('Unsupported stream type: ' + stream);
}



function applyUrlChange() {
    var r = parse_rtmp_url(urlInput.value);
    if (r.schema === 'rtmp') {
        showError('RTMP is not supported. Use HTTP-FLV or HLS instead.');
        return null;
    }
    linkUrl.href = buildShareUrl(r);
    linkUrl.textContent = buildShareUrl(r);
    shareSection.classList.remove('hidden');
    return r;
}

document.getElementById('btn_play').addEventListener('click', function () {
    videoEl.muted = false;
    audioEl.muted = false;
    var r = applyUrlChange();
    if (r) startPlay(r);
});

// Initialize
if (!isPreview) {
    var query = parse_query_string();
    urlInput.value = build_default_flv_url();

    if (query.autostart === 'true') {
        videoEl.muted = true;
        var r = applyUrlChange();
        if (r) startPlay(r);
    } else {
        videoEl.classList.add('hidden');
        audioEl.classList.add('hidden');
    }
}
