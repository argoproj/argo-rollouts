import * as React from 'react';
import * as moment from 'moment';
import {Typography} from 'antd';

import {AnalysisStatus, FunctionalStatus} from '../types';
import Header from '../header/header';

import classNames from 'classnames/bind';
import './styles.scss';

const cx = classNames;

const {Text} = Typography;

const timeRangeFormatter = (start: number, end: number | null) => {
    const startFormatted = moment(start).format('LLL');
    if (end === null) {
        return `${startFormatted} - present`;
    }
    const isSameDate = moment(start).isSame(moment(end), 'day');
    const endFormatted = isSameDate ? moment(end).format('LT') : moment(end).format('LLL');
    return `${startFormatted} - ${endFormatted}`;
};

interface SummaryPanelProps {
    className?: string[] | string;
    endTime: number | null;
    images: string[];
    message?: string;
    revision: string;
    startTime: number | null;
    status: AnalysisStatus;
    substatus?: FunctionalStatus.ERROR | FunctionalStatus.WARNING;
    title: string;
}

const SummaryPanel = ({className, endTime, images, message, revision, startTime, status, substatus, title}: SummaryPanelProps) => (
    <div className={cx(className)}>
        <Header className={cx('analysis-header')} title={title} status={status} substatus={substatus} />
        {images.length > 0 && (
            <div className={cx('summary-section')}>
                <Text className={cx('label')} strong>
                    {images.length > 1 ? `Versions` : `Version`}
                </Text>
                <Text>{images.join(', ')}</Text>
            </div>
        )}
        <div className={cx('summary-section')}>
            <Text className={cx('label')} strong>
                Revision
            </Text>
            <Text>{revision}</Text>
        </div>
        {startTime !== null && (
            <div className={cx('summary-section')}>
                <Text className={cx('label')} strong>
                    Run time
                </Text>
                <Text>{timeRangeFormatter(startTime, endTime)}</Text>
            </div>
        )}
        {message && (
            <div className={cx('summary-section')}>
                <Text className={cx('label')} strong>
                    Summary
                </Text>
                <Text>{message}</Text>
            </div>
        )}
    </div>
);

export default SummaryPanel;
