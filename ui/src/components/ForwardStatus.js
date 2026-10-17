//
// Copyright (c) 2022-2026 Winlin
//
// SPDX-License-Identifier: MIT
//
import React from 'react';
import {Badge, Popover, OverlayTrigger} from 'react-bootstrap';
import {Link} from 'react-router-dom';
import axios from 'axios';
import {Token} from '../utils';
import {useTranslation} from 'react-i18next';

// Always-visible navbar indicator of the multi-platform forwarding state, so the status is
// visible on every page instead of only inside Scenario -> Forward (which requires switching
// pages and expanding an accordion). Clicking opens a popover with the per-target state and the
// global pause switch. It never changes any target's own enabled preference.
export default function ForwardStatus() {
  const [forwards, setForwards] = React.useState([]);
  const [master, setMaster] = React.useState(true);
  const {t} = useTranslation();

  const refresh = React.useCallback(() => {
    axios.post('/terraform/v1/ffmpeg/forward/streams', {}, {headers: Token.loadBearerHeader()})
      .then(res => setForwards(res.data.data || [])).catch(() => setForwards([]));
    axios.post('/terraform/v1/ffmpeg/forward/enabled', {action: 'query'}, {headers: Token.loadBearerHeader()})
      .then(res => setMaster(res.data.data?.enabled !== false)).catch(() => {});
  }, []);

  React.useEffect(() => {
    refresh();
    const timer = setInterval(refresh, 10000);
    return () => clearInterval(timer);
  }, [refresh]);

  const total = forwards.length;
  const running = forwards.filter(e => e.status === 'running').length;
  const anyFallback = forwards.some(e => e.fallback);
  const variant = !master ? 'danger' : (anyFallback ? 'warning' : (running > 0 ? 'success' : 'secondary'));

  // A target name: built-in platforms show their name, custom targets their label.
  const name = (e) => e.custom ? (e.label || t('plat.com.custom')) : (e.platform || '');

  return (
    <OverlayTrigger trigger="click" placement="bottom-end" rootClose overlay={
      <Popover style={{maxWidth: 420}}>
        <Popover.Header as="h6">{t('forward.statusTitle')}</Popover.Header>
        <Popover.Body>
          <div className="mb-2">
            <Badge bg={master ? 'success' : 'secondary'}>{t('forward.master')}: {master ? t('forward.statusOn') : t('forward.statusOff')}</Badge>
            <span className="ms-2 small text-muted">{t('forward.statusCount')}: {running}/{total}</span>
          </div>
          {total === 0 ? <div className="small text-muted">{t('forward.statusNone')}</div> : (
            <div className="small">
              {forwards.map((e, i) => (
                <div key={i} className="d-flex justify-content-between border-top py-1">
                  <span className="text-truncate me-2">{name(e)}</span>
                  <span>
                    <Badge bg={e.status === 'running' ? 'success' : 'secondary'}>{e.status || '-'}</Badge>
                    {e.fallback && <Badge bg="dark" className="ms-1">{t('forward.fallback')}</Badge>}
                  </span>
                </div>
              ))}
            </div>
          )}
          <div className="mt-2 text-end">
            <Link to="/routers-scenario?tab=forward" className="small">{t('forward.statusOpen')}</Link>
          </div>
        </Popover.Body>
      </Popover>
    }>
      <a role="button" className="text-decoration-none align-self-center me-2" title={t('forward.statusTitle')}>
        <Badge bg={variant}>{master ? `${t('forward.statusShort')} ${running}/${total}` : t('forward.statusPaused')}</Badge>
      </a>
    </OverlayTrigger>
  );
}
