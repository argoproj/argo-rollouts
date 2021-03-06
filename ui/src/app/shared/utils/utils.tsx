import * as React from 'react';
import * as moment from 'moment';

export function useServerData<T>(getData: () => Promise<T>) {
    const [data, setData] = React.useState({} as T);
    React.useEffect(() => {
        const fx = async () => {
            const data = await getData();
            setData(data);
        };
        fx();
    }, [getData]);
    return data as T;
}

export function formatTimestamp(ts: string): string {
    const inputFormat = 'YYYY-MM-DD HH:mm:ss Z z';
    const m = moment(ts, inputFormat);
    if (!ts || !m.isValid()) {
        return 'Never';
    }
    return m.format('MMM D YYYY [at] hh:mm:ss');
}
