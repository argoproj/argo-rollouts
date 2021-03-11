import {RolloutRolloutWatchEvent, RolloutServiceApiFetchParamCreator} from '../../../models/rollout/generated';
import {useWatch, useWatchList} from '../utils/watch';
import {RolloutInfo} from '../../../models/rollout/rollout';
import * as React from 'react';

export const useWatchRollouts = (init?: RolloutInfo[]): [RolloutInfo[], boolean, boolean] => {
    const findRollout = React.useCallback((ri: RolloutInfo, change: RolloutRolloutWatchEvent) => ri.objectMeta.name === change.rolloutInfo?.objectMeta?.name, []);
    const getRollout = React.useCallback((c) => c.rolloutInfo as RolloutInfo, []);
    const streamUrl = RolloutServiceApiFetchParamCreator().watchRollouts().url;
    return useWatchList<RolloutInfo, RolloutRolloutWatchEvent>(streamUrl, findRollout, getRollout, init);
};

export const useWatchRollout = (name: string, subscribe: boolean, timeoutAfter?: number, callback?: (ri: RolloutInfo) => void): [RolloutInfo, boolean] => {
    name = name || '';
    const streamUrl = RolloutServiceApiFetchParamCreator().watchRollout(name).url;
    const ri = useWatch<RolloutInfo>(
        streamUrl,
        subscribe,
        (a, b) => {
            if (!a.objectMeta || !b.objectMeta) {
                return false;
            }

            return JSON.parse(a.objectMeta.resourceVersion) === JSON.parse(b.objectMeta.resourceVersion);
        },
        timeoutAfter
    );
    if (callback && ri.objectMeta) {
        callback(ri);
    }
    const [loading, setLoading] = React.useState(true);
    if (ri.objectMeta && loading) {
        setLoading(false);
    }
    return [ri, loading];
};
