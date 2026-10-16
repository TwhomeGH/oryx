import React from 'react';
import {useTranslation} from 'react-i18next';

/** Format a finite number to one decimal, or an em dash when unavailable. */
function fmt(value) {
  return Number.isFinite(value) ? value.toFixed(1) : '—';
}

/**
 * Extract FFmpeg's cumulative processing statistics, not source frame rate.
 * @param {string} log Latest FFmpeg progress line.
 * @returns {{fps: string|null, speed: string|null, time: string|null}} Missing/invalid values remain null.
 */
export function parseForwardProgress(log = '') {
  const token = (key) => String(log).match(new RegExp(`(?:^|\\s)${key}=\\s*(\\S+)`))?.[1];
  const fps = token('fps');
  const speed = token('speed');
  const time = token('time');
  return {
    fps: /^\d+(?:\.\d+)?$/.test(fps) ? fps : null,
    speed: /^\d+(?:\.\d+)?x$/.test(speed) ? speed : null,
    time: /^\d+:\d{2}:\d{2}(?:\.\d+)?$/.test(time) ? time : null,
  };
}

/** A tiny inline trend line of recent average FPS samples, or null when there is not enough data. */
function Sparkline({data}) {
  if (!data || data.length < 2) {
    return null;
  }
  const max = Math.max(...data);
  const min = Math.min(...data);
  const range = max - min || 1;
  const width = 120;
  const height = 24;
  const points = data.map((v, i) => `${(i / (data.length - 1)) * width},${height - ((v - min) / range) * height}`).join(' ');
  return <svg width={width} height={height} className="text-primary" role="img" aria-label="fps trend">
    <polyline points={points} fill="none" stroke="currentColor" strokeWidth="1" />
  </svg>;
}

/**
 * Show local processing evidence without asserting destination playback health.
 * @param {{log?: string, metrics?: object, events?: Array, logs?: Array}} props Latest progress
 *   log, the metrics object, and the diagnostics timeline / recent logs of the target.
 * @returns {React.ReactElement} Local progress summary and raw diagnostics.
 */
export default function ForwardProgress({log, metrics, events, logs, encoder}) {
  const {t} = useTranslation();
  const progress = parseForwardProgress(log);
  // Hardware encoders (NVENC/VAAPI/QSV/AMF/VideoToolbox) vs software (libx264).
  const encHardware = /nvenc|vaapi|qsv|amf|videotoolbox/.test(encoder || '');
  const history = React.useRef([]);

  // Accumulate a short in-memory trend of the average FPS. The guard keeps it append-only and
  // idempotent across the parent's periodic re-renders.
  const fps = metrics?.avgFps;
  if (typeof fps === 'number' && fps > 0 && history.current[history.current.length - 1] !== fps) {
    history.current.push(fps);
    if (history.current.length > 40) {
      history.current.shift();
    }
  }

  return <div>
    <div>{t('forward.progress.fps')}: {progress.fps ?? '—'}</div>
    <div>{t('forward.progress.speed')}: {progress.speed ?? '—'}</div>
    <div>{t('forward.progress.time')}: {progress.time ?? '—'}</div>
    {metrics && <>
      <div>{t('forward.progress.avgFps')}: {fmt(metrics.avgFps)}</div>
      <div>{t('forward.progress.noOutput')}: {fmt(metrics.noOutputSeconds)}s</div>
      <div>{t('forward.progress.drift')}: {fmt(metrics.mediaClockDriftSec)}s</div>
      {metrics.relayReconnects > 0 && <div>{t('forward.progress.reconnects')}: {metrics.relayReconnects}</div>}
      {encoder && <div>{t('forward.progress.encoder')}: {encoder}{encHardware ? ` ${t('forward.progress.gpu')}` : ''}</div>}
    </>}
    <Sparkline data={history.current} />
    {log && <details className="mt-1">
      <summary>{t('forward.progress.raw')}</summary>
      <pre className="small mb-0" style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{log}</pre>
    </details>}
    {events?.length > 0 && <details className="mt-1">
      <summary>{t('forward.progress.events')}</summary>
      <ul className="small mb-0 ps-3">
        {events.map((e, i) => <li key={i}>{e.at} {e.name}{e.detail ? `: ${e.detail}` : ''}</li>)}
      </ul>
    </details>}
    {logs?.length > 0 && <details className="mt-1">
      <summary>{t('forward.progress.logs')}</summary>
      <pre className="small mb-0" style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{logs.join('\n')}</pre>
    </details>}
  </div>;
}
