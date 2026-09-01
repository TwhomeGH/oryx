//
// Copyright (c) 2022-2024 Winlin
//
// SPDX-License-Identifier: MIT
//
import '@testing-library/jest-dom';
import {render, screen} from '@testing-library/react';
import {describe, expect, it} from 'vitest';
import {TutorialsToast, fmtCount, tutorialDisplayTitle} from './TutorialsButton';

describe('TutorialsToast', () => {
  it('shows a visible fallback title for Bilibili entries without metadata', () => {
    const tutorial = {
      id: 'BV1844y1L7dL',
      media: 'Bilibili',
      author: 'SRS',
      link: 'https://www.bilibili.com/video/BV1844y1L7dL',
    };

    render(<TutorialsToast tutorials={[tutorial]} />);

    const link = screen.getByRole('link', {name: '前往观看'});
    expect(link).toHaveAttribute('href', tutorial.link);
    expect(link).toHaveAttribute('title', tutorial.id);
  });

  it('keeps existing tutorial titles unchanged', () => {
    expect(tutorialDisplayTitle({
      id: 'BV1844y1L7dL',
      media: 'Bilibili',
      title: 'Oryx tutorial',
      link: 'https://www.bilibili.com/video/BV1844y1L7dL',
    })).toBe('Oryx tutorial');
    expect(tutorialDisplayTitle({
      id: 'BV1844y1L7dL',
      media: 'Bilibili',
      link: 'https://www.bilibili.com/video/BV1844y1L7dL',
    }, '前往观看')).toBe('前往观看');
  });

  it('ignores invalid tutorial stats safely', () => {
    expect(fmtCount(undefined)).toBeNull();
    expect(fmtCount('')).toBeNull();
    expect(fmtCount('1200')).toBe('1.2K');
  });
});
