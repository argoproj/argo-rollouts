import * as React from 'react';
import {fromEvent, Observable, Observer, Subscription} from 'rxjs';
import {bufferTime, debounceTime, delay, filter, map, mergeMap, repeat, retryWhen, scan, timeout} from 'rxjs/operators';

enum ReadyState {
    CONNECTING = 0,
    OPEN = 1,
    CLOSED = 2,
    DONE = 4,
}

export interface ListState<T> {
    loading: boolean;
    items: T[];
}

export const useLoading = (list: any[], minLength?: number) => {
    const [loading, setLoading] = React.useState(true);
    React.useEffect(() => {
        if (!list) {
            return;
        }
        if (list.length > (minLength || 0)) {
            setLoading(false);
        }
    }, [list, minLength]);
    return loading;
};

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

interface WatchEvent {
    type?: string;
}

// NOTE: findItem and getItem must be React.useCallback functions
export function useWatchList<T, E extends WatchEvent>(url: string, findItem: (item: T, change: E) => boolean, getItem: (change: E) => T, init?: T[]): T[] {
    const [items, setItems] = React.useState(init as T[]);

    React.useEffect(() => {
        setItems(init);
        const stream = fromEventSource(url).pipe(map((res) => JSON.parse(res).result as E));
        let watch = stream.pipe(
            repeat(),
            retryWhen((errors) => errors.pipe(delay(500))),
            scan((items, change) => {
                const index = items.findIndex((i) => findItem(i, change));
                switch (change.type) {
                    case 'Deleted':
                        if (index > -1) {
                            items.splice(index, 1);
                        }
                        break;
                    case 'Updated':
                        if (index > -1) {
                            const updated = {...items[index], ...getItem(change)};
                            items[index] = updated as T;
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
            }, init || []),
            bufferTime(BUFFER_TIME),
            mergeMap((l) => l)
        );

        const sub = handlePageVisibility(() => watch).subscribe((l) => {
            setItems([...l]);
        });

        return () => {
            sub.unsubscribe();
            watch = null;
        };
    }, [init, url, findItem, getItem]);
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

        let liveStream = handlePageVisibility(() =>
            watch.pipe(
                bufferTime(BUFFER_TIME),
                mergeMap((r) => r)
            )
        );

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
