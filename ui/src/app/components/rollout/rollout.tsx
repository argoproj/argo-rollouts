import * as React from 'react';
import {useParams} from 'react-router-dom';
import {Helmet} from 'react-helmet';

import './rollout.scss';
import {RolloutActions} from '../rollout-actions/rollout-actions';
import {useServerData} from '../../shared/utils/utils';
import {RolloutServiceApi} from '../../../models/rollout/generated';
import {RolloutStatus, statusIcon} from '../status-icon/status-icon';

export const Rollout = () => {
    const {name} = useParams<{name: string}>();
    const getRollout = React.useCallback(() => new RolloutServiceApi().getRollout(name), [name]);
    const rollout = useServerData(getRollout);
    return (
        <div className='rollout'>
            <Helmet>
                <title>{name} / Argo Rollouts</title>
            </Helmet>
            <div className='rollout__toolbar'>
                <RolloutActions />
            </div>
            <div className='rollout__body'>
                <header>
                    {name} {statusIcon(rollout.status as RolloutStatus)}
                </header>
            </div>
        </div>
    );
};
