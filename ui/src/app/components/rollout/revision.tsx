import {ActionButton, EffectDiv, formatTimestamp, InfoItemProps, InfoItemRow, ThemeDiv, Tooltip} from 'argo-ui/v2';
import * as React from 'react';
import {RolloutAnalysisRunInfo, RolloutExperimentInfo, RolloutReplicaSetInfo} from '../../../models/rollout/generated';
import {IconForTag} from '../../shared/utils/utils';
import {PodWidget, ReplicaSets} from '../pods/pods';
import {ImageInfo, parseImages} from './rollout';
import './rollout.scss';
import '../pods/pods.scss';

export interface Revision {
    number: string;
    replicaSets: RolloutReplicaSetInfo[];
    experiments: RolloutExperimentInfo[];
    analysisRuns: RolloutAnalysisRunInfo[];
}

const ImageItems = (props: {images: ImageInfo[]}) => {
    return (
        <div>
            {props.images.map((img) => {
                let imageItems = img?.tags?.map((t) => {
                    return {content: t, icon: IconForTag(t)} as InfoItemProps;
                });
                if (imageItems.length === 0) {
                    imageItems = [];
                }
                return <InfoItemRow key={img.image} label={<ThemeDiv className={`image image--${img.color || 'unknown'}`}>{img.image}</ThemeDiv>} items={imageItems} />;
            })}
        </div>
    );
};

interface RevisionWidgetProps {
    revision: Revision;
    initCollapsed?: boolean;
    rollback?: (revision: number) => void;
    current: boolean;
    message: String;
}

export const RevisionWidget = (props: RevisionWidgetProps) => {
    const {revision, initCollapsed} = props;
    const [collapsed, setCollapsed] = React.useState(initCollapsed);
    const icon = collapsed ? 'fa-chevron-circle-down' : 'fa-chevron-circle-up';
    const images = parseImages(revision.replicaSets);
    return (
        <EffectDiv key={revision.number} className='revision'>
            <ThemeDiv className='revision__header'>
                Revision {revision.number}
                <div style={{marginLeft: 'auto', display: 'flex', alignItems: 'center'}}>
                    {!props.current && props.rollback && (
                        <ActionButton
                            action={() => props.rollback(Number(revision.number))}
                            label='ROLLBACK'
                            icon='fa-undo-alt'
                            style={{fontSize: '13px'}}
                            indicateLoading
                            shouldConfirm
                        />
                    )}
                    <ThemeDiv className='revision__header__button' onClick={() => setCollapsed(!collapsed)}>
                        <i className={`fa ${icon}`} />
                    </ThemeDiv>
                </div>
            </ThemeDiv>
            <ThemeDiv className='revision__images'>
                <ImageItems images={images} />
            </ThemeDiv>

            {!collapsed && (
                <React.Fragment>
                    <ReplicaSets replicaSets={revision.replicaSets} />
                    {(revision.analysisRuns || []).length > 0 && (
                        <React.Fragment>
                            <div style={{marginTop: '1em'}}>
                                <AnalysisRunWidget analysisRuns={revision.analysisRuns} />
                            </div>
                        </React.Fragment>
                    )}
                </React.Fragment>
            )}
        </EffectDiv>
    );
};

const AnalysisRunWidget = (props: {analysisRuns: RolloutAnalysisRunInfo[]}) => {
    const {analysisRuns} = props;
    const [opened, setOpened] = React.useState(false);
    const icon = opened ? 'fa-chevron-circle-up' : 'fa-chevron-circle-down';
    return (
        <ThemeDiv className='analysis'>
            <div className='analysis-header'>
                Analysis Runs
                <ThemeDiv onClick={() => setOpened(!opened)}>
                    <i className={`fa ${icon}`} />
                </ThemeDiv>
            </div>
            <div className='analysis__runs'>
                {analysisRuns.map((ar) => (
                    <Tooltip
                        key={ar.objectMeta?.name}
                        content={
                            <React.Fragment>
                                <div>name: {ar.objectMeta.name}</div>
                                <div>created at: {formatTimestamp(JSON.stringify(ar.objectMeta?.creationTimestamp))}</div>
                                {ar?.metrics && (
                                    <div>
                                        AnalysisTemplatesUsed:
                                        {ar.metrics.map((metric) => {
                                            return (
                                                <div>
                                                    {metric?.name && <div> analysisTemplateName: {metric.name}</div>}
                                                    {metric?.count && <div> count: {metric.count}</div>}
                                                    {metric?.successCondition && <div> successCondition: {metric.successCondition}</div>}
                                                    {metric?.failureLimit && <div> failureLimit: {metric.failureLimit}</div>}
                                                    {metric?.inconclusiveLimit && <div> inconclusiveLimit: {metric.inconclusiveLimit}</div>}
                                                </div>
                                            );
                                        })}
                                    </div>
                                )}
                            </React.Fragment>
                        }>
                        <ThemeDiv className={`analysis__run analysis__run--${ar.status ? ar.status.toLowerCase() : 'unknown'}`} />
                    </Tooltip>
                ))}
            </div>

            {opened &&
                analysisRuns.map((ar) => {
                    return (
                        <React.Fragment key={ar.objectMeta?.name}>
                            <div>
                                {ar.objectMeta?.name}
                                <i className={`fa ${ar.status === 'Successful' ? 'fa-check-circle analysis--success' : 'fa-times-circle analysis--failure'}`} />
                            </div>
                            {ar?.jobs && (
                                <div className='analysis__run__jobs'>
                                    {ar.jobs.map((job) => {
                                        return (
                                            <PodWidget
                                                key={job.objectMeta?.name}
                                                name={job.objectMeta.name}
                                                status={job.status}
                                                tooltip={
                                                    <div>
                                                        <div>job-name: {job.objectMeta?.name}</div>
                                                        <div>StartedAt: {formatTimestamp(JSON.stringify(job.startedAt))}</div>
                                                        <div>Status: {job.status}</div>
                                                        <div>AnalysisTemplate: {job.analysisTemplateName}</div>
                                                    </div>
                                                }
                                            />
                                        );
                                    })}
                                </div>
                            )}
                            {ar?.nonJobInfo && (
                                <div className='analysis__run__jobs'>
                                    {ar.nonJobInfo.map((nonJob) => {
                                        return (
                                            <PodWidget
                                                key={new Date(nonJob.startedAt.seconds).getTime()}
                                                name={nonJob.value}
                                                status={nonJob.status}
                                                tooltip={
                                                    <div>
                                                        <pre>Value: {JSON.stringify(JSON.parse(nonJob.value), null, 2)}</pre>
                                                        <div>StartedAt: {formatTimestamp(JSON.stringify(nonJob.startedAt))}</div>
                                                        <div>Status: {nonJob.status}</div>
                                                        <div>AnalysisTemplate: {nonJob.analysisTemplateName}</div>
                                                    </div>
                                                }
                                            />
                                        );
                                    })}
                                </div>
                            )}
                        </React.Fragment>
                    );
                })}
        </ThemeDiv>
    );
};
