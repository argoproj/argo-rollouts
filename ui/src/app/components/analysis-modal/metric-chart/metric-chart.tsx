// eslint-disable-file @typescript-eslint/ban-ts-comment
import * as React from 'react';
import * as moment from 'moment';
import {CartesianGrid, DotProps, Label, Line, LineChart, ReferenceLine, ResponsiveContainer, Tooltip, TooltipProps, XAxis, YAxis} from 'recharts';
import {NameType, ValueType} from 'recharts/types/component/DefaultTooltipContent';
import {Typography} from 'antd';

import {AnalysisStatus, FunctionalStatus, TransformedMeasurement} from '../types';
import {ANALYSIS_STATUS_THEME_MAP} from '../constants';
import {isValidDate} from '../transforms';

import StatusIndicator from '../status-indicator/status-indicator';

import classNames from 'classnames/bind';
import './metric-chart.scss';

const {Text} = Typography;
const cx = classNames;

const CHART_HEIGHT = 254;
const X_AXIS_HEIGHT = 45;

const defaultValueFormatter = (value: number | string | null) => (value === null ? '' : value.toString());

const timeTickFormatter = (axisData?: string) => {
    if (axisData === undefined || !isValidDate(axisData)) {
        return '';
    }
    return moment(axisData).format('LT');
};

type MeasurementDotProps = DotProps & {
    payload?: {
        phase: AnalysisStatus;
        startedAt: string;
        value: string | null;
    };
};

const MeasurementDot = ({cx, cy, payload}: MeasurementDotProps) => (
    <circle r={4} cx={cx} cy={cy ?? CHART_HEIGHT - X_AXIS_HEIGHT} className={`dot-${ANALYSIS_STATUS_THEME_MAP[payload?.phase ?? AnalysisStatus.Unknown] as FunctionalStatus}`} />
);

type TooltipContentProps = TooltipProps<ValueType, NameType> & {
    conditionKeys: string[];
    valueFormatter: (value: number | string | null) => string;
};

const TooltipContent = ({active, conditionKeys, payload, valueFormatter}: TooltipContentProps) => {
    if (!active || payload?.length === 0 || !payload?.[0].payload) {
        return null;
    }

    const data = payload[0].payload;
    let label;
    if (data.phase === AnalysisStatus.Error) {
        label = data.message ?? 'Measurement error';
    } else if (conditionKeys.length > 0) {
        const sublabels = conditionKeys.map((cKey) => (conditionKeys.length > 1 ? `${valueFormatter(data.chartValue[cKey])} (${cKey})` : valueFormatter(data.chartValue[cKey])));
        label = sublabels.join(' , ');
    } else {
        label = valueFormatter(data.chartValue);
    }

    return (
        <div className={cx('metric-chart-tooltip')}>
            <Text className={cx('metric-chart-tooltip-timestamp')} type='secondary' style={{fontSize: 12}}>
                {moment(data.startedAt).format('LTS')}
            </Text>
            <div className={cx('metric-chart-tooltip-status')}>
                <StatusIndicator size='small' status={data.phase} />
                <Text>{label}</Text>
            </div>
        </div>
    );
};

interface MetricChartProps {
    className?: string[] | string;
    conditionKeys: string[];
    data: TransformedMeasurement[];
    failThresholds: number[] | null;
    max: number | null;
    min: number | null;
    successThresholds: number[] | null;
    valueFormatter?: (value: number | string | null) => string;
    yAxisFormatter?: (value: any, index: number) => string;
    yAxisLabel: string;
}

const MetricChart = ({
    className,
    conditionKeys,
    data,
    failThresholds,
    max,
    min,
    successThresholds,
    valueFormatter = defaultValueFormatter,
    yAxisFormatter = defaultValueFormatter,
    yAxisLabel,
}: MetricChartProps) => {
    // show ticks at boundaries of analysis
    // @ts-ignore
    const startingTick = data[0]?.startedAt ?? '';
    // @ts-ignore
    const endingTick = data[data.length - 1]?.finishedAt ?? '';
    const timeTicks: any[] = [startingTick, endingTick];

    return (
        <ResponsiveContainer className={cx(className)} height={CHART_HEIGHT} width='100%'>
            <LineChart
                className={cx('metric-chart')}
                data={data}
                margin={{
                    top: 0,
                    right: 0,
                    left: 0,
                    bottom: 0,
                }}>
                <CartesianGrid strokeDasharray='4 4' />
                <XAxis className={cx('chart-axis')} height={X_AXIS_HEIGHT} dataKey='startedAt' ticks={timeTicks} tickFormatter={timeTickFormatter} />
                <YAxis className={cx('chart-axis')} width={60} domain={[min ?? 0, max ?? 'auto']} tickFormatter={yAxisFormatter}>
                    <Label className={cx('chart-label')} angle={-90} dx={-20} position='inside' value={yAxisLabel} />
                </YAxis>
                <Tooltip content={<TooltipContent conditionKeys={conditionKeys} valueFormatter={valueFormatter} />} filterNull={false} isAnimationActive={true} />
                {failThresholds !== null && (
                    <>
                        {failThresholds.map((threshold) => (
                            <ReferenceLine key={`fail-line-${threshold}`} className={cx('reference-line', 'is-ERROR')} y={threshold} />
                        ))}
                    </>
                )}
                {successThresholds !== null && (
                    <>
                        {successThresholds.map((threshold) => (
                            <ReferenceLine key={`success-line-${threshold}`} className={cx('reference-line', 'is-SUCCESS')} y={threshold} />
                        ))}
                    </>
                )}
                {conditionKeys.length === 0 ? (
                    <Line
                        className={cx('chart-line')}
                        dataKey={conditionKeys.length === 0 ? 'chartValue' : `chartValue.${conditionKeys[0]}`}
                        isAnimationActive={false}
                        activeDot={false}
                        dot={<MeasurementDot />}
                    />
                ) : (
                    <>
                        {conditionKeys.map((cKey) => (
                            <Line key={cKey} className={cx('chart-line')} dataKey={`chartValue.${cKey}`} isAnimationActive={false} activeDot={false} dot={<MeasurementDot />} />
                        ))}
                    </>
                )}
            </LineChart>
        </ResponsiveContainer>
    );
};

export default MetricChart;
