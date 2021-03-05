import {RolloutRolloutWatchEvent, RolloutServiceApiFetchParamCreator} from '../../../models/rollout/generated';
import {useWatch} from '../utils/watch';
import {Rollout} from '../../../models/rollout/rollout';
import * as React from 'react';

export const useWatchRollouts = (init?: Rollout[]): Rollout[] => {
    const findRollout = React.useCallback((rollout: Rollout, change: RolloutRolloutWatchEvent) => rollout.metadata.name === change.rollout.metadata.name, []);
    const getRollout = React.useCallback((c) => c.rollout, []);
    const streamUrl = RolloutServiceApiFetchParamCreator().watchRollouts().url;
    return useWatch<Rollout, RolloutRolloutWatchEvent>(streamUrl, findRollout, getRollout, init);
};
