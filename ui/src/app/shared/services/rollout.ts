import {RolloutRolloutWatchEvent, RolloutServiceApiFetchParamCreator} from '../../../models/rollout/generated';
import {useWatch, watchFromUrl} from '../utils/watch';
import {Rollout} from '../../../models/rollout/rollout';

export const useWatchRollouts = (init?: Rollout[]) => {
    const streamUrl = RolloutServiceApiFetchParamCreator().watchRollouts().url;
    return useWatch(() =>
        watchFromUrl<Rollout, RolloutRolloutWatchEvent>(
            streamUrl,
            (rollout, change) => rollout.metadata.name === change.rollout.metadata.name,
            (change) => change.rollout,
            init
        )
    );
};
