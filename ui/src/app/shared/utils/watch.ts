import * as React from 'react';
import {Observable} from 'rxjs';
import {delay, map, repeat, retryWhen, scan} from 'rxjs/operators';

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

interface WatchEvent {
    type?: string;
}

export function watchFromUrl<T, E extends WatchEvent>(url: string, findItem: (item: T, change: E) => boolean, getItem: (change: E) => T, init?: T[]): Observable<T[]> {
    const stream = fromEventSource(url).pipe(map((res) => JSON.parse(res.data).result as E));
    return stream.pipe(
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
                        items[index] = getItem(change);
                    } else {
                        items.unshift(getItem(change));
                    }
                    break;
            }
            return items;
        }, init || [])
    );
}

export function useWatch<T>(watchFxn: () => Observable<T[]>): T[] {
    const [items, setItems] = React.useState([] as T[]);
    React.useEffect(() => {
        watchFxn().subscribe((list) => {
            setItems(list);
        });
    }, [watchFxn]);
    return items;
}
