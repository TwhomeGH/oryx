//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import { Accordion, Badge, Button, Col, Form, Row, Table } from "react-bootstrap";
import React from "react";
import { Token , isZh} from "../utils";
import axios from "axios";
import moment from "moment";
import { useErrorHandler } from "react-error-boundary";
import { useSrsLanguage } from "../components/LanguageSwitch";
import { useTranslation } from "react-i18next";
import { SecretInput } from "../components/SecretInput";
import { SrsEnvContext } from "../components/SrsEnvContext";
import ForwardProgress from "../components/ForwardProgress";

export default function ScenarioForward() {
  const [init, setInit] = React.useState();
  const [activeKey, setActiveKey] = React.useState();
  const [secrets, setSecrets] = React.useState();
  const handleError = useErrorHandler();

  React.useEffect(() => {
    axios.post('/terraform/v1/ffmpeg/forward/secret', {
    }, {
      headers: Token.loadBearerHeader(),
    }).then(res => {
      const secrets = res.data.data;
      setInit(true);
      setSecrets(secrets || {});
      console.log(`Forward: Secret query ok ${JSON.stringify(secrets)}`);
    }).catch(handleError);
  }, [handleError]);

  React.useEffect(() => {
    if (!init || !secrets) return;

    if (secrets.wx?.enabled || secrets.bilibili?.enabled || secrets.kuaishou?.enabled) {
      return setActiveKey('99');
    }

    // Do not auto-expand a config card: the configs are set-and-forget, so open the STATUS
    // section instead and leave the config cards collapsed.
    setActiveKey('99');
  }, [init, secrets]);

  if (!activeKey) return <></>;
  return <ScenarioForwardImpl defaultActiveKey={activeKey} defaultSecrets={secrets} />;
}

