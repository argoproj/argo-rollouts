import {RolloutRolloutWatchEvent, RolloutServiceApiFetchParamCreator} from '../../../models/rollout/generated';
import {ListState, useLoading, useWatch, useWatchList} from '../utils/watch';
import {RolloutInfo} from '../../../models/rollout/rollout';
import * as React from 'react';
import {NamespaceContext, RolloutAPIContext} from '../context/api';

export const useRollouts = (): RolloutInfo[] => {
    const api = React.useContext(RolloutAPIContext);
    const namespaceCtx = React.useContext(NamespaceContext);
    const [rollouts, setRollouts] = React.useState([]);

    React.useEffect(() => {
        const fetchList = async () => {
            const list = await api.rolloutServiceListRolloutInfos(namespaceCtx.namespace);
            setRollouts(list.rollouts || []);
        };
        fetchList();
    }, [api, namespaceCtx]);

    return rollouts;
};

export const useWatchRollouts = (): ListState<RolloutInfo> => {
    const findRollout = React.useCallback((ri: RolloutInfo, change: RolloutRolloutWatchEvent) => ri.objectMeta.name === change.rolloutInfo?.objectMeta?.name, []);
    const getRollout = React.useCallback((c) => c.rolloutInfo as RolloutInfo, []);
    const namespaceCtx = React.useContext(NamespaceContext);
    const streamUrl = RolloutServiceApiFetchParamCreator().rolloutServiceWatchRolloutInfos(namespaceCtx.namespace).url;

    const init = useRollouts();
    const loading = useLoading(init);

    const [rollouts, setRollouts] = React.useState(init);
    const liveList = useWatchList<RolloutInfo, RolloutRolloutWatchEvent>(streamUrl, findRollout, getRollout, rollouts);

    React.useEffect(() => {
        setRollouts(init);
    }, [init, loading]);

    return {
        items: liveList,
        loading,
    } as ListState<RolloutInfo>;
};

export const useWatchRollout = (name: string, subscribe: boolean, timeoutAfter?: number, callback?: (ri: RolloutInfo) => void): [RolloutInfo, boolean] => {
    const namespaceCtx = React.useContext(NamespaceContext);
    name = name || '';
    const isEqual = React.useCallback((a, b) => {
        if (!a.objectMeta || !b.objectMeta) {
            return false;
        }

        return JSON.parse(a.objectMeta.resourceVersion) === JSON.parse(b.objectMeta.resourceVersion);
    }, []);
    const streamUrl = RolloutServiceApiFetchParamCreator().rolloutServiceWatchRolloutInfo(namespaceCtx.namespace, name).url;
    const ri = useWatch<RolloutInfo>(streamUrl, subscribe, isEqual, timeoutAfter);
    if (callback && ri.objectMeta) {
        callback(ri);
    }
    const [loading, setLoading] = React.useState(true);
    if (ri.objectMeta && loading) {
        setLoading(false);
    }
    return [ri, loading];
};
