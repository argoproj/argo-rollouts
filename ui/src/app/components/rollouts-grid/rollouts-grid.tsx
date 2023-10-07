import * as React from 'react';

import './rollouts-grid.scss';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {RolloutWidget} from '../rollout-widget/rollout-widget';

export const RolloutsGrid = ({rollouts}: {rollouts: RolloutInfo[]}) => {
    // ({ rollouts, onFavoriteChange, favorites }: { rollouts: RolloutInfo[], onFavoriteChange: (rollout: RolloutInfo) => void, favorites: { [key: string]: boolean } }) => {
    // const handleFavoriteChange = (rollout: RolloutInfo) => {
    //   onFavoriteChange(rollout);
    // };
    return (
        <div className='rollouts-grid'>
            {rollouts.map((rollout, i) => (
                <RolloutWidget key={rollout.objectMeta?.uid} rollout={rollout} deselect={() => {}} />
            ))}
        </div>
    );
};
