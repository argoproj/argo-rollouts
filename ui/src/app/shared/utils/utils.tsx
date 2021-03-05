import * as React from 'react';
import {RolloutCondition} from '../../../models/rollout/rollout';
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

export function latestCondition(conditions: RolloutCondition[]): RolloutCondition {
    if (conditions.length === 0) return {} as RolloutCondition;
    let latest = conditions[0];
    conditions.forEach((condition) => {
        const curTimestamp = moment(condition.lastUpdateTime as string);
        const latestTimestamp = moment(latest.lastUpdateTime as string);
        if (latestTimestamp.isSameOrBefore(curTimestamp)) {
            latest = condition;
        }
    });
    return latest;
}

export function formatTimestamp(ts: string): string {
    const inputFormat = 'YYYY-MM-DDTHH:mm:ss[Z]';
    const m = moment(ts, inputFormat);
    if (!ts || !m.isValid()) {
        return 'Never';
    }
    return m.format('MMM D YYYY [at] hh:mm:ss');
}
