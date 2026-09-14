import {computePauseProgress, durationToSeconds} from './pause-timer';

describe('durationToSeconds', () => {
    it('parses unit suffixes and bare seconds', () => {
        expect(durationToSeconds('15m')).toBe(900);
        expect(durationToSeconds('30s')).toBe(30);
        expect(durationToSeconds('1h')).toBe(3600);
        expect(durationToSeconds('300')).toBe(300);
    });
    it('returns 0 for unparseable input', () => {
        expect(durationToSeconds('')).toBe(0);
        expect(durationToSeconds('abc')).toBe(0);
    });
});

describe('computePauseProgress', () => {
    const start = 1_000_000;
    it('reports fractional progress and remaining time mid-pause', () => {
        const r = computePauseProgress({startTimeMs: start, durationSeconds: 900, nowMs: start + 300_000});
        expect(r.fillPercent).toBeCloseTo(33.33, 1);
        expect(r.remainingMs).toBe(600_000);
        expect(r.done).toBe(false);
    });
    it('clamps to 100% / 0 remaining once elapsed reaches total', () => {
        const r = computePauseProgress({startTimeMs: start, durationSeconds: 900, nowMs: start + 1_000_000});
        expect(r.fillPercent).toBe(100);
        expect(r.remainingMs).toBe(0);
        expect(r.done).toBe(true);
    });
    it('never goes negative when the clock is behind the start', () => {
        const r = computePauseProgress({startTimeMs: start, durationSeconds: 900, nowMs: start - 5_000});
        expect(r.fillPercent).toBe(0);
        expect(r.remainingMs).toBe(900_000);
        expect(r.done).toBe(false);
    });
    it('treats a zero/invalid duration as done', () => {
        const r = computePauseProgress({startTimeMs: start, durationSeconds: 0, nowMs: start + 1_000});
        expect(r.fillPercent).toBe(100);
        expect(r.remainingMs).toBe(0);
        expect(r.done).toBe(true);
    });
});
