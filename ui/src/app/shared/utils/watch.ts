import * as React from 'react';
import {fromEvent, interval, Observable, Observer, Subscription} from 'rxjs';
import {bufferTime, debounceTime, delay, map, mergeMap, repeat, retryWhen, scan, takeUntil} from 'rxjs/operators';

function fromEventSource(url: string): Observable<MessageEvent> {
    return new Observable<MessageEvent>((subscriber) => {
        let sse = new EventSource(url);
        sse.onmessage = (e) => subscriber.next(e);
        sse.onerror = (e) => subscriber.error(e);
        return () => {
            if (sse.readyState === 1) {
                sse.close();
                sse = null;
            }
        };
    });
}

const INITIAL_LOAD_TIME = 500;
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

interface WatchEvent {
    type?: string;
}

// NOTE: findItem and getItem must be React.useCallback functions
export function useWatch<T, E extends WatchEvent>(url: string, findItem: (item: T, change: E) => boolean, getItem: (change: E) => T, init?: T[]): [T[], boolean, boolean] {
    const [items, setItems] = React.useState([] as T[]);
    const [error, setError] = React.useState(false);
    const [loading, setLoading] = React.useState(true);
    React.useEffect(() => {
        const stream = fromEventSource(url).pipe(map((res) => JSON.parse(res.data).result as E));
        let watch = stream.pipe(
            repeat(),
            retryWhen((errors) => errors.pipe(delay(500))),
            scan((items, change) => {
                const index = items.findIndex((i) => findItem(i, change));
                switch (change.type) {
                    case 'DELETED':
                        if (index > -1) {
                            items.splice(index, 1);
                        }
                        break;
                    default:
                        if (index > -1) {
                            items[index] = getItem(change) as T;
                        } else {
                            items.unshift(getItem(change) as T);
                        }
                        break;
                }
                return items;
            }, init || [])
        );

        const subscribeList = (list: T[]) => {
            setItems([...list]);
        };

        const initialLoad = watch.pipe(
            takeUntil(interval(INITIAL_LOAD_TIME)),
            bufferTime(INITIAL_LOAD_TIME),
            mergeMap((r) => r)
        );

        initialLoad.subscribe(
            subscribeList,
            () => {
                setLoading(false);
                setError(true);
            },
            () => {
                setLoading(false);
            }
        );

        const liveStream = handlePageVisibility(() =>
            watch.pipe(
                bufferTime(BUFFER_TIME),
                mergeMap((r) => r)
            )
        );
        liveStream.subscribe(subscribeList);

        return () => {
            watch = null;
        };
    }, [init, url, findItem, getItem]);
    return [items, loading, error];
}
