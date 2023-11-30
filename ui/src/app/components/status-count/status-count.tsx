import * as React from 'react';

import {RolloutStatus, StatusIcon} from '../status-icon/status-icon';

import './status-count.scss';

export const StatusCount = ({status, count, defaultIcon = 'fa-exclamation-circle', active = false}: {status: String; count: Number; defaultIcon?: String; active?: boolean}) => {
    return (
        <div className={`status-count ${active ? 'active' : ''}`}>
            <div className='status-count__icon'>
                <StatusIcon status={status as RolloutStatus} showTooltip={false} defaultIcon={defaultIcon} />
            </div>
            <div className='status-count__count'>{count}</div>
        </div>
    );
};
