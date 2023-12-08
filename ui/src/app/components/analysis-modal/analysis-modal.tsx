import * as React from 'react';
import {Modal, Tabs} from 'antd';
import {RolloutAnalysisRunInfo} from '../../../models/rollout/generated';

import MetricLabel from './metric-label/metric-label';
import {MetricPanel, SummaryPanel} from './panels';
import {analysisEndTime, analysisStartTime, getAdjustedMetricPhase, metricStatusLabel, metricSubstatus, transformMetrics} from './transforms';
import {AnalysisStatus} from './types';

import classNames from 'classnames';
import './styles.scss';

const cx = classNames;

interface AnalysisModalProps {
    analysis: RolloutAnalysisRunInfo;
    analysisName: string;
    images: string[];
    onClose: () => void;
    open: boolean;
    revision: string;
}

export const AnalysisModal = ({analysis, analysisName, images, onClose, open, revision}: AnalysisModalProps) => {
    const analysisResults = analysis.specAndStatus?.status;

    // eslint-disable-next-line @typescript-eslint/ban-ts-comment
    // @ts-ignore
    const analysisStart = analysisStartTime(analysis.objectMeta?.creationTimestamp);
    const analysisEnd = analysisEndTime(analysisResults?.metricResults ?? []);

    const analysisSubstatus = metricSubstatus(
        (analysisResults?.phase ?? AnalysisStatus.Unknown) as AnalysisStatus,
        analysisResults?.runSummary.failed ?? 0,
        analysisResults?.runSummary.error ?? 0,
        analysisResults?.runSummary.inconclusive ?? 0
    );
    const transformedMetrics = transformMetrics(analysis.specAndStatus);

    const adjustedAnalysisStatus = getAdjustedMetricPhase(analysis.status as AnalysisStatus);

    const tabItems = [
        {
            label: <MetricLabel label='Summary' status={adjustedAnalysisStatus} substatus={analysisSubstatus} />,
            key: 'analysis-summary',
            children: (
                <SummaryPanel
                    title={metricStatusLabel((analysis.status ?? AnalysisStatus.Unknown) as AnalysisStatus, analysis.failed ?? 0, analysis.error ?? 0, analysis.inconclusive ?? 0)}
                    status={adjustedAnalysisStatus}
                    substatus={analysisSubstatus}
                    images={images}
                    revision={revision}
                    message={analysisResults.message}
                    startTime={analysisStart}
                    endTime={analysisEnd}
                />
            ),
        },
        ...Object.values(transformedMetrics)
            .sort((a, b) => a.name.localeCompare(b.name))
            .map((metric) => ({
                label: <MetricLabel label={metric.name} status={metric.status.adjustedPhase} substatus={metric.status.substatus} />,
                key: metric.name,
                children: (
                    <MetricPanel
                        metricName={metric.name}
                        status={(metric.status.phase ?? AnalysisStatus.Unknown) as AnalysisStatus}
                        substatus={metric.status.substatus}
                        metricSpec={metric.spec}
                        metricResults={metric.status}
                    />
                ),
            })),
    ];

    return (
        <Modal centered open={open} title={analysisName} onCancel={onClose} width={866} footer={null}>
            <Tabs className={cx('tabs')} items={tabItems} tabPosition='left' size='small' tabBarGutter={12} />
        </Modal>
    );
};
