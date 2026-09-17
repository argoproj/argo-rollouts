import {RolloutRolloutWatchEvent, RolloutServiceApiFetchParamCreator} from '../../../models/rollout/generated';
import {ListState, useWatch, useWatchList} from '../utils/watch';
import {RolloutInfo} from '../../../models/rollout/rollout';
import * as React from 'react';
import {NamespaceContext, RolloutAPIContext, getApiBasePath} from '../context/api';
import { notification } from 'antd';
import {failedListState, loadedListState, loadingListState} from './rollout-state';

export const useRollouts = (): ListState<RolloutInfo> => {
    const api = React.useContext(RolloutAPIContext);
    const namespaceCtx = React.useContext(NamespaceContext);
    const [state, setState] = React.useState<ListState<RolloutInfo>>(loadingListState());

    React.useEffect(() => {
        let cancelled = false;

        const fetchList = async () => {
            setState(loadingListState());
            try {
                const list = await api.rolloutServiceListRolloutInfos(namespaceCtx.namespace);
                if (!cancelled) {
                    setState(loadedListState(list.rollouts || []));
                }
            } catch (error) {
                if (!cancelled) {
                    const errorState = failedListState<RolloutInfo>(error);
                    const fetchError = errorState.error;
                    setState(errorState);
                    console.error('Error fetching rollouts:', fetchError);
                    notification.error({
                        message: 'Error fetching rollouts',
                        description: fetchError.message,
                        duration: 8,
                        placement: 'bottomRight',
                    });
                }
            }
        };
        fetchList();

        return () => {
            cancelled = true;
        };
    }, [api, namespaceCtx]);

    return state;
};

export const useWatchRollouts = (): ListState<RolloutInfo> => {
    const findRollout = React.useCallback((ri: RolloutInfo, change: RolloutRolloutWatchEvent) => ri.objectMeta.name === change.rolloutInfo?.objectMeta?.name, []);
    const getRollout = React.useCallback((c) => c.rolloutInfo as RolloutInfo, []);
    const namespaceCtx = React.useContext(NamespaceContext);
    const streamUrl = getApiBasePath() + RolloutServiceApiFetchParamCreator().rolloutServiceWatchRolloutInfos(namespaceCtx.namespace).url;

    const initialState = useRollouts();

    const [rollouts, setRollouts] = React.useState(initialState.items);
    const liveList = useWatchList<RolloutInfo, RolloutRolloutWatchEvent>(streamUrl, findRollout, getRollout, rollouts);

    React.useEffect(() => {
        setRollouts(initialState.items);
    }, [initialState.items]);

    return {
        items: liveList,
        loading: initialState.loading,
        error: initialState.error,
    };
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
    const streamUrl = getApiBasePath() + RolloutServiceApiFetchParamCreator().rolloutServiceWatchRolloutInfo(namespaceCtx.namespace, name).url;
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
