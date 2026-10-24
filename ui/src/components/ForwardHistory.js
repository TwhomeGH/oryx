import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import axios from 'axios';
import {Token} from '../utils';

/**
 * A compact line chart of a numeric series drawn as an inline SVG sparkline.
 * @param {{values: number[], color: string, height?: number}} props Series values and style.
 */
function Sparkline({values, color, height = 40}) {
  if (!values || values.length < 2) {
    return <div className="text-muted small">—</div>;
  }
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = max - min || 1;
  const width = 240;
  const points = values.map((v, i) => {
    const x = (i / (values.length - 1)) * width;
    const y = height - ((v - min) / span) * height;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  }).join(' ');
  return (
    <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none">
      <polyline fill="none" stroke={color} strokeWidth="1.5" points={points} />
    </svg>
  );
}

/** Shows the sampled pipeline history (drift / fps / bitrate) for one forward target. */
export default function ForwardHistory({platform}) {
  const {t} = useTranslation();
  const [samples, setSamples] = useState([]);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      try {
        const res = await axios.post('/terraform/v1/ffmpeg/forward/secret',
          {action: 'metrics', platform}, {headers: Token.loadBearerHeader()});
        if (!cancelled) {
          setSamples(res.data?.data || []);
          setError('');
        }
      } catch (e) {
        if (!cancelled) setError(e.message || 'failed');
      }
    };
    load();
    const timer = setInterval(load, 10000);
    return () => { cancelled = true; clearInterval(timer); };
  }, [platform]);

  if (error) {
    return <div className="small text-danger">{error}</div>;
  }
  if (samples.length < 2) {
    return <div className="small text-muted">{t('forward.history.empty')}</div>;
  }

  const drift = samples.map(s => Number(s.drift) || 0);
  const fps = samples.map(s => Number(s.fps) || 0);
  const kbps = samples.map(s => Number(s.kbps) || 0);
  const last = samples[samples.length - 1];

  return (
    <div className="mt-2">
      <div className="small fw-bold">{t('forward.history.title')}</div>
      <table className="small">
        <tbody>
          <tr>
            <td className="pe-2 text-muted">{t('forward.history.drift')}</td>
            <td><Sparkline values={drift} color="#dc3545" /></td>
            <td className="ps-2">{Number(last.drift).toFixed(2)}s</td>
          </tr>
          <tr>
            <td className="pe-2 text-muted">{t('forward.history.fps')}</td>
            <td><Sparkline values={fps} color="#0d6efd" /></td>
            <td className="ps-2">{Number(last.fps).toFixed(1)}</td>
          </tr>
          <tr>
            <td className="pe-2 text-muted">{t('forward.history.bitrate')}</td>
            <td><Sparkline values={kbps} color="#198754" /></td>
            <td className="ps-2">{Number(last.kbps).toFixed(0)}k</td>
          </tr>
        </tbody>
      </table>
      <div className="small text-muted">{t('forward.history.samples', {count: samples.length})}</div>
    </div>
  );
}