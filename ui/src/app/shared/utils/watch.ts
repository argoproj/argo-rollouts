import * as React from 'react';
import {fromEvent, Observable, Observer, OperatorFunction, Subscription} from 'rxjs';
import {bufferTime, debounceTime, delay, filter, map, repeat, retryWhen, scan, timeout} from 'rxjs/operators';

enum ReadyState {
    CONNECTING = 0,
    OPEN = 1,
    CLOSED = 2,
    DONE = 4,
}

export interface ListState<T> {
    loading: boolean;
    items: T[];
    error: Error | null;
}

function fromEventSource(url: string): Observable<string> {
    return Observable.create((observer: Observer<any>) => {
        let eventSource = new EventSource(url);
        eventSource.onmessage = (msg) => observer.next(msg.data);
        eventSource.onerror = (e) => () => {
            observer.error(e);
        };

        const interval = setInterval(() => {
            if (eventSource && eventSource.readyState === ReadyState.CLOSED) {
                observer.error('connection got closed unexpectedly');
            }
        }, 500);
        return () => {
            clearInterval(interval);
            eventSource.close();
            eventSource = null;
        };
    });
}

const BUFFER_TIME = 500;

export function handlePageVisibility<T>(src: () => Observable<T>): Observable<T> {
    return new Observable<T>((observer: Observer<T>) => {
        let subscription: Subscription;
        const ensureUnsubscribed = () => {
            if (subscription) {
                subscription.unsubscribe();
                subscription = null;
            }
        };
        const start = () => {
            ensureUnsubscribed();
            subscription = src().subscribe(
                (item: T) => observer.next(item),
                (err) => observer.error(err),
                () => observer.complete()
            );
        };

        if (!document.hidden) {
            start();
        }

        const visibilityChangeSubscription = fromEvent(document, 'visibilitychange')
            .pipe(debounceTime(500))
            .subscribe(() => {
                if (document.hidden && subscription) {
                    ensureUnsubscribed();
                } else if (!document.hidden && !subscription) {
                    start();
                }
            });

        return () => {
            visibilityChangeSubscription.unsubscribe();
            ensureUnsubscribed();
        };
    });
}

// latestPerInterval emits the latest value received in each interval, so that a burst of
// values results in a single emission. Nothing is emitted for an interval without values.
export function latestPerInterval<T>(interval: number): OperatorFunction<T, T> {
    return (src) =>
        src.pipe(
            bufferTime(interval),
            filter((batch) => batch.length > 0),
            map((batch) => batch[batch.length - 1])
        );
}

interface WatchEvent {
    type?: string;
}

// applyWatchEvent returns the list with a watch event applied. The given list is never
// modified, and it is returned as is when the event changes nothing.
export function applyWatchEvent<T, E extends WatchEvent>(items: T[], change: E, findItem: (item: T, change: E) => boolean, getItem: (change: E) => T): T[] {
    const index = items.findIndex((i) => findItem(i, change));
    switch (change.type) {
        case 'Deleted':
            if (index === -1) {
                return items;
            }
            return items.filter((_, i) => i !== index);
        case 'Updated': {
            if (index === -1) {
                return items;
            }
            const updated = [...items];
            updated[index] = {...items[index], ...getItem(change)} as T;
            return updated;
        }
        default: {
            if (index === -1) {
                return [getItem(change), ...items];
            }
            const replaced = [...items];
            replaced[index] = getItem(change);
            return replaced;
        }
    }
}

// NOTE: findItem and getItem must be React.useCallback functions
export function useWatchList<T, E extends WatchEvent>(url: string, findItem: (item: T, change: E) => boolean, getItem: (change: E) => T, init?: T[]): T[] {
    const [items, setItems] = React.useState(init as T[]);
    const latestItems = React.useRef(init || []);

    React.useEffect(() => {
        latestItems.current = init || [];
        setItems(init);
    }, [init]);

    React.useEffect(() => {
        const stream = fromEventSource(url).pipe(map((res) => JSON.parse(res).result as E));
        let watch = stream.pipe(
            repeat(),
            retryWhen((errors) => errors.pipe(delay(500))),
            map((change) => {
                latestItems.current = applyWatchEvent(latestItems.current, change, findItem, getItem);
                return latestItems.current;
            }),
            latestPerInterval(BUFFER_TIME)
        );

        const sub = handlePageVisibility(() => watch).subscribe((l) => {
            setItems(l);
        });

        return () => {
            sub.unsubscribe();
            watch = null;
        };
    }, [url, findItem, getItem]);
    return items;
}

export function useWatch<T>(url: string, subscribe: boolean, isEqual: (a: T, b: T) => boolean, timeoutAfter?: number) {
    const [item, setItem] = React.useState({} as T);
    React.useEffect(() => {
        if (!subscribe) {
            return;
        }
        const stream = fromEventSource(url).pipe(map((res) => JSON.parse(res).result as T));
        let watch = stream.pipe(
            repeat(),
            retryWhen((errors) => errors.pipe(delay(500))),
            scan(
                (acc, update) => {
                    return {data: update, updated: !isEqual(update, acc.data)};
                },
                {data: {} as T, updated: true}
            ),
            filter((i) => i.updated),
            map((i) => i.data)
        );

        let liveStream = handlePageVisibility(() => watch.pipe(latestPerInterval(BUFFER_TIME)));

        if (timeoutAfter > 0) {
            liveStream = liveStream.pipe(timeout(timeoutAfter));
        }

        const sub = liveStream.subscribe((i) => setItem(i));
        return () => {
            liveStream = null;
            sub.unsubscribe();
        };
    }, [url, subscribe, timeoutAfter, isEqual]);
    return item;
}
