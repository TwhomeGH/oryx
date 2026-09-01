//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
// SRS Console: real-time monitoring of the SRS core (summaries, vhosts,
// streams, clients, configs). Replaces the legacy AngularJS console
// under /console/. Talks to the SRS HTTP API via the platform /api/ proxy.
//
import Container from "react-bootstrap/Container";
import React from "react";
import {Token} from "../utils";
import axios from "axios";
import {Row, Col, Card, Table, Tab, Tabs, Button, Badge, Alert} from "react-bootstrap";
import moment from "moment";
import {useTranslation} from "react-i18next";
import {useErrorHandler} from "react-error-boundary";
import {SrsErrorBoundary} from "../components/SrsErrorBoundary";

export default function SrsConsole() {
  return (
    <SrsErrorBoundary>
      <SrsConsoleImpl />
    </SrsErrorBoundary>
  );
}

// Call the SRS HTTP API via the platform /api/ proxy. No auth on
// /api/v1/versions; everything else requires the bearer token.
//
// SRS HTTP API responses have inconsistent shapes:
//   /api/v1/summaries -> {code:0, data:{self, system, ...}}
//   /api/v1/vhosts/   -> {code:0, vhosts:[...]}
//   /api/v1/streams/  -> {code:0, streams:[...]}
//   /api/v1/clients/  -> {code:0, clients:[...]}
//   /api/v1/raw       -> {code:0, http_api:{...}}
// So we return the full envelope and let callers pick the field.
function srsApi(path, params) {
  const url = `/api/v1${path}`;
  const headers = Token.loadBearerHeader();
  return axios.get(url, {
    params: params || {},
    headers,
  }).then(res => {
    if (res.data.code !== 0) {
      throw new Error(`SRS API error ${res.data.code}: ${res.data.server || ''}`);
    }
    return res.data;
  });
}

function SrsConsoleImpl() {
  const {t} = useTranslation();
  const handleError = useErrorHandler();
  const [key, setKey] = React.useState('overview');

  return (
    <>
      <Container fluid className="pt-3">
        <Tabs activeKey={key} onSelect={(k) => setKey(k)} className="mb-3">
          <Tab eventKey="overview" title={t('console.overview')}>
            <SrsOverview {...{handleError}} />
          </Tab>
          <Tab eventKey="vhosts" title={t('console.vhosts')}>
            <SrsVhosts {...{handleError}} />
          </Tab>
          <Tab eventKey="streams" title={t('console.streams')}>
            <SrsStreams {...{handleError}} />
          </Tab>
          <Tab eventKey="clients" title={t('console.clients')}>
            <SrsClients {...{handleError}} />
          </Tab>
          <Tab eventKey="configs" title={t('console.configs')}>
            <SrsConfigs {...{handleError}} />
          </Tab>
        </Tabs>
      </Container>
    </>
  );
}

// ── Helpers ──
function fmtUptime(sec) {
  if (!sec && sec !== 0) return '-';
  sec = Math.floor(sec);
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  const pad = (n) => String(n).padStart(2, '0');
  let out = `${pad(h)}:${pad(m)}:${pad(s)}`;
  if (d > 0) out = `${d}d ${out}`;
  return out;
}

