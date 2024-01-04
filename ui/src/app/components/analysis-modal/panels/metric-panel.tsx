// eslint-disable-file @typescript-eslint/ban-ts-comment
import * as React from 'react';

import {Radio, Typography} from 'antd';
import type {RadioChangeEvent} from 'antd';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {faChartLine, faList} from '@fortawesome/free-solid-svg-icons';

import Header from '../header/header';
import CriteriaList from '../criteria-list/criteria-list';
import Legend from '../legend/legend';
import MetricChart from '../metric-chart/metric-chart';
import MetricTable from '../metric-table/metric-table';
import QueryBox from '../query-box/query-box';
import {AnalysisStatus, FunctionalStatus, TransformedMetricSpec, TransformedMetricStatus} from '../types';
import {isFiniteNumber} from '../transforms';
import {METRIC_CONSECUTIVE_ERROR_LIMIT_DEFAULT, METRIC_FAILURE_LIMIT_DEFAULT, METRIC_INCONCLUSIVE_LIMIT_DEFAULT} from '../constants';

import classNames from 'classnames';
import './styles.scss';

const cx = classNames;

const {Paragraph, Title} = Typography;

interface MetricPanelProps {
    className?: string[] | string;
    metricName: string;
    metricSpec?: TransformedMetricSpec;
    metricResults: TransformedMetricStatus;
    status: AnalysisStatus;
    substatus?: FunctionalStatus.ERROR | FunctionalStatus.WARNING;
}

const MetricPanel = ({className, metricName, metricSpec, metricResults, status, substatus}: MetricPanelProps) => {
    const consecutiveErrorLimit = isFiniteNumber(metricSpec.consecutiveErrorLimit ?? null) ? metricSpec.consecutiveErrorLimit : METRIC_CONSECUTIVE_ERROR_LIMIT_DEFAULT;
    const failureLimit = isFiniteNumber(metricSpec.failureLimit ?? null) ? metricSpec.failureLimit : METRIC_FAILURE_LIMIT_DEFAULT;
    const inconclusiveLimit = isFiniteNumber(metricSpec.inconclusiveLimit ?? null) ? metricSpec.inconclusiveLimit : METRIC_INCONCLUSIVE_LIMIT_DEFAULT;

    const canChartMetric = metricResults.chartable && metricResults.chartMax !== null;

    const [selectedView, setSelectedView] = React.useState(canChartMetric ? 'chart' : 'table');

    const onChangeView = ({target: {value}}: RadioChangeEvent) => {
        setSelectedView(value);
    };

    return (
        <div className={cx(className)}>
            <div className={cx('metric-header')}>
                <Header title={metricName} subtitle={metricResults.statusLabel} status={metricResults.adjustedPhase} substatus={substatus} />
                {canChartMetric && (
                    <Radio.Group onChange={onChangeView} value={selectedView} size='small'>
                        <Radio.Button value='chart'>
                            <FontAwesomeIcon icon={faChartLine} />
                        </Radio.Button>
                        <Radio.Button value='table'>
                            <FontAwesomeIcon icon={faList} />
                        </Radio.Button>
                    </Radio.Group>
                )}
            </div>
            {status === AnalysisStatus.Pending && (
                <Paragraph style={{marginTop: 12}}>
                    {metricName} analysis measurements have not yet begun. Measurement information will appear here when it becomes available.
                </Paragraph>
            )}
            {status !== AnalysisStatus.Pending && metricResults.transformedMeasurements.length === 0 && (
                <Paragraph style={{marginTop: 12}}>Measurement results for {metricName} cannot be displayed.</Paragraph>
            )}
            {status !== AnalysisStatus.Pending && metricResults.transformedMeasurements.length > 0 && (
                <>
                    <Legend
                        className={cx('legend')}
                        errors={metricResults.error ?? 0}
                        failures={metricResults.failed ?? 0}
                        inconclusives={metricResults.inconclusive ?? 0}
                        successes={metricResults.successful ?? 0}
                    />
                    {selectedView === 'chart' && (
                        <MetricChart
                            className={cx('metric-section', 'top-content')}
                            data={metricResults.transformedMeasurements}
                            max={metricResults.chartMax}
                            min={metricResults.chartMin}
                            failThresholds={metricSpec.failThresholds}
                            successThresholds={metricSpec.successThresholds}
                            yAxisLabel={metricResults.name}
                            conditionKeys={metricSpec.conditionKeys}
                        />
                    )}
                    {selectedView === 'table' && (
                        <MetricTable
                            className={cx('metric-section', 'top-content')}
                            data={metricResults.transformedMeasurements}
                            conditionKeys={metricSpec.conditionKeys}
                            failCondition={metricSpec.failConditionLabel}
                            successCondition={metricSpec.successConditionLabel}
                        />
                    )}
                </>
            )}
            <div className={cx('metric-section', 'medium-space')}>
                <Title className={cx('section-title')} level={5}>
                    Pass requirements
                </Title>
                <CriteriaList
                    analysisStatus={status}
                    // @ts-ignore
                    maxConsecutiveErrors={consecutiveErrorLimit}
                    // @ts-ignore
                    maxFailures={failureLimit}
                    // @ts-ignore
                    maxInconclusives={inconclusiveLimit}
                    consecutiveErrors={metricResults.consecutiveError ?? 0}
                    failures={metricResults.failed ?? 0}
                    inconclusives={metricResults.inconclusive ?? 0}
                    showIcons={metricResults.measurements?.length > 0}
                />
            </div>
            {Array.isArray(metricSpec?.queries) && (
                <>
                    <div className={cx('query-header')}>
                        <Title className={cx('section-title')} level={5}>
                            {metricSpec.queries.length > 1 ? 'Queries' : 'Query'}
                        </Title>
                    </div>
                    {metricSpec.queries.map((query) => (
                        <QueryBox key={`query-box-${query}`} className={cx('query-box')} query={query} />
                    ))}
                </>
            )}
        </div>
    );
};

export default MetricPanel;