function ScenarioForwardImpl({ defaultActiveKey, defaultSecrets }) {
  const language = useSrsLanguage();
  const { t } = useTranslation();
  const handleError = useErrorHandler();
  const env = React.useContext(SrsEnvContext)[0];

  const [configs, setConfigs] = React.useState([]);
  const [forwards, setForwards] = React.useState();
  // Active streams on SRS, for source suggestions and auto-selection hints.
  const [activeStreams, setActiveStreams] = React.useState();
  const [submiting, setSubmiting] = React.useState();
  // The accordion is controlled so the batch buttons (expand/collapse all) can drive it.
  const [activeKeys, setActiveKeys] = React.useState([defaultActiveKey]);
  const [masterEnabled, setMasterEnabled] = React.useState(true);

  // The global forward switch (pause/resume all without changing per-target preferences).
  // Declared after handleError, which it lists as a dependency (referring to it earlier is a
  // temporal dead zone error).
  React.useEffect(() => {
    axios.post('/terraform/v1/ffmpeg/forward/enabled', { action: 'query' }, {
      headers: Token.loadBearerHeader(),
    }).then(res => setMasterEnabled(res.data.data?.enabled !== false)).catch(handleError);
  }, [handleError]);

  // Convert default config from kv to objects in array.
  React.useEffect(() => {
    if (!defaultSecrets) return;

    let index = 1;
    const confs = [{
      platform: 'wx', enabled: false, index: String(index++), allowCustom: true,
      ...defaultSecrets?.wx,
      locale: {
        label: null, link: t('plat.wx.link'), link2: t('plat.wx.link2'),
        generate: (e) => {
          e.locale.label = e.custom ? t('plat.com.custom') : t('plat.wx.title');
        },
      },
    }, {
      platform: 'bilibili', enabled: false, index: String(index++), allowCustom: true,
      ...defaultSecrets?.bilibili,
      locale: {
        label: null, link: t('plat.bl.link'), link2: t('plat.bl.link2'),
        generate: (e) => {
          e.locale.label = e.custom ? t('plat.com.custom') : t('plat.bl.title');
        },
      },
    }, {
      platform: 'kuaishou', enabled: false, index: String(index++), allowCustom: true, custom: isZh(language),
      ...defaultSecrets?.kuaishou,
      locale: {
        label: null, link: t('plat.ks.link'), link2: t('plat.ks.link2'),
        generate: (e) => {
          e.locale.label = e.custom ? t('plat.com.custom') : t('plat.ks.title');
        },
      },
    }];

    // Regenerate the locale label, because it may change after created from defaults.
    confs.forEach((e) => {
      e?.locale?.generate && e.locale.generate(e);
    });

    // Load existing custom forwarding configures from defaults.
    // No empty slots are pre-created; user adds them on demand with the Add button.
    const customs = Object.values(defaultSecrets)
      .filter(e => e.platform.indexOf('forwarding-') === 0)
      .sort((a, b) => a.platform.localeCompare(b.platform));
    customs.forEach((e) => {
      confs.push({ ...e, index: String(index++), allowCustom: false });
    });

    setConfigs(confs);
    console.log(`Forward: Init configs ${JSON.stringify(confs)}`);
  }, [defaultSecrets, setConfigs, language, t]);

  // Fetch the forwarding streams from server.
  React.useEffect(() => {
    const refreshStreams = () => {
      axios.post('/terraform/v1/ffmpeg/forward/streams', {
      }, {
        headers: Token.loadBearerHeader(),
      }).then(res => {
        setForwards(res.data.data.map((e, i) => ({
          ...e,
          name: {
            wx: t('plat.wx.title'),
            bilibili: t('plat.bl.title'),
            kuaishou: t('plat.ks.title')
          }[e.platform],
          start: e.start ? moment(e.start) : null,
          ready: e.ready ? moment(e.ready) : null,
          update: e.frame?.update ? moment(e.frame.update) : null,
          lastErrorAt: e.lastErrorAt ? moment(e.lastErrorAt) : null,
          nextRetryAt: e.nextRetryAt ? moment(e.nextRetryAt) : null,
          i,
        })));
        console.log(`Forward: Query streams ${JSON.stringify(res.data.data)}`);
      }).catch(handleError);
    };

    refreshStreams();
    const timer = setInterval(() => refreshStreams(), 10 * 1000);
    return () => clearInterval(timer);
  }, [t, handleError, setForwards]);

  // Fetch active streams periodically, to suggest source candidates.
  React.useEffect(() => {
    const refreshActive = () => {
      axios.post('/terraform/v1/mgmt/streams/query', {}, {
        headers: Token.loadBearerHeader(),
      }).then(res => {
        setActiveStreams(res.data.data?.streams || []);
      }).catch(e => {
        console.log('ignore error during streams query', e);
      });
    };

    refreshActive();
    const timer = setInterval(refreshActive, 10 * 1000);
    return () => clearInterval(timer);
  }, [setActiveStreams]);

  // Hint for empty source stream: show which stream it would forward,
  // mimicking the backend selectActiveStream logic (latest update wins).
  const autoSourceHint = (conf) => {
    const running = (forwards || []).find(f => f.platform === conf.platform);
    if (running?.input) {
      return `${t('forward.currentSource')}: ${running.input}`;
    }
    const actives = [...(activeStreams || [])].filter(s => s.update)
      .sort((a, b) => new Date(b.update) - new Date(a.update));
    if (actives.length > 0) {
      return `${t('forward.autoSource')}, ${t('forward.currentSource')}: ${actives[0].stream}`;
    }
    return t('forward.autoSource');
  };
  const forwardStatus = (file) => {
    if (!file.enabled) return {bg: 'secondary', label: t('plat.com.s2')};

    switch (file.status) {
      case 'running':
        return file.ready
          ? {bg: 'success', label: t('forward.progress.outputReported')}
          : {bg: 'primary', label: t('forward.progress.starting')};
      case 'waiting_input':
        return {bg: 'primary', label: t('forward.status.waitingInput')};
      case 'retrying':
        return {bg: 'warning', label: t('forward.status.retrying')};
      case 'degraded':
        return {bg: 'danger', label: t('forward.status.degraded')};
      case 'idle':
        return {bg: 'secondary', label: t('forward.status.idle')};
      default:
        return {bg: 'primary', label: t('forward.progress.unknown')};
    }
  };
  const updateConfigObject = React.useCallback((conf) => {
    const confs = configs.map((e) => {
      if (e.platform === conf.platform) {
        return conf;
      }
      return e;
    })
    setConfigs(confs);
    console.log(`Forward: Update config ${JSON.stringify(conf)} to ${JSON.stringify(confs)}`);
  }, [configs, setConfigs]);

  // Update the forward config to server.
  const updateSecrets = React.useCallback((e, action, platform, stream, server, secret, enabled, custom, label, failover, normalize, onSuccess) => {
    e.preventDefault();
    if (!server) return alert(t('plat.com.addr'));
    if (custom && !label) return alert(t('plat.com.label'));

    try {
      setSubmiting(true);

      axios.post('/terraform/v1/ffmpeg/forward/secret', {
        action, platform, stream, server, secret, enabled: !!enabled, custom: !!custom, label, failover, normalize: !!normalize,
      }, {
        headers: Token.loadBearerHeader(),
      }).then(res => {
        alert(t('plat.com.ok'));
        onSuccess && onSuccess();
      }).catch(handleError);
    } finally {
      new Promise(resolve => setTimeout(resolve, 3000)).then(() => setSubmiting(false));
    }
  }, [t, handleError, setSubmiting]);

  // Toggle the global forward switch: pauses/resumes ALL forwards without changing any target's
  // own enabled preference (which stays the user's chosen set). Persisted server-side.
  const updateMaster = React.useCallback((enabled) => {
    setSubmiting(true);
    axios.post('/terraform/v1/ffmpeg/forward/enabled', { action: 'update', enabled: !!enabled }, {
      headers: Token.loadBearerHeader(),
    }).then(() => {
      setMasterEnabled(!!enabled);
    }).catch(handleError).finally(() => {
      new Promise((resolve) => setTimeout(resolve, 1500)).then(() => setSubmiting(false));
    });
  }, [handleError]);

  // Append a new empty custom forwarding config, persisted when user starts it.
  const addForwarding = React.useCallback(() => {
    const customs = configs.filter(e => e.platform.indexOf('forwarding-') === 0);
    if (customs.length >= (env?.forwardLimit || 10)) return alert(t('forward.limit'));

    const rid = Math.random().toString(16).slice(-6);
    const rindex = configs.length ? Math.max(...configs.map(e => Number(e.index) || 0)) + 1 : 1;
    setConfigs([...configs, {
      platform: `forwarding-${Date.now().toString(36)}-${rid}`, enabled: false, index: String(rindex), allowCustom: false,
      server: null, secret: null, custom: true, label: `${t('forward.defaultLabel')} ${rindex}`, failover: '', normalize: false,
    }]);
  }, [configs, env, t]);

  // Delete the custom forwarding config from server, then remove it from UI.
  const removeForwarding = React.useCallback((e, conf) => {
    e.preventDefault();
    if (!window.confirm(t('forward.removeConfirm'))) return;

    axios.post('/terraform/v1/ffmpeg/forward/secret', {
      action: 'delete', platform: conf.platform,
    }, {
      headers: Token.loadBearerHeader(),
    }).then(res => {
      setConfigs(configs.filter(x => x.platform !== conf.platform));
      alert(t('forward.deleted'));
    }).catch(handleError);
  }, [configs, t, handleError]);

  return (<div>
    <div className="d-flex flex-wrap gap-2 my-2">
      <Button size="sm" variant="outline-secondary" onClick={() => setActiveKeys(configs.map((c) => c.index).concat(['0', '99']))}>{t('forward.expandAll')}</Button>
      <Button size="sm" variant="outline-secondary" onClick={() => setActiveKeys([])}>{t('forward.collapseAll')}</Button>
      <span className="ms-2 align-self-center small text-muted" title={t('forward.masterHint')}>{t('forward.master')}:</span>
      <Button size="sm" variant={masterEnabled ? 'success' : 'outline-success'} disabled={submiting} title={t('forward.masterHint')} onClick={() => updateMaster(true)}>{t('forward.startAll')}</Button>
      <Button size="sm" variant={!masterEnabled ? 'danger' : 'outline-danger'} disabled={submiting} title={t('forward.masterHint')} onClick={() => updateMaster(false)}>{t('forward.stopAll')}</Button>
      <span className="align-self-center small text-muted">{t('forward.masterHint')}</span>
    </div>
    <Accordion activeKey={activeKeys} onSelect={(k) => setActiveKeys(k || [])} alwaysOpen>
      <React.Fragment>
        {isZh(language) ?
          <Accordion.Item eventKey="0">
            <Accordion.Header>场景介绍</Accordion.Header>
            <Accordion.Body>
              <div>
                多平台转播，将流转播给其他平台，比如视频号直播、快手、B站等。
                <p></p>
              </div>
              <p>可应用的具体场景包括：</p>
              <ul>
                <li>节约上行带宽，避免客户端推多路流，服务器转播更有保障</li>
              </ul>
              <p>使用说明：</p>
              <ul>
                <li>首先使用适合你的场景推流</li>
                <li>然后设置转播的平台</li>
              </ul>
            </Accordion.Body>
          </Accordion.Item> :
          <Accordion.Item eventKey="0">
            <Accordion.Header>Introduction</Accordion.Header>
            <Accordion.Body>
              <div>
                Multi-platform streaming, forward to other platforms, such as YouTube, Twitch, TikTok, etc.
                <p></p>
              </div>
              <p>Specific application scenarios include:</p>
              <ul>
                <li>Save on upstream bandwidth, avoid pushing multiple streams from the client, and server streaming is more secure</li>
              </ul>
              <p>Usage instructions:</p>
              <ul>
                <li>First, use the appropriate streaming method for your scenario</li>
                <li>Then set the platform for streaming</li>
              </ul>
            </Accordion.Body>
          </Accordion.Item>}
      </React.Fragment>
      {configs.map((conf) => {
        return (
          <Accordion.Item eventKey={conf.index} key={conf.platform}>
            <Accordion.Header>{conf?.locale?.label} {conf.label || (conf.custom && conf.platform.indexOf('forwarding-') === 0 ? `${t('plat.com.custom')} #${conf.index}` : '')}</Accordion.Header>
            <Accordion.Body>
              <Form>
                <Form.Group className="mb-3">
                  <Form.Label>{t('plat.com.name')}</Form.Label>
                  <Form.Text> * {conf.custom ? `(${t('helper.required')})` : `(${t('helper.optional')})`} {t('plat.com.name2')}</Form.Text>
                  <Form.Control as="input" defaultValue={conf.label} onChange={(e) => updateConfigObject({ ...conf, label: e.target.value })} />
                </Form.Group>
                <Form.Group className="mb-3">
                  <Form.Label>{t('plat.com.source')}</Form.Label>
                  {!conf.custom && <Form.Text> * {t('plat.com.source')} check System-{'>'}Streams tab</Form.Text>}
                  <Form.Control as="input" list={`sourceOptions-${conf.platform}`} defaultValue={conf.stream} onChange={(e) => updateConfigObject({ ...conf, stream: e.target.value })} />
                  <datalist id={`sourceOptions-${conf.platform}`}>
                    {(activeStreams || []).map((s, i) => (
                      <option key={`${s.app}/${s.stream}-${i}`} value={s.stream} />
                    ))}
                  </datalist>
                  {!conf.stream && (
                    <Form.Text style={{wordBreak: 'break-all'}}>
                      * {autoSourceHint(conf)}
                    </Form.Text>
                  )}
                </Form.Group>
                <Form.Group className="mb-3">
                  <Form.Label>{conf.custom ? t('plat.com.server') : t('plat.com.server2')}</Form.Label>
                  {!conf.custom && <Form.Text> * {t('plat.com.server3')} <a href={conf?.locale?.link} target='_blank' rel='noreferrer'>{conf?.locale?.link2}</a>, {t('plat.com.server4')}</Form.Text>}
                  <Form.Control as="input" defaultValue={conf.server} onChange={(e) => updateConfigObject({ ...conf, server: e.target.value })} />
                </Form.Group>
                <Form.Group className="mb-3">
                  <Form.Label>{t('plat.com.key')}</Form.Label>
                  {!conf.custom && <Form.Text> * {t('plat.com.server3')} <a href={conf?.locale?.link} target='_blank' rel='noreferrer'>{conf?.locale?.link2}</a>, {t('plat.com.key2')}</Form.Text>}
                  <SecretInput value={conf.secret} onChange={(v) => updateConfigObject({ ...conf, secret: v })} />
                </Form.Group>
                <Form.Group className="mb-3">
                  <Form.Label>{t('forward.failover')}</Form.Label>
                  <Form.Text> * {t('forward.failoverHint')}</Form.Text>
                  <Form.Select defaultValue={conf.failover || ''} onChange={(e) => updateConfigObject({ ...conf, failover: e.target.value })}>
                    <option value=''>{t('forward.failoverHold')}</option>
                    <option value='slate'>{t('forward.failoverSlate')}</option>
                  </Form.Select>
                  <pre className="small text-muted mt-1 mb-0" style={{whiteSpace: 'pre-wrap'}}>
{`slate: [base=color+drawtext 時鐘][source]overlay=eof_action=pass + [silence][src]amix
       h264_nvenc|libx264 -preset ... -g <keyframeSeconds × 來源 fps> -c:a aac
hold : -c copy（不轉碼，斷源時凍結最後一格）`}
                  </pre>
                </Form.Group>
                {conf?.allowCustom && (
                  <Row>
                    <Col xs='auto'>
                      <Form.Group className="mb-3" controlId={`formCustomCheckbox-${conf.platform}`}>
                        <Form.Check type="checkbox" label={t('plat.com.custom')} checked={!!conf.custom} onChange={() => updateConfigObject({ ...conf, custom: !conf.custom })} />
                      </Form.Group>
                    </Col>
                  </Row>
                )}
                <Button
                  variant="primary"
                  type="submit"
                  disabled={submiting}
                  onClick={(e) => {
                    updateSecrets(e, 'update', conf.platform, conf.stream, conf.server, conf.secret, !conf.enabled, conf.custom, conf.label, conf.failover || '', !!conf.normalize, () => {
                      updateConfigObject({ ...conf, enabled: !conf.enabled });
                    });
                  }}
                >
                  {conf.enabled ? t('plat.com.stop') : t('plat.com.start')}
                </Button> &nbsp;
                {conf.platform.indexOf('forwarding-') === 0 && (
                  <Button variant="danger" disabled={submiting} onClick={(e) => removeForwarding(e, conf)}>
                    {t('forward.remove')}
                  </Button>
                )} &nbsp;
                <Form.Text> * {t('forward.tip')}</Form.Text>
              </Form>
            </Accordion.Body>
          </Accordion.Item>
        );
      })}
      <div className="text-end my-2 me-2">
        <Button variant="outline-primary" onClick={addForwarding}>{t('forward.add')}</Button>
      </div>
      <Accordion.Item eventKey="99">
        <Accordion.Header>{t('plat.com.status')}</Accordion.Header>
        <Accordion.Body>
          <p className="text-muted small">{t('forward.progress.hint')}</p>
          <p className="text-muted small">{t('forward.progress.destination')}</p>
          {
            forwards?.length ? (
              <Table striped bordered hover responsive>
                <thead>
                  <tr>
                    <th>#</th>
                    <th>{t('plat.com.platform')}</th>
                    <th>{t('plat.com.status2')}</th>
                    <th>{t('forward.progress.start')}</th>
                    <th>{t('forward.progress.ready')}</th>
                    <th>{t('forward.progress.update')}</th>
                    <th>{t('plat.com.source')}</th>
                    <th>{t('forward.restarts')}</th>
                    <th>{t('forward.nextRetry')}</th>
                    <th>{t('forward.lastError')}</th>
                    <th>{t('forward.progress.title')}</th>
                  </tr>
                </thead>
                <tbody>
                  {
                    forwards?.map(file => {
                      const status = forwardStatus(file);
                      return <tr key={file.platform} style={{ verticalAlign: 'middle' }}>
                        <td>{file.i}</td>
                        <td>{file.custom ? (file.label ? '' : t('plat.com.custom')) : file.name} {file.label}</td>
                        <td>
                          <Badge bg={status.bg}>{status.label}</Badge>
                          {file.fallback && <div className="mt-1">
                            <Badge bg="dark">{t('forward.fallback')}</Badge>
                          </div>}
                          {!!file.consecutiveFailures && <div className="small text-muted">
                            {t('forward.failures')}: {file.consecutiveFailures}
                          </div>}
                        </td>
                        <td>{file.start && `${file.start?.format('YYYY-MM-DD HH:mm:ss')}`}</td>
                        <td>{file.ready && `${file.ready?.format('YYYY-MM-DD HH:mm:ss')}`}</td>
                        <td>{file.update && `${file.update?.format('YYYY-MM-DD HH:mm:ss')}`}</td>
                        <td>{file.stream}</td>
                        <td>{file.restartCount || 0}</td>
                        <td>{file.nextRetryAt && `${file.nextRetryAt?.format('YYYY-MM-DD HH:mm:ss')}`}</td>
                        <td style={{ maxWidth: 280, wordBreak: 'break-word' }}>
                          {file.lastError}
                          {file.lastErrorAt && <div className="small text-muted">
                            {file.lastErrorAt?.format('YYYY-MM-DD HH:mm:ss')}
                          </div>}
                        </td>
                        <td style={{ maxWidth: 360, wordBreak: 'break-word' }}><ForwardProgress log={file.frame?.log} metrics={file.metrics} events={file.events} logs={file.logs} encoder={file.encoder} ffmpegArgs={file.ffmpegArgs} programSourceRate={file.programSourceRate} /></td>
                      </tr>;
                    })
                  }
                </tbody>
              </Table>
            ) : ''
          }
          {!forwards?.length ? t('forward.s3') : ''}
        </Accordion.Body>
      </Accordion.Item>
    </Accordion>
  </div>);
}
