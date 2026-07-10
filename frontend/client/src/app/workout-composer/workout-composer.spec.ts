import { pacingCurveToPoints } from './workout-composer';

describe('pacingCurveToPoints', () => {
  it('maps a curve into an SVG points string within the viewBox bounds', () => {
    const points = pacingCurveToPoints([100, 150, 120, 180], 300, 100);
    const pairs = points.split(' ').map((p) => p.split(',').map(Number));

    expect(pairs.length).toBe(4);
    expect(pairs[0][0]).toBe(0);
    expect(pairs[3][0]).toBe(300);
    for (const [, y] of pairs) {
      expect(y).toBeGreaterThanOrEqual(0);
      expect(y).toBeLessThanOrEqual(100);
    }
  });

  it('returns an empty string for an empty curve', () => {
    expect(pacingCurveToPoints([])).toBe('');
  });
});
