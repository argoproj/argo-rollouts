import * as React from 'react';

import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {faCircleNotch} from '@fortawesome/free-solid-svg-icons';
import {faCheckCircle, faPauseCircle, faQuestionCircle, faTimesCircle} from '@fortawesome/free-regular-svg-icons';
import {RolloutCondition} from '../../../models/rollout/rollout';

import './status-icon.scss';

export const conditionIcon = (condition: RolloutCondition): JSX.Element => {
    let icon, className;
    let spin = false;
    switch (condition.type) {
        case 'Progressing': {
            icon = faCircleNotch;
            className = 'progressing';
            spin = true;
            break;
        }
        case 'Available': {
            icon = faCheckCircle;
            className = 'healthy';
            break;
        }
        default: {
            icon = faQuestionCircle;
            className = 'unknown';
        }
    }
    return <FontAwesomeIcon icon={icon} className={`condition-icon--${className}`} spin={spin} />;
};

export enum RolloutStatus {
    Progressing = 'Progressing',
    Degraded = 'Degraded',
    Paused = 'Paused',
    Healthy = 'Healthy',
}

export const statusIcon = (status: RolloutStatus): JSX.Element => {
    let icon, className;
    let spin = false;
    switch (status) {
        case 'Progressing': {
            icon = faCircleNotch;
            className = 'progressing';
            spin = true;
            break;
        }
        case 'Healthy': {
            icon = faCheckCircle;
            className = 'healthy';
            break;
        }
        case 'Paused': {
            icon = faPauseCircle;
            className = 'paused';
            break;
        }
        case 'Degraded': {
            icon = faTimesCircle;
            className = 'degraded';
            break;
        }
        default: {
            icon = faQuestionCircle;
            className = 'unknown';
        }
    }
    return <FontAwesomeIcon icon={icon} className={`status-icon--${className}`} spin={spin} />;
};
