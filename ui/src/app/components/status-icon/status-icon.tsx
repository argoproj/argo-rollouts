import * as React from 'react';
import './status-icon.scss';
import {Tooltip} from 'antd';

export enum RolloutStatus {
    Progressing = 'Progressing',
    Degraded = 'Degraded',
    Paused = 'Paused',
    Healthy = 'Healthy',
}

export const StatusIcon = (props: {status: RolloutStatus; showTooltip?: boolean; defaultIcon?: String}): JSX.Element => {
    let icon, className;
    let spin = false;
    const showTooltip = props.showTooltip ?? true;
    const defaultIcon = props.defaultIcon ?? 'fa-question-circle';
    const {status} = props;
    switch (status) {
        case 'Progressing': {
            icon = 'fa-circle-notch';
            className = 'progressing';
            spin = true;
            break;
        }
        case 'Healthy': {
            icon = 'fa-check-circle';
            className = 'healthy';
            break;
        }
        case 'Paused': {
            icon = 'fa-pause-circle';
            className = 'paused';
            break;
        }
        case 'Degraded': {
            icon = 'fa-times-circle';
            className = 'degraded';
            break;
        }
        default: {
            icon = defaultIcon;
            className = 'unknown';
        }
    }
    return (
        <React.Fragment>
            {showTooltip && (
                <Tooltip title={status}>
                    <i className={`fa ${icon} status-icon--${className} ${spin ? 'fa-spin' : ''}`} />
                </Tooltip>
            )}
            {!showTooltip && <i className={`fa ${icon} status-icon--${className} ${spin ? 'fa-spin' : ''}`} />}
        </React.Fragment>
    );
};

export enum ReplicaSetStatus {
    Running = 'Running',
    Degraded = 'Degraded',
    ScaledDown = 'ScaledDown',
    Healthy = 'Healthy',
    Progressing = 'Progressing',
}

export const ReplicaSetStatusIcon = (props: {status: ReplicaSetStatus; showTooltip?: boolean; defaultIcon?: String}) => {
    let icon, className;
    let spin = false;
    const showTooltip = props.showTooltip ?? true;
    const defaultIcon = props.defaultIcon ?? 'fa-question-circle';
    const {status} = props;
    switch (status) {
        case 'Healthy':
        case 'Running': {
            icon = 'fa-check-circle';
            className = 'healthy';
            break;
        }
        case 'ScaledDown': {
            icon = 'fa-arrow-alt-circle-down';
            className = 'paused';
            break;
        }
        case 'Degraded': {
            icon = 'fa-times-circle';
            className = 'degraded';
            break;
        }
        case 'Progressing': {
            icon = 'fa-circle-notch';
            spin = true;
            className = 'progressing';
            break;
        }
        default: {
            icon = defaultIcon;
            className = 'unknown';
        }
    }
    return (
        <React.Fragment>
            {showTooltip && (
                <Tooltip title={status}>
                    <i className={`fa ${icon} status-icon--${className} ${spin ? 'fa-spin' : ''}`} />
                </Tooltip>
            )}
            {!showTooltip && <i className={`fa ${icon} status-icon--${className} ${spin ? 'fa-spin' : ''}`} />}
        </React.Fragment>
    );
};
