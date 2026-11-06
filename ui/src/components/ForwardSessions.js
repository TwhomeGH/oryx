import React, { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import axios from 'axios';
import { Token } from '../utils';

/**
 * Persisted run history for one forward target: the last few runs that actually produced output,
 * with their events and recent logs. This is what makes a failure investigable after the fact —
 * the in-memory diagnostic timeline only covers the current run and is lost on restart.
 *
 * @param {{platform: string}} props Target platform (the forward config key).
 */
export default function ForwardSessions({ platform }) {
  const { t } = useTranslation();
  const [sessions, setSessions] = useState([]);
  const [error, setError] = useState('');
  const [open, setOpen] = useState('');

  const load = useCallback(() => {
    axios.post('/terraform/v1/ffmpeg/forward/secret', {
      action: 'sessions', platform,
    }, {
      headers: Token.loadBearerHeader(),
    }).then(res => {
      setSessions(res.data.data || []);
      setError('');
    }).catch(() => setError(t('forward.sessions.error')));
  }, [platform, t]);

  useEffect(() => {
    load();
    const timer = setInterval(load, 30000);
    return () => clearInterval(timer);
  }, [load]);

  const when = (iso) => {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
  };

  if (error) {
    return <div className="small text-muted mt-3 pt-3 border-top">{error}</div>;
  }
  if (sessions.length === 0) {
    return <div className="small text-muted mt-3 pt-3 border-top">{t('forward.sessions.empty')}</div>;
  }

  return (
    <div className="mt-3 pt-3 border-top">
      <div className="small fw-bold mb-2">{t('forward.sessions.title')}</div>
      <table className="small w-100">
        <tbody>
          {sessions.map((s) => (
            <React.Fragment key={s.id}>
              <tr style={{cursor: 'pointer'}} onClick={() => setOpen(open === s.id ? '' : s.id)}>
                <td className="text-muted small">{when(s.startedAt)}</td>
                <td className="small text-end text-nowrap">{Math.round(s.durationSec)}s</td>
                <td className="small text-end text-nowrap">{s.finalState}</td>
                <td className="small text-end text-nowrap text-muted">
                  {t('forward.sessions.restarts', {count: s.restartCount})}
                </td>
              </tr>
              {open === s.id && (
                <tr>
                  <td colSpan={4} className="p-2 border-top">
                    {s.lastError && <div className="small text-danger mb-1">{s.lastError}</div>}
                    <div className="small text-muted mb-1">{t('forward.sessions.timeline')}</div>
                    <div className="small mb-2" style={{maxHeight: 160, overflowY: 'auto'}}>
                      {(s.events || []).map((e, i) => (
                        <div key={i} className="text-nowrap">
                          <span className="text-muted">{new Date(e.at).toLocaleTimeString()} </span>
                          <b>{e.name}</b> {e.detail}
                        </div>
                      ))}
                    </div>
                    <div className="small text-muted mb-1">{t('forward.sessions.logs')}</div>
                    <div className="small font-monospace" style={{maxHeight: 200, overflowY: 'auto'}}>
                      {(s.logs || []).map((l, i) => <div key={i} className="text-nowrap">{l}</div>)}
                    </div>
                  </td>
                </tr>
              )}
            </React.Fragment>
          ))}
        </tbody>
      </table>
    </div>
  );
}
