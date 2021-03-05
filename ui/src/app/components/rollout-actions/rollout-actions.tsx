import {faPlayCircle} from '@fortawesome/free-regular-svg-icons';
import {faArrowCircleUp, faExclamationCircle, faRedoAlt, faSync} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';
import {ActionButton} from '../action-button/action-button';

const Actions = [
    {
        label: 'RESTART',
        icon: faSync,
        action: (): any => null,
    },
    {
        label: 'RESUME',
        icon: faPlayCircle,
        action: (): any => null,
    },
    {
        label: 'RETRY',
        icon: faRedoAlt,
        action: (): any => null,
    },
    {
        label: 'ABORT',
        icon: faExclamationCircle,
        action: (): any => null,
    },
    {
        label: 'PROMOTE-FULL',
        icon: faArrowCircleUp,
        action: (): any => null,
    },
];

export const RolloutActions = () => (
    <div style={{display: 'flex'}}>
        {Actions.map((action) => (
            <ActionButton key={action.label} label={action.label} action={action.action} icon={<FontAwesomeIcon icon={action.icon} />} />
        ))}
    </div>
);
