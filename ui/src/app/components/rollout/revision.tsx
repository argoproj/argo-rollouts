import * as React from 'react';
import * as moment from 'moment';
import {RolloutAnalysisRunInfo, RolloutExperimentInfo, RolloutReplicaSetInfo} from '../../../models/rollout/generated';
import {IconForTag} from '../../shared/utils/utils';
import {ReplicaSets} from '../pods/pods';
import {ImageInfo, parseImages, parseInitContainerImages} from './rollout';
import './rollout.scss';
import '../pods/pods.scss';
import {ConfirmButton} from '../confirm-button/confirm-button';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {faChevronCircleDown, faChevronCircleUp, faUndoAlt} from '@fortawesome/free-solid-svg-icons';
import {Button, Space, Tooltip, Typography} from 'antd';
import {InfoItemProps, InfoItemRow} from '../info-item/info-item';
import {AnalysisModal} from '../analysis-modal/analysis-modal';
import StatusIndicator from '../analysis-modal/status-indicator/status-indicator';
import {AnalysisStatus} from '../analysis-modal/types';
import {getAdjustedMetricPhase} from '../analysis-modal/transforms';

const {Text} = Typography;

function formatTimestamp(ts: string): string {
    const inputFormat = 'YYYY-MM-DD HH:mm:ss Z z';
    const m = moment(ts, inputFormat);
    if (!ts || !m.isValid()) {
        return 'Never';
    }
    return m.format('MMM D YYYY [at] hh:mm:ss');
}

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
                return <InfoItemRow key={img.image} label={<div className={`image image--${img.color || 'unknown'}`}>{img.image}</div>} items={imageItems} />;
            })}
        </div>
    );
};

interface RevisionWidgetProps {
    revision: Revision;
    initCollapsed?: boolean;
    rollback?: (revision: number) => void;
    current: boolean;
}

export const RevisionWidget = ({current, initCollapsed, revision, rollback}: RevisionWidgetProps) => {
    const [collapsed, setCollapsed] = React.useState(initCollapsed);
    const icon = collapsed ? faChevronCircleDown : faChevronCircleUp;
    const images = parseImages(revision.replicaSets ?? []);
    const initContainerImages = parseInitContainerImages(revision.replicaSets ?? []);
    const combinedImages = images.concat(initContainerImages);
    const hasPods = (revision.replicaSets || []).some((rs) => rs.pods?.length > 0);

    return (
        <div key={revision.number} className='revision'>
            <div className='revision__header'>
                Revision {revision.number}
                <div style={{marginLeft: 'auto', display: 'flex', alignItems: 'center'}}>
                    {!current && rollback && (
                        <ConfirmButton
                            onClick={() => rollback(Number(revision.number))}
                            type='default'
                            icon={<FontAwesomeIcon icon={faUndoAlt} style={{marginRight: '5px'}} />}
                            style={{fontSize: '13px', marginRight: '10px'}}>
                            Rollback
                        </ConfirmButton>
                    )}
                    {hasPods && <FontAwesomeIcon icon={icon} className='revision__header__button' onClick={() => setCollapsed(!collapsed)} />}
                </div>
            </div>
            <div className='revision__images'>
                <ImageItems images={combinedImages} />
            </div>

            {!collapsed && (
                <React.Fragment>
                    <ReplicaSets replicaSets={revision.replicaSets} />
                    {(revision.analysisRuns || []).length > 0 && (
                        <div style={{marginTop: '1em'}}>
                            <AnalysisRunWidget analysisRuns={revision.analysisRuns} images={images} revision={revision.number} />
                        </div>
                    )}
                </React.Fragment>
            )}
        </div>
    );
};

const analysisName = (ar: RolloutAnalysisRunInfo): string => {
    const temp = ar.objectMeta?.name?.split('-') ?? '';
    const len = temp.length;
    return len < 2 ? 'Analysis' : `Analysis ${temp[len - 2] + '-' + temp[len - 1]}`;
};

interface AnalysisRunWidgetProps {
    analysisRuns: RolloutAnalysisRunInfo[];
    images: ImageInfo[];
    revision: string;
}

const AnalysisRunWidget = ({analysisRuns, images, revision}: AnalysisRunWidgetProps) => {
    const [selectedAnalysis, setSelectedAnalysis] = React.useState<RolloutAnalysisRunInfo>(null);
    const imageNames = images.map((img) => img.image);

    return (
        <div className='analysis'>
            <div className='analysis-header'>Analysis Runs</div>
            <div className='analysis__runs'>
                {analysisRuns.map((ar) => (
                    <Tooltip
                        key={ar.objectMeta?.name}
                        title={
                            <>
                                <div>
                                    <b>Name:</b> {ar.objectMeta.name}
                                </div>
                                <div>
                                    <b>Created at: </b>
                                    {formatTimestamp(JSON.stringify(ar.objectMeta?.creationTimestamp))}
                                </div>
                                <div>
                                    <b>Status: </b>
                                    {ar.status}
                                </div>
                            </>
                        }>
                        <div
                            className={`analysis__runs-action ${
                                ar.status === 'Running' ? 'analysis--pending' : ar.status === 'Successful' ? 'analysis--success' : 'analysis--failure'
                            }`}>
                            <Button onClick={() => (selectedAnalysis?.objectMeta.name === ar.objectMeta.name ? setSelectedAnalysis(null) : setSelectedAnalysis(ar))}>
                                <Space size='small'>
                                    <StatusIndicator size='small' status={getAdjustedMetricPhase(ar.status as AnalysisStatus)} />
                                    <Text>{analysisName(ar)}</Text>
                                </Space>
                            </Button>
                        </div>
                    </Tooltip>
                ))}
            </div>
            {selectedAnalysis !== null && (
                <AnalysisModal
                    analysis={analysisRuns.find((ar) => ar.objectMeta.name === selectedAnalysis.objectMeta.name)}
                    analysisName={analysisName(selectedAnalysis)}
                    images={imageNames}
                    revision={revision}
                    open={selectedAnalysis !== null}
                    onClose={() => setSelectedAnalysis(null)}
                />
            )}
        </div>
    );
};
