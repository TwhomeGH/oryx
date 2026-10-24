import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import axios from 'axios';
import {Token} from '../utils';

/**
 * A fixed-scale line chart with an optional safe band. Unlike a plain sparkline this keeps a
 * stable vertical scale (so 0.1s and 1s changes look different) and draws the value range.
 *
 * @param {{values: number[], color: string, min: number, max: number, band?: [number, number],
 *          unit: string, height?: number}} props Chart data and scale.
 */
function ScaleChart({values, color, min, max, band, unit, height = 64}) {
  if (!values || values.length < 2) {
    return <div className="text-muted small">—</div>;
  }
  const width = 320;
  const span = max - min || 1;
  const y = v => height - ((Math.min(Math.max(v, min), max) - min) / span) * height;
  const points = values.map((v, i) => `${((i / (values.length - 1)) * width).toFixed(1)},${y(v).toFixed(1)}`).join(' ');
  const bandRect = band
    ? { y: y(band[1]), h: Math.max(1, y(band[0]) - y(band[1])) }
    : null;
  const fmt = v => (Math.abs(v) >= 100 ? Math.round(v) : v.toFixed(1)).replace(/\.0$/, '');

  return (
    <div className="d-inline-flex align-items-stretch">
      {/* Vertical scale: max at the top, min at the bottom. */}
      <div className="d-flex flex-column justify-content-between text-end text-muted small pe-1" style={{height}}>
        <span>{fmt(max)}{unit}</span>
        <span>{fmt(min)}{unit}</span>
      </div>
      <svg width={width} height={height} viewBox={`0 0 ${width} ${height}`} preserveAspectRatio="none">
        {bandRect && <rect x="0" y={bandRect.y} width={width} height={bandRect.h} fill="rgba(25,135,84,0.15)" />}
        <line x1="0" y1={y(0)} x2={width} y2={y(0)} stroke="#adb5bd" strokeWidth="0.5" strokeDasharray="2 2" />
        <polyline fill="none" stroke={color} strokeWidth="1.5" points={points} />
      </svg>
    </div>
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
          // Drop startup samples where the encoder had not produced frames yet (fps 0).
          setSamples((res.data?.data || []).filter(s => Number(s.fps) > 0 && Number(s.frame) > 0));
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

  const series = key => samples.map(s => Number(s[key]) || 0);
  const stats = key => {
    const v = series(key);
    return {min: Math.min(...v), max: Math.max(...v), last: v[v.length - 1],
            avg: v.reduce((a, b) => a + b, 0) / v.length};
  };
  // Fixed scales: +/-1s for drift (0.5s = safe), a fixed fps window around 60, and a stable
  // bitrate window so small changes are visible but the chart is not dominated by outliers.
  const drift = stats('drift');
  const fps = stats('fps');
  const kbps = stats('kbps');
  // Fixed scale first; only widen when a sample genuinely exceeds it.
  const driftMin = Math.min(-1, Math.floor(drift.min));
  const driftMax = Math.max(1, Math.ceil(drift.max));
  const fpsMin = Math.min(55, Math.floor(fps.min));
  const fpsMax = Math.max(65, Math.ceil(fps.max));
  const kbpsMin = Math.floor(Math.min(kbps.min * 0.9, kbps.avg * 0.8) / 100) * 100;
  const kbpsMax = Math.ceil(Math.max(kbps.max * 1.1, kbps.avg * 1.2) / 100) * 100;
  const first = samples[0].t?.slice(11, 19);
  const last = samples[samples.length - 1].t?.slice(11, 19);

  const row = (label, key, color, min, max, band, unit) => {
    const s = stats(key);
    return (
      <tr>
        <td className="pe-2 text-muted text-nowrap">{label}</td>
        <td><ScaleChart values={series(key)} color={color} min={min} max={max} band={band} unit={unit} /></td>
        <td className="ps-2 small text-nowrap">
          {s.last.toFixed(2)}{unit}
          <div className="text-muted" style={{fontSize: '0.72em'}}>
            {t('forward.history.range', {min: s.min.toFixed(2), max: s.max.toFixed(2)})}
          </div>
        </td>
      </tr>
    );
  };

  return (
    <div className="mt-3 pt-3 border-top">
      <div className="small fw-bold">{t('forward.history.title')}
        <span className="text-muted fw-normal ps-2">{first} – {last}</span>
      </div>
      <table className="small mb-1">
        <tbody>
          {row(t('forward.history.drift'), 'drift', '#dc3545', driftMin, driftMax, [-0.5, 0.5], 's')}
          {row(t('forward.history.fps'), 'fps', '#0d6efd', fpsMin, fpsMax, [59, 61], '')}
          {row(t('forward.history.bitrate'), 'kbps', '#198754', kbpsMin, kbpsMax, null, 'k')}
        </tbody>
      </table>
      <div className="small text-muted">
        {t('forward.history.samples', {count: samples.length})}
        <span className="ps-2">{t('forward.history.band')}</span>
      </div>
    </div>
  );
}
