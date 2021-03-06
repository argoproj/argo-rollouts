import {RolloutRolloutWatchEvent, RolloutServiceApiFetchParamCreator} from '../../../models/rollout/generated';
import {useWatch} from '../utils/watch';
import {RolloutInfo} from '../../../models/rollout/rollout';
import * as React from 'react';

export const useWatchRollouts = (init?: RolloutInfo[]): [RolloutInfo[], boolean, boolean] => {
    const findRollout = React.useCallback((ri: RolloutInfo, change: RolloutRolloutWatchEvent) => ri.objectMeta.name === change.rolloutInfo?.objectMeta?.name, []);
    const getRollout = React.useCallback((c) => c.rolloutInfo as RolloutInfo, []);
    const streamUrl = RolloutServiceApiFetchParamCreator().watchRollouts().url;
    return useWatch<RolloutInfo, RolloutRolloutWatchEvent>(streamUrl, findRollout, getRollout, init);
};
