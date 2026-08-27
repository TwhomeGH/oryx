// to query the swf anti cache.
function srs_get_version_code() { return "1.33"; }

var SRS_PREVIEW_VIDEO = 'https://test-streams.mux.dev/x36xhzz/x36xhzz.m3u8';

/**
 * Play the preview video in preview mode (file://).
 * Uses hls.js when available (Chrome/Edge/Firefox), falls back to native
 * HLS for Safari.
 * @param videoEl the video element to attach the stream to.
 */
function srs_preview_play(videoEl) {
    videoEl.classList.remove('hidden');
    if (typeof Hls !== 'undefined' && Hls.isSupported()) {
        var hls = new Hls({ enableWorker: true });
        hls.loadSource(SRS_PREVIEW_VIDEO);
        hls.attachMedia(videoEl);
    } else if (videoEl.canPlayType('application/vnd.apple.mpegurl')) {
        videoEl.src = SRS_PREVIEW_VIDEO;
    }
}

/**
 * update the navigator, add same query string.
 */
function update_nav() {
    var qs = window.location.search;
    var navs = {
        nav_srs_player: 'srs_player.html',
        nav_rtc_player: 'rtc_player.html',
        nav_rtc_publisher: 'rtc_publisher.html',
        nav_whip: 'whip.html',
        nav_whep: 'whep.html',
        nav_srs_publisher: 'srs_publisher.html'
    };
    for (var id in navs) {
        var el = document.getElementById(id);
        if (el) el.href = navs[id] + qs;
    }
}

// Special extra params, such as auth_key.
function user_extra_params(query, params) {
    var queries = params || [];
    var skip = {
        app:1, autostart:1, dir:1, filename:1, host:1, hostname:1,
        http_port:1, pathname:1, port:1, server:1, stream:1, buffer:1,
        schema:1, vhost:1, api:1, path:1
    };

    for (var key in query.user_query) {
        if (skip[key]) continue;
        if (query[key]) {
            queries.push(key + '=' + query[key]);
        }
    }

    return queries;
}

function is_default_port(schema, port) {
    return (schema === 'http' && port === 80)
        || (schema === 'https' && port === 443)
        || (schema === 'webrtc' && port === 1985)
        || (schema === 'rtmp' && port === 1935);
}

/**
 * Build default FLV URL from query string params.
 */
function build_default_flv_url() {
    var query = parse_query_string();

    var schema = (!query.schema) ? "http" : query.schema;
    var server = (!query.server) ? window.location.hostname : query.server;
    var port = (!query.port) ? (schema === "http" ? 8080 : 1935) : Number(query.port);
    var vhost = (!query.vhost) ? window.location.hostname : query.vhost;
    var app = (!query.app) ? "live" : query.app;
    var stream = (!query.stream) ? "livestream.flv" : query.stream;

    var queries = [];
    if (server !== vhost && vhost !== "__defaultVhost__") {
        queries.push("vhost=" + vhost);
    }
    queries = user_extra_params(query, queries);

    var uri = schema + "://" + server;
    if (!is_default_port(schema, port)) {
        uri += ":" + port;
    }
    uri += "/" + app + "/" + stream + "?" + queries.join('&');
    while (uri.indexOf("?") === uri.length - 1) {
        uri = uri.slice(0, uri.length - 1);
    }

    return uri;
}

function build_default_rtc_url(query) {
    var server = (!query.server) ? window.location.hostname : query.server;
    var vhost = (!query.vhost) ? window.location.hostname : query.vhost;
    var app = (!query.app) ? "live" : query.app;
    var stream = (!query.stream) ? "livestream" : query.stream;
    var api = query.api ? ':' + query.api : '';

    var queries = [];
    if (server !== vhost && vhost !== "__defaultVhost__") {
        queries.push("vhost=" + vhost);
    }
    if (query.schema && window.location.protocol !== query.schema + ':') {
        queries.push('schema=' + query.schema);
    }
    queries = user_extra_params(query, queries);

    var uri = "webrtc://" + server + api + "/" + app + "/" + stream + "?" + queries.join('&');
    while (uri.lastIndexOf("?") === uri.length - 1) {
        uri = uri.slice(0, uri.length - 1);
    }

    return uri;
}

function build_default_whip_whep_url(query, apiPath) {
    var server = (!query.server) ? window.location.hostname : query.server;
    var vhost = (!query.vhost) ? window.location.hostname : query.vhost;
    var app = (!query.app) ? "live" : query.app;
    var stream = (!query.stream) ? "livestream" : query.stream;
    var api = ':' + (query.api || (window.location.protocol === 'http:' ? '1985' : '1990'));
    var realApiPath = query.path || apiPath;

    var queries = [];
    if (server !== vhost && vhost !== "__defaultVhost__") {
        queries.push("vhost=" + vhost);
    }
    if (query.schema && window.location.protocol !== query.schema + ':') {
        queries.push('schema=' + query.schema);
    }
    queries = user_extra_params(query, queries);

    var uri = window.location.protocol + "//" + server + api + realApiPath + "?app=" + app + "&stream=" + stream + "&" + queries.join('&');
    while (uri.lastIndexOf("?") === uri.length - 1) {
        uri = uri.slice(0, uri.length - 1);
    }
    while (uri.lastIndexOf("&") === uri.length - 1) {
        uri = uri.slice(0, uri.length - 1);
    }

    return uri;
}

/**
 * initialize the page.
 * @param flv_url the div id contains the flv stream url to play
 * @param modal_player the div id contains the modal player
 */
function srs_init_flv(flv_url, modal_player) {
    update_nav();
    if (flv_url) {
        document.querySelector(flv_url).value = build_default_flv_url();
    }
}
function srs_init_rtc(id, query) {
    update_nav();
    document.querySelector(id).value = build_default_rtc_url(query);
}
function srs_init_whip(id, query) {
    update_nav();
    document.querySelector(id).value = build_default_whip_whep_url(query, '/rtc/v1/whip/');
}
function srs_init_whep(id, query) {
    update_nav();
    document.querySelector(id).value = build_default_whip_whep_url(query, '/rtc/v1/whep/');
}
