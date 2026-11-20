import {Subject} from 'rxjs';
import {applyWatchEvent, latestPerInterval, watchListItems} from './watch';

interface Item {
    name: string;
    value?: number;
    extra?: string;
}

interface Event {
    type?: string;
    item: Item;
}

const findItem = (item: Item, change: Event) => item.name === change.item.name;
const getItem = (change: Event) => change.item;

describe('applyWatchEvent', () => {
    const a: Item = {name: 'a', value: 1, extra: 'kept'};
    const b: Item = {name: 'b', value: 1};
    const items = [a, b];

    it('merges an update into the matching item without touching the others', () => {
        const next = applyWatchEvent(items, {type: 'Updated', item: {name: 'a', value: 2}}, findItem, getItem);

        expect(next).toEqual([{name: 'a', value: 2, extra: 'kept'}, b]);
        expect(next[1]).toBe(b);
        expect(next).not.toBe(items);
        expect(items).toEqual([a, b]);
    });

    it('returns the same list when an update matches no item', () => {
        expect(applyWatchEvent(items, {type: 'Updated', item: {name: 'c'}}, findItem, getItem)).toBe(items);
    });

    it('removes a deleted item', () => {
        expect(applyWatchEvent(items, {type: 'Deleted', item: {name: 'a'}}, findItem, getItem)).toEqual([b]);
        expect(items).toEqual([a, b]);
    });

    it('returns the same list when a deleted item is unknown', () => {
        expect(applyWatchEvent(items, {type: 'Deleted', item: {name: 'c'}}, findItem, getItem)).toBe(items);
    });

    it('replaces a known item and prepends an unknown item for other event types', () => {
        expect(applyWatchEvent(items, {type: 'Added', item: {name: 'a', value: 3}}, findItem, getItem)).toEqual([{name: 'a', value: 3}, b]);
        expect(applyWatchEvent(items, {type: 'Added', item: {name: 'c'}}, findItem, getItem)).toEqual([{name: 'c'}, a, b]);
        expect(items).toEqual([a, b]);
    });
});

describe('latestPerInterval', () => {
    beforeEach(() => jest.useFakeTimers());
    afterEach(() => jest.useRealTimers());

    it('emits only the latest value of each interval and nothing for an idle interval', () => {
        const source = new Subject<number>();
        const received: number[] = [];
        const sub = source.pipe(latestPerInterval(500)).subscribe((v) => received.push(v));

        source.next(1);
        source.next(2);
        source.next(3);
        expect(received).toEqual([]);

        jest.advanceTimersByTime(500);
        expect(received).toEqual([3]);

        jest.advanceTimersByTime(1500);
        expect(received).toEqual([3]);

        source.next(4);
        jest.advanceTimersByTime(500);
        expect(received).toEqual([3, 4]);

        sub.unsubscribe();
    });
});

describe('watchListItems', () => {
    beforeEach(() => jest.useFakeTimers());
    afterEach(() => jest.useRealTimers());

    it('emits the current list when it was replaced while events were buffered', () => {
        const changes = new Subject<Event>();
        const items = {current: [] as Item[]};
        const received: Item[][] = [];
        const sub = watchListItems(changes, items, findItem, getItem).subscribe((l) => received.push(l));

        changes.next({type: 'Updated', item: {name: 'a', value: 2}});
        const loaded = [{name: 'a', value: 1}];
        items.current = loaded;
        jest.advanceTimersByTime(500);

        expect(received).toEqual([loaded]);

        changes.next({type: 'Updated', item: {name: 'a', value: 3}});
        jest.advanceTimersByTime(500);

        expect(received[1]).toEqual([{name: 'a', value: 3}]);

        sub.unsubscribe();
    });
});
