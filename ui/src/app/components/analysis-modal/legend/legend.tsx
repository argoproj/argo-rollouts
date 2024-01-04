import * as React from 'react';

import {Space, Typography} from 'antd';

import {AnalysisStatus} from '../types';
import StatusIndicator from '../status-indicator/status-indicator';

import classNames from 'classnames';

const {Text} = Typography;

interface LegendItemProps {
    label: string;
    status: AnalysisStatus;
}

const LegendItem = ({label, status}: LegendItemProps) => (
    <Space size={4}>
        <StatusIndicator size='small' status={status} />
        <Text>{label}</Text>
    </Space>
);

const pluralize = (count: number, singular: string, plural: string) => (count === 1 ? singular : plural);

interface LegendProps {
    className?: string[] | string;
    errors: number;
    failures: number;
    inconclusives: number;
    successes: number;
}

const Legend = ({className, errors, failures, inconclusives, successes}: LegendProps) => (
    <Space className={classNames(className)} size='small'>
        <LegendItem status={AnalysisStatus.Successful} label={`${successes} ${pluralize(successes, 'Success', 'Successes')}`} />
        <LegendItem status={AnalysisStatus.Failed} label={`${failures} ${pluralize(failures, 'Failure', 'Failures')}`} />
        <LegendItem status={AnalysisStatus.Error} label={`${errors} ${pluralize(errors, 'Error', 'Errors')}`} />
        {inconclusives > 0 && <LegendItem status={AnalysisStatus.Inconclusive} label={`${inconclusives} Inconclusive`} />}
    </Space>
);

export default Legend;
export {LegendItem};
