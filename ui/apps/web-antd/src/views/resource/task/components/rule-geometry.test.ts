import type { VideoViewport } from './rule-geometry';

import { describe, expect, it } from 'vitest';

import {
  denormalizePoint,
  getRenderedVideoRect,
  hasSelfIntersection,
  normalizePoint,
} from './rule-geometry';

const WIDE_VIDEO_IN_TALL_ELEMENT: VideoViewport = {
  elementHeight: 600,
  elementWidth: 800,
  videoHeight: 1080,
  videoWidth: 1920,
};

describe('getRenderedVideoRect', () => {
  it('centers a wide video with top and bottom black bars', () => {
    expect(getRenderedVideoRect(WIDE_VIDEO_IN_TALL_ELEMENT)).toEqual({
      height: 450,
      offsetX: 0,
      offsetY: 75,
      width: 800,
    });
  });

  it('centers a narrow video with left and right black bars', () => {
    const rendered = getRenderedVideoRect({
      elementHeight: 800,
      elementWidth: 1200,
      videoHeight: 1080,
      videoWidth: 1280,
    });
    expect(rendered.height).toBe(800);
    expect(rendered.offsetX).toBeCloseTo(125.92592592592592, 12);
    expect(rendered.offsetY).toBe(0);
    expect(rendered.width).toBeCloseTo(948.1481481481482, 12);
  });

  it('uses the complete element when aspect ratios are equal', () => {
    expect(
      getRenderedVideoRect({
        elementHeight: 450,
        elementWidth: 800,
        videoHeight: 1080,
        videoWidth: 1920,
      }),
    ).toEqual({
      height: 450,
      offsetX: 0,
      offsetY: 0,
      width: 800,
    });
  });
});

describe('normalizePoint and denormalizePoint', () => {
  it('maps the center of a 16:9 video in a 4:3 element to the normalized center', () => {
    expect(
      normalizePoint({ x: 400, y: 300 }, WIDE_VIDEO_IN_TALL_ELEMENT),
    ).toEqual({ x: 0.5, y: 0.5 });
  });

  it('ignores points in black bars when clampToViewport is false', () => {
    expect(
      normalizePoint({ x: 400, y: 20 }, WIDE_VIDEO_IN_TALL_ELEMENT, false),
    ).toBeNull();
  });

  it('clamps points in black bars when clampToViewport is true', () => {
    expect(
      normalizePoint({ x: 400, y: 20 }, WIDE_VIDEO_IN_TALL_ELEMENT, true),
    ).toEqual({ x: 0.5, y: 0 });
    expect(
      normalizePoint({ x: 400, y: 550 }, WIDE_VIDEO_IN_TALL_ELEMENT, true),
    ).toEqual({ x: 0.5, y: 1 });
  });

  it('round-trips normalized points through the rendered video rect', () => {
    const viewports: VideoViewport[] = [
      WIDE_VIDEO_IN_TALL_ELEMENT,
      {
        elementHeight: 800,
        elementWidth: 1200,
        videoHeight: 1080,
        videoWidth: 1280,
      },
      {
        elementHeight: 450,
        elementWidth: 800,
        videoHeight: 1080,
        videoWidth: 1920,
      },
    ];

    for (const viewport of viewports) {
      const original = { x: 0.23, y: 0.81 };
      const canvasPoint = denormalizePoint(original, viewport);
      const roundTrip = normalizePoint(canvasPoint, viewport);
      expect(roundTrip?.x).toBeCloseTo(original.x, 10);
      expect(roundTrip?.y).toBeCloseTo(original.y, 10);
    }
  });
});

describe('hasSelfIntersection', () => {
  it('accepts a simple polygon', () => {
    expect(
      hasSelfIntersection([
        { x: 0.1, y: 0.1 },
        { x: 0.9, y: 0.1 },
        { x: 0.9, y: 0.9 },
        { x: 0.1, y: 0.9 },
      ]),
    ).toBe(false);
  });

  it('detects a bow-tie polygon', () => {
    expect(
      hasSelfIntersection([
        { x: 0.1, y: 0.1 },
        { x: 0.9, y: 0.9 },
        { x: 0.1, y: 0.9 },
        { x: 0.9, y: 0.1 },
      ]),
    ).toBe(true);
  });
});
