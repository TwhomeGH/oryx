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
export default function ForwardProgress({log, metrics, events, logs, encoder, ffmpegArgs, programSourceRate, programSourceCFR, programSourceJitterMS, programSourceMaxGapMS, programPlan, programPlanReason, programLastError, programLogs, programArgs, programMetrics}) {
  const {t} = useTranslation();

  // The program timeline plan is a code (copy/repair/cfr/composite) with a reason code; translate
  // both so "why is it copying / why is it CFR" is answerable without reading the ffmpeg command.
  const planText = (p) => ({
    copy: t('forward.progress.planCopy'),
    repair: t('forward.progress.planRepair'),
    cfr: t('forward.progress.planCfr'),
    composite: t('forward.progress.planComposite'),
  }[p] || p || '—');
  // The verdict is measured, so show the measurement: jitter for a CFR source, the largest hole
  // for a VFR one. A label without its evidence is not verifiable.
  const sourceEvidence = (cfr, jitter, maxGap) => {
    if (cfr && typeof jitter === 'number') return `（${t('forward.progress.sourceJitter', {value: jitter.toFixed(1)})}）`;
    if (!cfr && typeof maxGap === 'number') return `（${t('forward.progress.sourceMaxGap', {value: maxGap.toFixed(1)})}）`;
    return '';
  };
  const planNoteText = (n) => ({
    'normalize-off': t('forward.progress.noteNormalizeOff'),
    passthrough: t('forward.progress.notePassthrough'),
    'cfr-but-source-cfr': t('forward.progress.noteCfrButSourceCfr'),
    'cfr-forced': t('forward.progress.noteCfrForced'),
    'auto-cfr': t('forward.progress.noteAutoCfr'),
    'auto-repair': t('forward.progress.noteAutoRepair'),
    'auto-vfr': t('forward.progress.noteAutoVfr'),
    composite: t('forward.progress.noteComposite'),
  }[n] || n || '');
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

  if (!log && !metrics && !events?.length && !logs?.length && !ffmpegArgs && !programLastError && !(programLogs?.length) && !programArgs && !programPlan && !(programMetrics?.updatedAt)) {
    return <div className="small text-muted">{t('forward.progress.empty')}</div>;
  }

  return <div className="small">
    {log && <>
      <div>{t('forward.progress.fps')}: {progress.fps ?? '—'}</div>
      <div>{t('forward.progress.speed')}: {progress.speed ?? '—'}</div>
      <div>{t('forward.progress.time')}: {progress.time ?? '—'}</div>
    </>}
    {metrics && <>
          <div>{t('forward.progress.avgFps')}: {fmt(metrics.avgFps)}</div>
          {programSourceRate && <div>{t('forward.progress.sourceRate')}: {programSourceRate} → {fmt(metrics.avgFps)}</div>}
      <div>{t('forward.progress.noOutput')}: {fmt(metrics.noOutputSeconds)}s</div>
      <div>
        {t('forward.progress.driftRate')}: {
          metrics.timelineRate
            ? <>{metrics.timelineRate.toFixed(4)} <span className="text-muted">
                ({metrics.driftPerHour !== undefined
                  ? (Math.abs(metrics.driftPerHour) <= 10
                    ? t('forward.progress.driftRateOk')
                    : t('forward.progress.driftRateBad', {perHour: fmt(metrics.driftPerHour)}))
                  : t('forward.progress.driftRateSampling')
                })
              </span></>
            : <span className="text-muted">{t('forward.progress.driftRatePending')}</span>
        }
      </div>
      <div>{t('forward.progress.driftLead')}: {fmt(metrics.mediaClockDriftSec)}s
        <span className="text-muted"> · {t('forward.progress.driftLeadHint')}</span>
      </div>
      {(metrics.fanoutLagSec !== undefined || metrics.srsQueueLimitSec) && <div className="mt-1">
        {t('forward.progress.delivery')}:
        {metrics.fanoutLagSec !== undefined && <> {t('forward.progress.hopProgramFanout')} {fmt(metrics.fanoutLagSec)}s</>}
        {metrics.srsQueueLimitSec ? <> · {t('forward.progress.hopQueue', {sec: metrics.srsQueueLimitSec})}</> : null}
        {metrics.srsPlayLatencyMs ? <> · {t('forward.progress.hopPlayLatency', {ms: metrics.srsPlayLatencyMs})}</> : null}
        {metrics.destinationUnknown && <span className="text-muted"> · {t('forward.progress.hopDestinationUnknown')}</span>}
      </div>}
      {programMetrics && programMetrics.updatedAt && <div>
        {t('forward.progress.programFps')}: {fmt(programMetrics.fps)}
        {programMetrics.encodedFps ? ` · ${t('forward.progress.programEncoded')}: ${programMetrics.encodedFps.toFixed(1)}fps` : ''}
        {programMetrics.speed ? ` · speed ${programMetrics.speed.toFixed(2)}x` : ''}
        {programMetrics.bitrateKbps ? ` · ${Math.round(programMetrics.bitrateKbps)} kbps` : ''}
        {programMetrics.startupMs ? ` · ${t('forward.progress.programStartup')}: ${(programMetrics.startupMs / 1000).toFixed(1)}s` : ''}
      </div>}
      {metrics.relayReconnects > 0 && <div>{t('forward.progress.reconnects')}: {metrics.relayReconnects}</div>}
      {encoder && <div>{t('forward.progress.encoder')}: {encoder}{encHardware ? ` ${t('forward.progress.gpu')}` : ''}</div>}
      {(programPlan || programSourceRate) && <div>
        {t('forward.progress.plan')}: <b>{planText(programPlan)}</b>
        {programPlanReason ? `（${planNoteText(programPlanReason)}）` : ''}
        {programSourceRate ? ` · ${t('forward.progress.sourceKind')}: ${programSourceCFR ? t('forward.progress.sourceCFR') : t('forward.progress.sourceVFR')} ${programSourceRate}fps${sourceEvidence(programSourceCFR, programSourceJitterMS, programSourceMaxGapMS)}` : ''}
      </div>}
    </>}
    <Sparkline data={history.current} />
    {log && <details className="mt-1">
      <summary>{t('forward.progress.raw')}</summary>
      <pre className="small mb-0" style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{log}</pre>
    </details>}
    <details className="mt-1">
      <summary>{t('forward.progress.ffmpegArgsProgram')}</summary>
      {programArgs
        ? <pre className='small mb-0' style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{programArgs}</pre>
        : <div className='small text-muted'>{t('forward.progress.ffmpegArgsIdle')}</div>}
    </details>
    <details className="mt-1">
      <summary>{t('forward.progress.ffmpegArgsFanout')}</summary>
      {ffmpegArgs
        ? <pre className='small mb-0' style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{ffmpegArgs}</pre>
        : <div className='small text-muted'>{t('forward.progress.ffmpegArgsIdle')}</div>}
    </details>
    {events?.length > 0 && <details className="mt-1">
      <summary>{t('forward.progress.events')}</summary>
      <ul className="small mb-0 ps-3">
        {events.map((e, i) => <li key={i}>{e.at} {e.name}{e.detail ? `: ${e.detail}` : ''}</li>)}
      </ul>
    </details>}
    {(programLastError || programLogs?.length > 0) && (
      <details className="mt-1">
        <summary className="text-danger">{t('forward.programError')}</summary>
        {programLastError && <div className="small text-danger">{programLastError}</div>}
        <pre className="small mb-0" style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>
          {(programLogs || []).slice(-20).join('\n')}
        </pre>
      </details>
    )}
    {logs?.length > 0 && <details className="mt-1">
      <summary>{t('forward.progress.logs')}</summary>
      <pre className="small mb-0" style={{whiteSpace: 'pre-wrap', overflowWrap: 'anywhere'}}>{logs.join('\n')}</pre>
    </details>}
  </div>;
}