function fmtBytes(n) {
  if (n === null || n === undefined) return '-';
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

function fmtBitrate(bps) {
  if (bps === null || bps === undefined) return '-';
  const units = ['bps', 'Kbps', 'Mbps', 'Gbps'];
  let i = 0;
  let v = bps;
  while (v >= 1000 && i < units.length - 1) { v /= 1000; i++; }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

function fmtPercent(n, digits = 2) {
  if (n === null || n === undefined) return '-';
  return `${(n * 100).toFixed(digits)}%`;
}

function fmtSec(sec) {
  if (sec === null || sec === undefined) return '-';
  if (sec < 60) return `${sec}s`;
  return moment.duration(sec, 'seconds').humanize();
}

// The stream URL, derived from tcUrl + name.
function buildStreamUrl(owner, stream) {
  let tcUrl = stream.tcUrl || stream.url || '';
  const qIdx = tcUrl.indexOf('?');
  let query = '';
  if (qIdx >= 0) {
    query = tcUrl.slice(qIdx);
    tcUrl = tcUrl.slice(0, qIdx);
  }
  // Drop default vhost query params.
  query = query.replace(/[?&](vhost|domain)=__defaultVhost__/g, '');
  const name = stream.name || tcUrl.slice(tcUrl.lastIndexOf('/') + 1);
  let url = `${tcUrl}/${name}${query}`;
  if (url.startsWith('?')) url = url.slice(1);
  return url;
}

function fmtVideo(v) {
  if (!v) return '-';
  return `${v.codec || ''} ${v.profile || ''} ${v.level || ''} ${v.width}x${v.height}`.trim() || '-';
}

function fmtAudio(a) {
  if (!a) return '-';
  // SRS 7 streams API reports the channel count as "channel" (singular);
  // keep "channels" as a fallback for older SRS versions. "stereo" is never
  // sent by SRS, so it must not be used to decide mono/stereo.
  const chans = a.channels !== undefined ? a.channels : a.channel;
  const ch = chans === 2 ? 'stereo' : chans === 1 ? 'mono' : (chans ? `${chans}ch` : '?');
  return `${a.codec || ''} ${a.sample_rate || ''} ${ch} ${a.profile || ''}`.trim() || '-';
}

// ── Overview (summaries) ──
function SrsOverview({handleError}) {
  const {t} = useTranslation();
  const [data, setData] = React.useState();
  const [kbps, setKbps] = React.useState();
  const prev = React.useRef();

  React.useEffect(() => {
    let cancelled = false;
    let timer;

    const refresh = () => {
      srsApi('/summaries').then((env) => {
        if (cancelled) return;
        const d = env.data;
        setData(d);

        // Compute network/srs kbps deltas between polls.
        if (prev.current) {
          const p = prev.current;
          const diffSys = d.system.net_sample_time - p.system.net_sample_time;
          const diffSrs = d.system.srs_sample_time - p.system.srs_sample_time;
          const k = {
            inSrs: diffSrs > 0 ? (d.system.srs_recv_bytes - p.system.srs_recv_bytes) * 8 / diffSrs : 0,
            outSrs: diffSrs > 0 ? (d.system.srs_send_bytes - p.system.srs_send_bytes) * 8 / diffSrs : 0,
            inSys: diffSys > 0 ? (d.system.net_recv_bytes - p.system.net_recv_bytes) * 8 / diffSys : 0,
            outSys: diffSys > 0 ? (d.system.net_send_bytes - p.system.net_send_bytes) * 8 / diffSys : 0,
            inInner: diffSys > 0 ? (d.system.net_recvi_bytes - p.system.net_recvi_bytes) * 8 / diffSys : 0,
            outInner: diffSys > 0 ? (d.system.net_sendi_bytes - p.system.net_sendi_bytes) * 8 / diffSys : 0,
          };
          setKbps(k);
        }
        prev.current = d;
        timer = setTimeout(refresh, 3000);
      }).catch((e) => {
        if (!cancelled) handleError(e);
      });
    };

    refresh();
    return () => { cancelled = true; if (timer) clearTimeout(timer); };
  }, [handleError]);

  if (!data) {
    return <Alert variant="info">{t('console.loading')}</Alert>;
  }

  const infoRow = (label, value) => (
    <tr>
      <td style={{color: 'var(--bs-secondary-color)'}}>{label}</td>
      <td>{value}</td>
    </tr>
  );

  return (
    <Row className="g-3">
      <Col xs={12} md={6} xl={3}>
        <Card>
          <Card.Header>SRS {data.self?.version}</Card.Header>
          <Table size="sm" striped hover className="mb-0">
            <tbody>
              {infoRow(t('console.uptime'), fmtUptime(data.self?.srs_uptime))}
              {infoRow(t('console.cpu'), fmtPercent(data.self?.cpu_percent))}
              {infoRow(t('console.mem'), fmtPercent(data.self?.mem_percent))}
              {infoRow(t('console.memKb'), fmtBytes(data.self?.mem_kbyte * 1024))}
              {infoRow(t('console.conn'), data.system?.conn_srs)}
              {infoRow(t('console.in'), kbps ? fmtBitrate(kbps.inSrs) : '-')}
              {infoRow(t('console.out'), kbps ? fmtBitrate(kbps.outSrs) : '-')}
            </tbody>
          </Table>
        </Card>
      </Col>
      <Col xs={12} md={6} xl={3}>
        <Card>
          <Card.Header>{t('console.osSystem')}</Card.Header>
          <Table size="sm" striped hover className="mb-0">
            <tbody>
              {infoRow(t('console.uptime'), fmtUptime(data.system?.uptime))}
              {infoRow(t('console.cpu'), fmtPercent(data.system?.cpu_percent * (data.system?.cpus_online || 1)))}
              {infoRow(t('console.mem'), fmtPercent(data.system?.mem_ram_percent))}
              {infoRow(t('console.memKb'), fmtBytes(data.system?.mem_ram_kbyte * 1024))}
              {infoRow(t('console.load'), `${data.system?.load_1m} / ${data.system?.load_5m} / ${data.system?.load_15m}`)}
              {infoRow(t('console.cpus'), `${data.system?.cpus} (${data.system?.cpus_online} ${t('console.online')})`)}
            </tbody>
          </Table>
        </Card>
      </Col>
      <Col xs={12} md={6} xl={3}>
        <Card>
          <Card.Header>{t('console.ioLoad')}</Card.Header>
          <Table size="sm" striped hover className="mb-0">
            <tbody>
              {infoRow(t('console.inSys'), kbps ? fmtBitrate(kbps.inSys) : '-')}
              {infoRow(t('console.outSys'), kbps ? fmtBitrate(kbps.outSys) : '-')}
              {infoRow(t('console.inInner'), kbps ? fmtBitrate(kbps.inInner) : '-')}
              {infoRow(t('console.outInner'), kbps ? fmtBitrate(kbps.outInner) : '-')}
              {infoRow(t('console.connSys'), data.system?.conn_sys)}
              {infoRow(t('console.diskBusy'), fmtPercent(data.system?.disk_busy_percent))}
              {infoRow(t('console.diskRead'), data.system?.disk_read_KBps ? `${data.system.disk_read_KBps} KB/s` : '-')}
              {infoRow(t('console.diskWrite'), data.system?.disk_write_KBps ? `${data.system.disk_write_KBps} KB/s` : '-')}
            </tbody>
          </Table>
        </Card>
      </Col>
      <Col xs={12} md={6} xl={3}>
        <Card>
          <Card.Header>{t('console.others')}</Card.Header>
          <Table size="sm" striped hover className="mb-0">
            <tbody>
              {infoRow(t('console.pid'), data.self?.pid)}
              {infoRow(t('console.ppid'), data.self?.ppid)}
              {infoRow(t('console.ok'), data.ok ? <Badge bg="success">{t('console.yes')}</Badge> : <Badge bg="danger">{t('console.no')}</Badge>)}
              {infoRow(t('console.srsConn'), data.system?.conn_srs)}
              {infoRow(t('console.sysConn'), data.system?.conn_sys)}
            </tbody>
          </Table>
        </Card>
      </Col>
    </Row>
  );
}

// ── Vhosts ──
function SrsVhosts({handleError}) {
  const {t} = useTranslation();
  const [vhosts, setVhosts] = React.useState();

  React.useEffect(() => {
    let cancelled = false;
    let timer;

    const refresh = () => {
      srsApi('/vhosts/').then((d) => {
        if (cancelled) return;
        setVhosts(d.vhosts || []);
        timer = setTimeout(refresh, 3000);
      }).catch((e) => {
        if (!cancelled) handleError(e);
      });
    };

    refresh();
    return () => { cancelled = true; if (timer) clearTimeout(timer); };
  }, [handleError]);

  if (!vhosts) return <Alert variant="info">{t('console.loading')}</Alert>;

  return (
    <Table size="sm" striped bordered hover>
      <thead>
        <tr>
          <th>ID</th>
          <th>{t('console.name')}</th>
          <th>{t('console.status')}</th>
          <th>{t('console.streams')}</th>
          <th>{t('console.clients')}</th>
          <th>{t('console.in')}</th>
          <th>{t('console.out')}</th>
          <th>HLS</th>
        </tr>
      </thead>
      <tbody>
        {vhosts.map(v => (
          <tr key={v.id}>
            <td><code>{v.id}</code></td>
            <td>{v.name}</td>
            <td>{v.enabled ? <Badge bg="success">{t('console.yes')}</Badge> : <Badge bg="secondary">{t('console.no')}</Badge>}</td>
            <td>{v.streams}</td>
            <td>{v.clients}</td>
            <td>{fmtBitrate(v.kbps?.recv_30s)}</td>
            <td>{fmtBitrate(v.kbps?.send_30s)}</td>
            <td>{v.hls?.enabled ? <Badge bg="success">{t('console.yes')}</Badge> : <Badge bg="secondary">{t('console.no')}</Badge>}</td>
          </tr>
        ))}
      </tbody>
    </Table>
  );
}

// ── Streams ──
function SrsStreams({handleError}) {
  const {t} = useTranslation();
  const [streams, setStreams] = React.useState();
  const [fps, setFps] = React.useState({});
  const streamsRef = React.useRef([]);

  React.useEffect(() => {
    let cancelled = false;
    let timer;
    let vhosts = [];

    const refresh = () => {
      // First fetch vhosts to join owner names, then streams.
      srsApi('/vhosts/').then((vd) => {
        vhosts = vd.vhosts || [];
        return srsApi('/streams/');
      }).then((sd) => {
        if (cancelled) return;
        const ownerOf = (vhostId) => {
          const v = vhosts.find(x => x.id === vhostId);
          return v ? v.name : vhostId;
        };
        const mapped = (sd.streams || []).map(s => ({...s, ownerName: ownerOf(s.vhost)}));
        streamsRef.current = mapped;
        setStreams(mapped);
        timer = setTimeout(refresh, 3000);
      }).catch((e) => {
        if (!cancelled) handleError(e);
      });
    };

    refresh();

    return () => { cancelled = true; if (timer) clearTimeout(timer); };
  }, [handleError]);

  const kickoff = React.useCallback((cid) => {
    axios.delete(`/api/v1/clients/${cid}`, {
      headers: Token.loadBearerHeader(),
    }).then(() => {
      // The next poll will refresh the list.
    }).catch(handleError);
  }, [handleError]);

  // Sample the fps of each publishing stream via the platform, throttled to once per
  // 10s, so the potentially abnormal streams (variable frame rate) are marked.
  const fpsCacheRef = React.useRef({});
  React.useEffect(() => {
    let cancelled = false;
    const timer = setInterval(() => {
      const now = Date.now();
      (streamsRef.current || []).filter(s => s.publish?.active).forEach(s => {
        const cachedAt = fpsCacheRef.current[s.id] || 0;
        if (now - cachedAt < 10000) return;
        fpsCacheRef.current[s.id] = now;
        axios.post('/terraform/v1/mgmt/streams/fps', {
          app: s.app, stream: s.name,
        }, {
          headers: Token.loadBearerHeader(),
        }).then(res => {
          if (cancelled) return;
          setFps(prev => ({...prev, [s.id]: res.data.data}));
        }).catch(() => {
          // Keep the previous result, the stream may just have stopped.
        });
      });
    }, 3000);
    return () => { cancelled = true; clearInterval(timer); };
  }, [handleError]);

  const abnormalCount = Object.values(fps).filter(f => f?.abnormal).length;

  const preview = React.useCallback((s) => {
    const schema = window.location.protocol.replace(':', '');
    const port = window.location.port || (schema === 'https' ? '443' : '80');
    const query = `schema=${schema}&port=${port}&vhost=${encodeURIComponent(s.ownerName)}&app=${encodeURIComponent(s.app)}&stream=${encodeURIComponent(s.name)}.flv&server=${window.location.hostname}&autostart=true`;
    window.open(`/players/srs_player.html?${query}`, '_blank');
  }, []);

  if (!streams) return <Alert variant="info">{t('console.loading')}</Alert>;

  return (
    <>
      {streams.length === 0 && <Alert variant="info">{t('console.noStreams')}</Alert>}
      {abnormalCount > 0 && (
        <Alert variant="warning">
          {t('console.fpsAbnormalAlert', {count: abnormalCount})}
        </Alert>
      )}
      <Table size="sm" striped bordered hover>
        <thead>
          <tr>
            <th>ID</th>
            <th>{t('console.name')}</th>
            <th>URL</th>
            <th>{t('console.vhosts')}</th>
            <th>{t('console.status')}</th>
            <th>{t('console.clients')}</th>
            <th>{t('console.in')}</th>
            <th>{t('console.out')}</th>
            <th>{t('console.video')}</th>
            <th>{t('console.audio')}</th>
            <th>{t('console.fps')}</th>
            <th>{t('console.manage')}</th>
          </tr>
        </thead>
        <tbody>
          {streams.map(s => {
            const fpsInfo = fps[s.id];
            return (
            <tr key={s.id} className={fpsInfo?.abnormal ? 'table-warning' : ''}>
              <td><code>{s.id}</code></td>
              <td>{s.name.length > 15 ? `${s.name.slice(0, 15)}…` : s.name}</td>
              <td style={{wordBreak: 'break-all'}}><code>{buildStreamUrl(s.ownerName, s)}</code></td>
              <td>{s.ownerName}</td>
              <td>{s.publish?.active ? <Badge bg="success">{t('console.publishing')}</Badge> : <Badge bg="secondary">{t('console.no')}</Badge>}</td>
              <td>{s.clients}</td>
              <td>{fmtBitrate(s.kbps?.recv_30s)}</td>
              <td>{fmtBitrate(s.kbps?.send_30s)}</td>
              <td style={{fontSize: '0.8em'}}>{fmtVideo(s.video)}</td>
              <td style={{fontSize: '0.8em'}}>{fmtAudio(s.audio)}</td>
              <td>
                {fpsInfo ? (
                  fpsInfo.abnormal ? (
                    <Badge bg="warning" title={t('console.fpsTooltip', {fps: fpsInfo.fps.toFixed(1), jitter: fpsInfo.jitterMs.toFixed(1)})}>
                      ⚠️ {t('console.fpsAbnormal')}
                    </Badge>
                  ) : (
                    <span title={t('console.fpsTooltip', {fps: fpsInfo.fps.toFixed(1), jitter: fpsInfo.jitterMs.toFixed(1)})}>
                      {fpsInfo.fps.toFixed(1)} fps
                    </span>
                  )
                ) : '-'}
              </td>
              <td>
                <Button size="sm" variant="outline-primary" className="me-1" onClick={() => preview(s)}>{t('console.preview')}</Button>
                <Button size="sm" variant="outline-danger" onClick={() => kickoff(s.publish?.cid)}>{t('console.kickoff')}</Button>
              </td>
            </tr>
            );
          })}
        </tbody>
      </Table>
    </>
  );
}

// ── Clients ──
function SrsClients({handleError}) {
  const {t} = useTranslation();
  const [clients, setClients] = React.useState();

  React.useEffect(() => {
    let cancelled = false;
    let timer;

    const refresh = () => {
      srsApi('/clients/').then((d) => {
        if (cancelled) return;
        setClients(d.clients || []);
        timer = setTimeout(refresh, 3000);
      }).catch((e) => {
        if (!cancelled) handleError(e);
      });
    };

    refresh();
    return () => { cancelled = true; if (timer) clearTimeout(timer); };
  }, [handleError]);

  const kickoff = React.useCallback((cid) => {
    axios.delete(`/api/v1/clients/${cid}`, {
      headers: Token.loadBearerHeader(),
    }).then(() => {}).catch(handleError);
  }, [handleError]);

  if (!clients) return <Alert variant="info">{t('console.loading')}</Alert>;

  return (
    <>
      {clients.length === 0 && <Alert variant="info">{t('console.noClients')}</Alert>}
      <Table size="sm" striped bordered hover>
        <thead>
          <tr>
            <th>ID</th>
            <th>IP</th>
            <th>{t('console.vhosts')}</th>
            <th>{t('console.streams')}</th>
            <th>{t('console.type')}</th>
            <th>{t('console.duration')}</th>
            <th>URL</th>
            <th>{t('console.category')}</th>
            <th>{t('console.manage')}</th>
          </tr>
        </thead>
        <tbody>
          {clients.map(c => (
            <tr key={c.id}>
              <td><code>{c.id}</code></td>
              <td>{c.ip}</td>
              <td>{c.vhost}</td>
              <td>{c.stream}</td>
              <td>{c.publish ? <Badge bg="danger">{t('console.publish')}</Badge> : <Badge bg="info">{t('console.play')}</Badge>}</td>
              <td>{fmtSec(c.alive)}</td>
              <td style={{wordBreak: 'break-all'}}><code>{buildStreamUrl(c.vhost, c)}</code></td>
              <td>{c.type}</td>
              <td>
                <Button size="sm" variant="outline-danger" onClick={() => kickoff(c.id)}>{t('console.kickoff')}</Button>
              </td>
            </tr>
          ))}
        </tbody>
      </Table>
    </>
  );
}

// ── Configs ──
function SrsConfigs({handleError}) {
  const {t} = useTranslation();
  const [cfg, setCfg] = React.useState();

  React.useEffect(() => {
    srsApi('/raw', {rpc: 'raw'}).then((d) => {
      setCfg(d.http_api);
    }).catch(handleError);
  }, [handleError]);

  if (!cfg) return <Alert variant="info">{t('console.loading')}</Alert>;

  const rows = [
    {key: 'http_api.enabled', value: cfg.enabled, desc: t('console.cfgEnabled')},
    {key: 'http_api.listen', value: cfg.listen, desc: t('console.cfgListen')},
    {key: 'http_api.crossdomain', value: cfg.crossdomain, desc: t('console.cfgCrossdomain')},
    {key: 'http_api.raw_api.enabled', value: cfg.raw_api?.enabled, desc: t('console.cfgRawEnabled')},
    {key: 'http_api.raw_api.allow_reload', value: cfg.raw_api?.allow_reload, desc: t('console.cfgRawReload')},
    {key: 'http_api.raw_api.allow_query', value: cfg.raw_api?.allow_query, desc: t('console.cfgRawQuery')},
    {key: 'http_api.raw_api.allow_update', value: cfg.raw_api?.allow_update, desc: t('console.cfgRawUpdate')},
  ];

  return (
    <>
      <Alert variant="warning">
        {t('console.rawApiRemoved')} <a href="https://github.com/ossrs/srs/issues/2653" target="_blank" rel="noreferrer">#2653</a>
      </Alert>
      <Table size="sm" striped bordered hover>
        <thead>
          <tr>
            <th>{t('console.key')}</th>
            <th>{t('console.value')}</th>
            <th>{t('console.desc')}</th>
            <th>{t('console.opt')}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(r => (
            <tr key={r.key}>
              <td><code>{r.key}</code></td>
              <td>{typeof r.value === 'boolean' ? (r.value ? <Badge bg="success">{t('console.yes')}</Badge> : <Badge bg="secondary">{t('console.no')}</Badge>) : String(r.value)}</td>
              <td style={{color: 'var(--bs-secondary-color)'}}>{r.desc}</td>
              <td><Badge bg="secondary">{t('console.readonly')}</Badge></td>
            </tr>
          ))}
        </tbody>
      </Table>
    </>
  );
}
