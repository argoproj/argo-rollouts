import * as React from 'react';
import * as moment from 'moment';
import {Table, Typography} from 'antd';

import {AnalysisStatus, TransformedMeasurement, TransformedValueObject} from '../types';
import StatusIndicator from '../status-indicator/status-indicator';
import {isValidDate} from '../transforms';

import classNames from 'classnames/bind';
import './metric-table.scss';

const {Column} = Table;
const {Text} = Typography;

const timeColFormatter = (startTime?: string) => (isValidDate(startTime) ? moment(startTime).format('LTS') : '');

const isObject = (tValue: TransformedValueObject | number | string | null) => typeof tValue === 'object' && !Array.isArray(tValue) && tValue !== null;

const columnValueLabel = (value: any, valueKey: string) => (isObject(value) && valueKey in (value as TransformedValueObject) ? (value as TransformedValueObject)[valueKey] : '');

interface MetricTableProps {
    className?: string[] | string;
    conditionKeys: string[];
    data: TransformedMeasurement[];
    failCondition: string | null;
    successCondition: string | null;
}

const MetricTable = ({className, conditionKeys, data, failCondition, successCondition}: MetricTableProps) => (
    <div className={classNames(className)}>
        <Table className={classNames('metric-table')} dataSource={data} size='small' pagination={false} scroll={{y: 190}}>
            <Column key='status' dataIndex='phase' width={28} render={(phase: AnalysisStatus) => <StatusIndicator size='small' status={phase} />} align='center' />
            {conditionKeys.length > 0 ? (
                <>
                    {conditionKeys.map((cKey) => (
                        <Column
                            key={cKey}
                            title={`Data Point ${cKey}`}
                            render={(columnValue: TransformedMeasurement) => {
                                const isError = columnValue.phase === AnalysisStatus.Error;
                                const errorMessage = columnValue.message ?? 'Measurement error';
                                const label = isError ? errorMessage : columnValueLabel(columnValue.tableValue, cKey);
                                return <span className={classNames(isError && 'error-message')}>{label}</span>;
                            }}
                        />
                    ))}
                </>
            ) : (
                <Column key='value' title='Value' dataIndex='tableValue' />
            )}
            <Column key='time' title='Time' dataIndex='startedAt' render={(startedAt: string) => <span>{timeColFormatter(startedAt)}</span>} />
        </Table>
        {failCondition !== null && (
            <Text className={classNames('condition', 'is-ERROR')} type='secondary'>
                Failure condition: {failCondition}
            </Text>
        )}
        {successCondition !== null && (
            <Text className={classNames('condition', 'is-SUCCESS')} type='secondary'>
                Success condition: {successCondition}
            </Text>
        )}
    </div>
);

export default MetricTable;
