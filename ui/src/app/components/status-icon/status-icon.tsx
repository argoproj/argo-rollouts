import * as React from 'react';

import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {faCheck, faCircleNotch, faExclamationTriangle, faTimes} from '@fortawesome/free-solid-svg-icons';
import {faCheckCircle, faPauseCircle, faQuestionCircle, faTimesCircle} from '@fortawesome/free-regular-svg-icons';

import './status-icon.scss';
import './pod-icon.scss';

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

export const PodIcon = (props: {status: string}) => {
    const {status} = props;
    let icon, className;
    let spin = false;
    if (status.startsWith('Init:')) {
        icon = faCircleNotch;
        spin = true;
    }
    if (status.startsWith('Signal:') || status.startsWith('ExitCode:')) {
        icon = faTimes;
    }
    if (status.endsWith('Error') || status.startsWith('Err')) {
        icon = faExclamationTriangle;
    }

    switch (status) {
        case 'Pending':
        case 'Terminating':
        case 'ContainerCreating':
            icon = faCircleNotch;
            className = 'pending';
            spin = true;
            break;
        case 'Running':
        case 'Completed':
            icon = faCheck;
            className = 'success';
            break;
        case 'Failed':
        case 'InvalidImageName':
        case 'CrashLoopBackOff':
            className = 'failure';
            icon = faTimes;
            break;
        case 'ImagePullBackOff':
        case 'RegistryUnavailable':
            className = 'warning';
            icon = faExclamationTriangle;
            break;
        default:
            className = 'unknown';
            icon = faQuestionCircle;
    }

    return (
        <div className={`pod-icon pod-icon--${className}`}>
            <FontAwesomeIcon icon={icon} spin={spin} />
        </div>
    );
};
