import { describe, expect, it } from 'vitest';

import { buildRtspUrl, parseRtspUrl } from './rtsp';

describe('parseRtspUrl', () => {
  it('parses a plain URL without credentials', () => {
    const parts = parseRtspUrl('rtsp://192.168.1.10/live');
    expect(parts).toEqual({
      address: 'rtsp://192.168.1.10/live',
      username: '',
      password: '',
    });
  });

  it('splits userinfo by the last @', () => {
    const parts = parseRtspUrl('rtsp://user:p%40ss@192.168.1.10/live');
    expect(parts.address).toBe('rtsp://192.168.1.10/live');
    expect(parts.username).toBe('user');
    expect(parts.password).toBe('p@ss');
  });

  it('keeps password containing unencoded @ / # ? before the last @', () => {
    const parts = parseRtspUrl('rtsp://admin:pa@ss#w?rd@192.168.1.10/live');
    expect(parts.address).toBe('rtsp://192.168.1.10/live');
    expect(parts.username).toBe('admin');
    expect(parts.password).toBe('pa@ss#w?rd');
  });

  it('treats userinfo without colon as username only', () => {
    const parts = parseRtspUrl('rtsp://admin@192.168.1.10/live');
    expect(parts.address).toBe('rtsp://192.168.1.10/live');
    expect(parts.username).toBe('admin');
    expect(parts.password).toBe('');
  });

  it('decodes percent-encoded credentials', () => {
    const parts = parseRtspUrl(
      'rtsp://user%40name:p%3A%2Fss@192.168.1.10/live',
    );
    expect(parts.username).toBe('user@name');
    expect(parts.password).toBe('p:/ss');
  });

  it('trims surrounding whitespace', () => {
    const parts = parseRtspUrl('  rtsp://u:p@192.168.1.10/live  ');
    expect(parts.address).toBe('rtsp://192.168.1.10/live');
    expect(parts.username).toBe('u');
    expect(parts.password).toBe('p');
  });
});

describe('buildRtspUrl', () => {
  it('builds a full URL with encoded credentials', () => {
    const url = buildRtspUrl('rtsp://192.168.1.10/live', 'user', 'p@ss/word#?');
    expect(url).toBe('rtsp://user:p%40ss%2Fword%23%3F@192.168.1.10/live');
  });

  it('returns plain address when no credentials', () => {
    const url = buildRtspUrl('rtsp://192.168.1.10/live', '', '');
    expect(url).toBe('rtsp://192.168.1.10/live');
  });

  it('strips existing userinfo from address before rebuild', () => {
    const url = buildRtspUrl(
      'rtsp://old:secret@192.168.1.10/live',
      'new',
      'pass',
    );
    expect(url).toBe('rtsp://new:pass@192.168.1.10/live');
  });

  it('round-trips parse/build', () => {
    const original = 'rtsp://admin:p%40ss@192.168.1.10/live';
    const parts = parseRtspUrl(original);
    const rebuilt = buildRtspUrl(parts.address, parts.username, parts.password);
    expect(rebuilt).toBe(original);
  });
});
