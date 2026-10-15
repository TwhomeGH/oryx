import React from 'react';
import {render, screen, cleanup} from '@testing-library/react';
import {afterEach, describe, expect, it, vi} from 'vitest';
import ForwardProgress, {parseForwardProgress} from './ForwardProgress';

vi.mock('react-i18next', () => ({useTranslation: () => ({t: key => key})}));
afterEach(cleanup);

const log = 'frame=53731 fps=60 q=-1.0 size=444182KiB time=00:18:33.45 bitrate=3268.0kbits/s speed=1.24x';
const metrics = {
  frame: 53731, outputFps: 60, avgFps: 25.03, speed: 1.24, mediaTime: '00:18:33.45',
  mediaSeconds: 1113.45, noOutputSeconds: 0.2, mediaClockDriftSec: 0.4, relayReconnects: 2,
};

describe('ForwardProgress', () => {
  it('reads padded FFmpeg fields without relabelling them as source FPS', () => {
    expect(parseForwardProgress('frame= 180 fps= 90 q=-1.0 time=00:00:03.00 speed= 1.5x'))
      .toEqual({fps: '90', speed: '1.5x', time: '00:00:03.00'});
  });

  it('keeps unavailable values distinct from real zero', () => {
    expect(parseForwardProgress('fps=N/A time=N/A speed=N/A')).toEqual({fps: null, speed: null, time: null});
    expect(parseForwardProgress('fps=0 time=00:00:00.00 speed=0x').fps).toBe('0');
    expect(parseForwardProgress()).toEqual({fps: null, speed: null, time: null});
  });

  it('shows the metrics and the diagnostics timeline, collapsed', () => {
    const {container} = render(<ForwardProgress log={log} metrics={metrics}
      events={[{at: '2026-10-15T00:00:00Z', name: 'start', detail: 'out'}]}
      logs={['[http] Will reconnect']} />);
    expect(screen.getByText('forward.progress.avgFps: 25.0')).toBeTruthy();
    expect(screen.getByText('forward.progress.reconnects: 2')).toBeTruthy();
    expect(screen.getByText(/^2026-10-15T00:00:00Z start/)).toBeTruthy();
    expect(container.querySelector('svg')).toBeNull();
    for (const d of container.querySelectorAll('details')) {
      expect(d.open).toBe(false);
    }
  });

  it('draws a trend once there are enough samples', () => {
    const {container, rerender} = render(<ForwardProgress log={log} metrics={{avgFps: 24}} />);
    rerender(<ForwardProgress log={log} metrics={{avgFps: 25}} />);
    expect(container.querySelector('svg polyline')).toBeTruthy();
  });

  it('renders raw diagnostics as text in a collapsed disclosure', () => {
    const withXss = 'fps=90 time=00:00:03.00 speed=1.5x <img src=x onerror=alert(1)>';
    const {container} = render(<ForwardProgress log={withXss} />);
    expect(screen.getByText('forward.progress.fps: 90')).toBeTruthy();
    expect(container.querySelector('details').open).toBe(false);
    expect(container.querySelector('pre').textContent).toBe(withXss);
    expect(container.querySelector('img')).toBeNull();
  });

  it('does not inject event or log content', () => {
    const {container} = render(<ForwardProgress log={log}
      events={[{at: 't', name: 'x', detail: '<img src=x onerror=alert(1)>'}]}
      logs={['<img src=x onerror=alert(1)>']} />);
    expect(container.querySelector('img')).toBeNull();
  });
});
