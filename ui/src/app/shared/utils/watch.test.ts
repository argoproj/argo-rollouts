import {Subject} from 'rxjs';
import {coalesceBuffered} from './watch';

describe('coalesceBuffered', () => {
    beforeEach(() => {
        jest.useFakeTimers();
    });

    afterEach(() => {
        jest.useRealTimers();
    });

    it('emits only the most recent value once per buffer window, instead of once per source emission', () => {
        const source = new Subject<number>();
        const emissions: number[] = [];
        source.pipe(coalesceBuffered(500)).subscribe((v) => emissions.push(v));

        // Simulates many watch events (e.g. an initial list of rollouts) arriving in the
        // same window: this should collapse into a single re-render instead of one-per-event.
        for (let i = 1; i <= 123; i++) {
            source.next(i);
        }

        jest.advanceTimersByTime(500);

        expect(emissions).toEqual([123]);
    });

    it('does not emit anything for a window with no values', () => {
        const source = new Subject<number>();
        const emissions: number[] = [];
        source.pipe(coalesceBuffered(500)).subscribe((v) => emissions.push(v));

        jest.advanceTimersByTime(500);

        expect(emissions).toEqual([]);
    });
});
