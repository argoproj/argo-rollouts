import {faPlayCircle} from '@fortawesome/free-regular-svg-icons';
import {faArrowCircleUp, faExclamationCircle, faRedoAlt, faSync} from '@fortawesome/free-solid-svg-icons';
import * as React from 'react';
import {RolloutServiceApi} from '../../../models/rollout/generated';
import {ActionButton} from '../action-button/action-button';

export const Actions = {
    Restart: {
        label: 'RESTART',
        icon: faSync,
        action: (api: RolloutServiceApi, rolloutName: string, cb?: Function) => {
            api.restartRollout(rolloutName);
            if (cb) {
                cb();
            }
        },
    },
    Resume: {
        label: 'RESUME',
        icon: faPlayCircle,
        action: (): any => null,
    },
    Retry: {
        label: 'RETRY',
        icon: faRedoAlt,
        action: (): any => null,
    },
    Abort: {
        label: 'ABORT',
        icon: faExclamationCircle,
        action: (): any => null,
    },
    PromoteFull: {
        label: 'PROMOTE-FULL',
        icon: faArrowCircleUp,
        action: (): any => null,
    },
};

export const RolloutActions = () => (
    <div style={{display: 'flex'}}>
        {Object.values(Actions).map((action) => (
            <ActionButton key={action.label} {...action} />
        ))}
    </div>
);
