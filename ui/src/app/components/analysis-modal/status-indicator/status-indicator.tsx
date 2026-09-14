import * as React from 'react';

import {AnalysisStatus, FunctionalStatus} from '../types';
import {ANALYSIS_STATUS_THEME_MAP} from '../constants';

import classNames from 'classnames';
import './status-indicator.scss';

const cx = classNames;

interface StatusIndicatorProps {
    children?: React.ReactNode;
    className?: string[] | string;
    size?: 'small' | 'large';
    status: AnalysisStatus;
    substatus?: FunctionalStatus.ERROR | FunctionalStatus.WARNING;
}

const StatusIndicator = ({children, className, size = 'large', status, substatus}: StatusIndicatorProps) => (
    <div className={cx('indicator-wrapper', className)}>
        <div className={cx('indicator', `is-${size}`, `is-${ANALYSIS_STATUS_THEME_MAP[status]}`)}>{children}</div>
        {substatus !== undefined && <div className={cx('substatus', `is-${size}`, `is-${substatus}`)} />}
    </div>
);

export default StatusIndicator;
