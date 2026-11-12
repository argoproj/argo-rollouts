import {computePauseProgress} from './pause-timer';

describe('computePauseProgress', () => {
    it('positions the bar from the elapsed fraction', () => {
        const r = computePauseProgress({durationSeconds: 900, remainingSeconds: 600});
        expect(r.fillPercent).toBeCloseTo(33.33, 1);
        expect(r.remainingMs).toBe(600_000);
        expect(r.done).toBe(false);
    });

    it('starts empty when the pause has just begun', () => {
        const r = computePauseProgress({durationSeconds: 900, remainingSeconds: 900});
        expect(r.fillPercent).toBe(0);
        expect(r.remainingMs).toBe(900_000);
        expect(r.done).toBe(false);
    });

    it('is complete once nothing remains', () => {
        const r = computePauseProgress({durationSeconds: 900, remainingSeconds: 0});
        expect(r.fillPercent).toBe(100);
        expect(r.remainingMs).toBe(0);
        expect(r.done).toBe(true);
    });

    // The controller normalizes every duration form time.ParseDuration accepts,
    // including compound and fractional ones the UI used to parse itself and get
    // wrong. Both values arrive as whole seconds.
    it.each([
        ['1h30m', 5400, 3600, 33.33],
        ['2h45m30s', 9930, 4965, 50],
        ['1.5h', 5400, 1350, 75],
        ['15m', 900, 450, 50],
    ])('tracks a %s pause normalized to %i seconds', (_form, durationSeconds, remainingSeconds, expectedPercent) => {
        const r = computePauseProgress({durationSeconds, remainingSeconds});
        expect(r.fillPercent).toBeCloseTo(expectedPercent, 1);
        expect(r.remainingMs).toBe(remainingSeconds * 1000);
        expect(r.done).toBe(false);
    });

    // 0 means "not paused on a timed step" and -1 means "the controller could not
    // parse the configured duration". Neither should render a bar, so they are
    // reported as absent rather than as an instantly-complete pause.
    it('reports no progress for an indefinite pause', () => {
        expect(computePauseProgress({durationSeconds: 0, remainingSeconds: 0})).toBeNull();
    });

    it('reports no progress when the controller could not parse the duration', () => {
        expect(computePauseProgress({durationSeconds: -1, remainingSeconds: 0})).toBeNull();
    });

    // The controller clamps, but the UI must not be the thing that breaks if a
    // future producer sends something outside the documented invariant.
    it('clamps a remaining value larger than the duration', () => {
        const r = computePauseProgress({durationSeconds: 900, remainingSeconds: 5000});
        expect(r.fillPercent).toBe(0);
        expect(r.remainingMs).toBe(900_000);
    });

    it('clamps a negative remaining value', () => {
        const r = computePauseProgress({durationSeconds: 900, remainingSeconds: -30});
        expect(r.fillPercent).toBe(100);
        expect(r.remainingMs).toBe(0);
        expect(r.done).toBe(true);
    });
});
