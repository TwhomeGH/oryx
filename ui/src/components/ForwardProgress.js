import React from 'react';
import {useTranslation} from 'react-i18next';

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

/**
 * Show local processing evidence without asserting destination playback health.
 * @param {{log?: string}} props Latest progress log, retained in an expandable view.
 * @returns {React.ReactElement} Local progress summary and raw diagnostics.
 */
export default function ForwardProgress({log}) {
  const {t} = useTranslation();
  const progress = parseForwardProgress(log);
  return <div>
    <div>{t('forward.progress.fps')}: {progress.fps ?? '—'}</div>
    <div>{t('forward.progress.speed')}: {progress.speed ?? '—'}</div>
    <div>{t('forward.progress.time')}: {progress.time ?? '—'}</div>
    {log && <details className="mt-1">
      <summary>{t('forward.progress.raw')}</summary>
      <pre className="small mb-0" style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{log}</pre>
    </details>}
  </div>;
}
