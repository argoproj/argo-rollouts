import * as React from 'react';

import './rollouts-grid.scss';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {RolloutWidget} from '../rollout-widget/rollout-widget';

export const RolloutsGrid = ({rollouts}: {rollouts: RolloutInfo[]}) => {
    return (
        <div className='rollouts-grid'>
            {rollouts.map((rollout, i) => (
                <RolloutWidget key={rollout.objectMeta?.uid} rollout={rollout} deselect={() => {}} />
            ))}
        </div>
    );
};
