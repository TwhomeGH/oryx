import React, { useCallback, useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import axios from 'axios';
import { Token } from '../utils';

/**
 * Retained source-timing captures for one forward target. Each run stores the ORIGINAL source
 * PTS/DTS of every packet, which is exactly what a CFR transcode destroys, so these files are the
 * only way to trace what the source really did after the fact.
 *
 * @param {{platform: string}} props Target platform (the forward config key).
 */
export default function ForwardTiming({ platform }) {
  const { t } = useTranslation();
  const [runs, setRuns] = useState([]);

  const load = useCallback(() => {
    axios.post('/terraform/v1/ffmpeg/forward/secret', {
      action: 'timing', platform,
    }, {
      headers: Token.loadBearerHeader(),
    }).then(res => {
      setRuns(res.data.data || []);
    }).catch(() => setRuns([]));
  }, [platform]);

  useEffect(() => {
    load();
    const timer = setInterval(load, 60000);
    return () => clearInterval(timer);
  }, [load]);

  const when = (iso) => {
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? iso : d.toLocaleString();
  };
  const size = (n) => {
    if (!n) return '—';
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / 1024 / 1024).toFixed(2)} MB`;
  };

  if (runs.length === 0) {
    return <div className="small text-muted mt-3 pt-3 border-top">{t('forward.timing.empty')}</div>;
  }

  return <div className="mt-3 pt-3 border-top">
    <div className="small fw-bold mb-2">{t('forward.timing.title')}</div>
    <table className="small w-100">
      <tbody>
        {runs.map((r) => (
          <tr key={r.file || r.startedAt}>
            <td className="text-muted small text-nowrap">{when(r.startedAt)}</td>
            <td className="small text-end text-nowrap">{t('forward.timing.lines', {count: r.lines || 0})}</td>
            <td className="small text-end text-nowrap">{size(r.gzBytes || r.bytes)}</td>
            <td className="small text-end text-nowrap text-muted">{r.endedAt ? t('forward.timing.done') : t('forward.timing.live')}</td>
          </tr>
        ))}
      </tbody>
    </table>
    <div className="small text-muted mt-1">{t('forward.timing.hint')}</div>
  </div>;
}
