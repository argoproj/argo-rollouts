import {ActionButton, EffectDiv, formatTimestamp, InfoItemProps, InfoItemRow, Menu, ThemeDiv, Tooltip} from 'argo-ui/v2';
import * as React from 'react';
import {RolloutAnalysisRunInfo, RolloutExperimentInfo, RolloutReplicaSetInfo, RolloutJobInfo} from '../../../models/rollout/generated';
import {IconForTag} from '../../shared/utils/utils';
import {ReplicaSets} from '../pods/pods';
import {ImageInfo, parseImages} from './rollout';
import './rollout.scss';

export interface Revision {
    number: number;
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
                        <ActionButton action={() => props.rollback(revision.number)} label='ROLLBACK' icon='fa-undo-alt' style={{fontSize: '13px'}} indicateLoading shouldConfirm />
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
                                <AnalysisRunWidget analysisRuns={revision.analysisRuns} message={props.message} />
                            </div>
                        </React.Fragment>
                    )}
                </React.Fragment>
            )}
        </EffectDiv>
    );
};

const AnalysisRunWidget = (props: {analysisRuns: RolloutAnalysisRunInfo[]; message: String}) => {
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
                {analysisRuns.map((ar, i) => (
                    <Tooltip
                        content={
                            <React.Fragment>
                                <div>{ar.objectMeta.name}</div>
                                <div>Created at {formatTimestamp(JSON.stringify(ar.objectMeta.creationTimestamp))}</div>
                                <div>{ar.status}</div>
                            </React.Fragment>
                        }>
                        <ThemeDiv className={`analysis__run analysis__run--${ar.status ? ar.status.toLowerCase() : 'unknown'}`} />
                    </Tooltip>
                ))}
            </div>

            {opened &&
                analysisRuns.map((ar) => {
                    return (
                        <React.Fragment>
                            <div>
                                {ar.objectMeta.name}
                                <i className={`fa ${ar.status === 'Successful' ? 'fa-check-circle analysis--success' : 'fa-times-circle analysis--failure'}`} />
                            </div>
                            {ar?.jobs && (
                                <div className='analysis__run__jobs'>
                                    {ar.jobs.map((job) => {
                                        return <AnalysisRunDetail key={job.objectMeta.name} job={job} />;
                                    })}
                                </div>
                            )}
                        </React.Fragment>
                    );
                })}
        </ThemeDiv>
    );
};

const AnalysisRunDetail = ({job}: {job: RolloutJobInfo}) => {
    return (
        <Menu items={[{label: 'Copy Name', action: () => navigator.clipboard.writeText(job.objectMeta?.name), icon: 'fa-clipboard'}]}>
            <Tooltip
                content={
                    <div>
                        <div>job-name: {job.objectMeta?.name}</div>
                        <div>Status: {job.status}</div>
                    </div>
                }>
                <i
                    className={`analysis__run__jobs-icon fa ${
                        job.status === 'Successful' ? 'fa-check analysis__run__jobs-icon--success' : 'fa-times analysis__run__jobs-icon--failure'
                    }`}
                />
            </Tooltip>
        </Menu>
    );
};
