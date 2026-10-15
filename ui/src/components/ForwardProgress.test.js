import React from 'react';
import {render, screen, cleanup} from '@testing-library/react';
import {afterEach, describe, expect, it, vi} from 'vitest';
import ForwardProgress, {parseForwardProgress} from './ForwardProgress';

vi.mock('react-i18next', () => ({useTranslation: () => ({t: key => key})}));
afterEach(cleanup);

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

  it('renders raw diagnostics as text in a collapsed disclosure', () => {
    const log = 'fps=90 time=00:00:03.00 speed=1.5x <img src=x onerror=alert(1)>';
    const {container} = render(<ForwardProgress log={log} />);
    expect(screen.getByText('forward.progress.fps: 90')).toBeTruthy();
    expect(container.querySelector('details').open).toBe(false);
    expect(container.querySelector('pre').textContent).toBe(log);
    expect(container.querySelector('img')).toBeNull();
  });
});
