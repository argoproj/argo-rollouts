import * as React from 'react';

import {Space} from 'antd';

import {AnalysisStatus, FunctionalStatus} from '../types';
import StatusIndicator from '../status-indicator/status-indicator';

import classNames from 'classnames/bind';
import './metric-label.scss';

const cx = classNames;

interface AnalysisModalProps {
    label: string;
    status: AnalysisStatus;
    substatus?: FunctionalStatus.ERROR | FunctionalStatus.WARNING;
}

const MetricLabel = ({label, status, substatus}: AnalysisModalProps) => (
    <Space size='small'>
        <StatusIndicator size='small' status={status} substatus={substatus} />
        <span className={cx('metric-label')} title={label}>
            {label}
        </span>
    </Space>
);

export default MetricLabel;
