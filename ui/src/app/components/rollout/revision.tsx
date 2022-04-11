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
    const [selection, setSelection] = React.useState<RolloutAnalysisRunInfo>(null);

    return (
        <ThemeDiv className='analysis'>
            <div className='analysis-header'>Analysis Runs</div>
            <div className='analysis__runs'>
                {analysisRuns.map((ar, index) => (
                    <Tooltip
                        key={ar.objectMeta?.name}
                        content={
                            <React.Fragment>
                                <div>
                                    <b>Name:</b> {ar.objectMeta.name}
                                </div>
                                <div>
                                    <b>Created at: </b>
                                    {formatTimestamp(JSON.stringify(ar.objectMeta?.creationTimestamp))}
                                </div>
                            </React.Fragment>
                        }>
                        <ActionButton
                            action={() => (selection?.objectMeta.name === ar.objectMeta.name ? setSelection(null) : setSelection(ar))}
                            label={`Analysis ${index}`}
                            icon={`fa ${ar.status === 'Successful' ? 'fa-check ' : 'fa-times '}`}
                            style={{
                                fontSize: '16px',
                                lineHeight: 1,
                                border: '1px solid',
                                padding: '8px 8px 8px 10px',
                                borderRadius: '12px',
                                color: 'white',
                                backgroundColor: ar.status === 'Successful' ? '#3f946d' : '#f85149',
                                borderColor: ar.status === 'Successful' ? '#3f946d' : '#f85149',
                                cursor: 'pointer',
                            }}
                            transparent
                        />
                    </Tooltip>
                ))}
            </div>

            {selection && (
                <React.Fragment key={selection.objectMeta?.name}>
                    <div>
                        {selection.objectMeta?.name}
                        <i className={`fa ${selection.status === 'Successful' ? 'fa-check-circle analysis--success' : 'fa-times-circle analysis--failure'}`} />
                    </div>
                    {selection?.jobs && (
                        <div className='analysis__run__jobs'>
                            <div className='analysis__run__jobs-list'>
                                {selection.jobs.map((job) => {
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
                            <Tooltip
                                content={selection?.metrics
                                    .filter((metric) => metric.name === selection.jobs[0].analysisTemplateName)
                                    .map((metric) => {
                                        return (
                                            <React.Fragment>
                                                {metric?.name && (
                                                    <div>
                                                        <b>AnalysisTemplateName:</b> {metric.name}
                                                    </div>
                                                )}
                                                {metric?.successCondition && (
                                                    <div>
                                                        <b>SuccessCondition: </b>
                                                        {metric.successCondition}
                                                    </div>
                                                )}
                                                {metric?.failureLimit && (
                                                    <div>
                                                        <b>FailureLimit:</b> {metric.failureLimit}
                                                    </div>
                                                )}
                                                {metric?.inconclusiveLimit && (
                                                    <div>
                                                        <b>InconclusiveLimit: </b>
                                                        {metric.inconclusiveLimit}
                                                    </div>
                                                )}
                                                {metric?.count && (
                                                    <div>
                                                        <b>Count: </b>
                                                        {metric.count}
                                                    </div>
                                                )}
                                            </React.Fragment>
                                        );
                                    })}>
                                <i className='fa fa-info-circle analysis__run__jobs-info' />
                            </Tooltip>
                        </div>
                    )}
                    {selection?.nonJobInfo && (
                        <div className='analysis__run__jobs'>
                            <div className='analysis__run__jobs-list'>
                                {selection.nonJobInfo.map((nonJob) => {
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
                            <Tooltip
                                content={selection?.metrics
                                    .filter((metric) => metric.name === selection.nonJobInfo[0].analysisTemplateName)
                                    .map((metric) => {
                                        return (
                                            <React.Fragment>
                                                {metric?.name && (
                                                    <div>
                                                        <b>AnalysisTemplateName:</b> {metric.name}
                                                    </div>
                                                )}
                                                {metric?.successCondition && (
                                                    <div>
                                                        <b>SuccessCondition: </b>
                                                        {metric.successCondition}
                                                    </div>
                                                )}
                                                {metric?.failureLimit && (
                                                    <div>
                                                        <b>FailureLimit:</b> {metric.failureLimit}
                                                    </div>
                                                )}
                                                {metric?.inconclusiveLimit && (
                                                    <div>
                                                        <b>InconclusiveLimit: </b>
                                                        {metric.inconclusiveLimit}
                                                    </div>
                                                )}
                                                {metric?.count && (
                                                    <div>
                                                        <b>Count: </b>
                                                        {metric.count}
                                                    </div>
                                                )}
                                            </React.Fragment>
                                        );
                                    })}>
                                <i className='fa fa-info-circle analysis__run__jobs-info' />
                            </Tooltip>
                        </div>
                    )}
                </React.Fragment>
            )}
        </ThemeDiv>
    );
};
