import {faCheckCircle, faPauseCircle, faQuestionCircle, faTimesCircle} from '@fortawesome/free-regular-svg-icons';
import {faArrowAltCircleDown, faCircleNotch} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {Tooltip} from 'argo-ux';
import * as React from 'react';
import './status-icon.scss';

export enum RolloutStatus {
    Progressing = 'Progressing',
    Degraded = 'Degraded',
    Paused = 'Paused',
    Healthy = 'Healthy',
}

export const StatusIcon = (props: {status: RolloutStatus}): JSX.Element => {
    let icon, className;
    let spin = false;
    const {status} = props;
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

export enum ReplicaSetStatus {
    Running = 'Running',
    Degraded = 'Degraded',
    ScaledDown = 'ScaledDown',
    Healthy = 'Healthy',
    Progressing = 'Progressing',
}

export const ReplicaSetStatusIcon = (props: {status: ReplicaSetStatus}) => {
    let icon, className;
    let spin = false;
    const {status} = props;
    switch (status) {
        case 'Healthy':
        case 'Running': {
            icon = faCheckCircle;
            className = 'healthy';
            break;
        }
        case 'ScaledDown': {
            icon = faArrowAltCircleDown;
            className = 'paused';
            break;
        }
        case 'Degraded': {
            icon = faTimesCircle;
            className = 'degraded';
            break;
        }
        case 'Progressing': {
            icon = faCircleNotch;
            spin = true;
            className = 'progressing';
            break;
        }
        default: {
            icon = faQuestionCircle;
            className = 'unknown';
        }
    }
    return (
        <Tooltip content={status}>
            <FontAwesomeIcon icon={icon} className={`status-icon--${className}`} spin={spin} />
        </Tooltip>
    );
};
