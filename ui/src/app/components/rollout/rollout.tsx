import {faChartBar} from '@fortawesome/free-regular-svg-icons';
import {
    faBalanceScale,
    faBalanceScaleRight,
    faBoxes,
    faChevronCircleDown,
    faChevronCircleUp,
    faDove,
    faExclamationCircle,
    faFlask,
    faPalette,
    faPauseCircle,
    faPencilAlt,
    faSave,
    faShoePrints,
    faTimes,
    faUndoAlt,
    faWeight,
    IconDefinition,
} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {ActionButton, Autocomplete, EffectDiv, InfoItem, InfoItemKind, InfoItemProps, InfoItemRow, Spinner, ThemeDiv, Tooltip, useInput, WaitFor} from 'argo-ux';
import * as React from 'react';
import {Helmet} from 'react-helmet';
import {Key, KeybindingContext} from 'react-keyhooks';
import {useHistory, useParams} from 'react-router-dom';
import {
    GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1CanaryStep,
    RolloutAnalysisRunInfo,
    RolloutContainerInfo,
    RolloutExperimentInfo,
    RolloutReplicaSetInfo,
} from '../../../models/rollout/generated';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {NamespaceContext, RolloutAPIContext} from '../../shared/context/api';
import {useWatchRollout} from '../../shared/services/rollout';
import {formatTimestamp, IconForTag, ImageTag} from '../../shared/utils/utils';
import {ReplicaSets} from '../pods/pods';
import {RolloutStatus, StatusIcon} from '../status-icon/status-icon';
import './rollout.scss';

const RolloutActions = React.lazy(() => import('../rollout-actions/rollout-actions'));
interface ImageInfo {
    image: string;
    tags: ImageTag[];
    color?: ImageColor;
}

enum ImageColor {
    Red = 'red',
    Blue = 'blue',
    Green = 'green',
    Orange = 'orange',
    Purple = 'purple',
}

enum Strategy {
    Canary = 'Canary',
    BlueGreen = 'BlueGreen',
}

const parseImages = (replicaSets: RolloutReplicaSetInfo[]): ImageInfo[] => {
    const images: {[key: string]: ImageInfo} = {};
    const unknownImages: {[key: string]: boolean} = {};
    (replicaSets || []).forEach((rs) => {
        (rs.images || []).forEach((img) => {
            const tags: ImageTag[] = [];

            if (rs.canary) {
                tags.push(ImageTag.Canary);
            }
            if (rs.stable) {
                tags.push(ImageTag.Stable);
            }
            if (rs.active) {
                tags.push(ImageTag.Active);
            }
            if (rs.preview) {
                tags.push(ImageTag.Preview);
            }

            if (images[img]) {
                images[img].tags = [...tags, ...images[img].tags];
            } else {
                images[img] = {
                    image: img,
                    tags: tags,
                };
            }

            if (images[img].tags.length === 0) {
                unknownImages[img] = true;
            } else {
                unknownImages[img] = false;
            }
        });
    });

    const imgArray = Object.values(images);
    imgArray.sort((a, b) => {
        return unknownImages[a.image] ? 1 : -1;
    });
    return imgArray;
};

export const Rollout = () => {
    const {name} = useParams<{name: string}>();

    const [rollout, loading] = useWatchRollout(name, true);
    const api = React.useContext(RolloutAPIContext);
    const namespace = React.useContext(NamespaceContext);
    const images = parseImages(rollout.replicaSets || []);

    for (const img of images) {
        for (const container of rollout.containers || []) {
            if (img.image === container.image) {
                img.color = ImageColor.Blue;
            }
        }
    }
    const curStep = parseInt(rollout.step, 10) || (rollout.steps || []).length;
    const revisions = ProcessRevisions(rollout);

    const {useKeybinding} = React.useContext(KeybindingContext);
    const [editing, setEditing] = React.useState(false);
    const history = useHistory();

    useKeybinding(Key.L, () => {
        if (editing) {
            return false;
        }
        history.push('/rollouts');
        return true;
    });

    return (
        <div className='rollout'>
            <Helmet>
                <title>{name} / Argo Rollouts</title>
            </Helmet>
            <ThemeDiv className='rollout__toolbar'>
                <ThemeDiv className='rollout__header'>
                    <div style={{marginRight: '5px'}}>{name}</div> <StatusIcon status={rollout.status as RolloutStatus} />
                </ThemeDiv>
                <div className='rollout__toolbar__actions'>
                    <React.Suspense fallback={<Spinner />}>
                        <RolloutActions rollout={rollout} />
                    </React.Suspense>
                </div>
            </ThemeDiv>

            <ThemeDiv className='rollout__body'>
                <WaitFor loading={loading}>
                    <div className='rollout__row rollout__row--top'>
                        <ThemeDiv className='info rollout__info'>
                            <div className='info__title'>Summary</div>

                            <InfoItemRow
                                items={{content: rollout.strategy, icon: iconForStrategy(rollout.strategy as Strategy), kind: rollout.strategy?.toLowerCase() as InfoItemKind}}
                                label='Strategy'
                            />
                            <ThemeDiv className='rollout__info__section'>
                                {rollout.strategy === Strategy.Canary && (
                                    <React.Fragment>
                                        <InfoItemRow items={{content: rollout.step, icon: faShoePrints}} label='Step' />
                                        <InfoItemRow items={{content: rollout.setWeight, icon: faBalanceScaleRight}} label='Set Weight' />
                                        <InfoItemRow items={{content: rollout.actualWeight, icon: faBalanceScale}} label='Actual Weight' />{' '}
                                    </React.Fragment>
                                )}
                            </ThemeDiv>
                        </ThemeDiv>
                        <ThemeDiv className='info rollout__info'>
                            <ContainersWidget
                                images={images}
                                containers={rollout.containers || []}
                                setImage={(container, image, tag) => {
                                    api.rolloutServiceSetRolloutImage({}, namespace, name, container, image, tag);
                                }}
                                editing={editing}
                                setEditing={setEditing}
                            />
                        </ThemeDiv>
                    </div>

                    <div className='rollout__row rollout__row--bottom'>
                        {rollout.replicaSets && rollout.replicaSets.length > 0 && (
                            <ThemeDiv className='info rollout__info rollout__revisions'>
                                <div className='info__title'>Revisions</div>
                                <div style={{marginTop: '1em'}}>
                                    {revisions.map((r, i) => (
                                        <RevisionWidget
                                            key={i}
                                            revision={r}
                                            initCollapsed={false}
                                            rollback={(r) => api.rolloutServiceUndoRollout({}, namespace, name, `${r}`)}
                                            current={i === 0}
                                        />
                                    ))}
                                </div>
                            </ThemeDiv>
                        )}
                        {(rollout.strategy || '').toLowerCase() === 'canary' && rollout.steps && rollout.steps.length > 0 && (
                            <ThemeDiv className='info steps'>
                                <ThemeDiv className='info__title'>Steps</ThemeDiv>
                                <div style={{marginTop: '1em'}}>
                                    {rollout.steps.map((step, i) => (
                                        <Step key={`step-${i}`} step={step} complete={i < curStep} current={i === curStep} last={i === (rollout.steps || []).length - 1} />
                                    ))}
                                </div>
                            </ThemeDiv>
                        )}
                    </div>
                </WaitFor>
            </ThemeDiv>
        </div>
    );
};

const iconForStrategy = (s: Strategy) => {
    switch (s) {
        case Strategy.Canary:
            return faDove;
        case Strategy.BlueGreen:
            return faPalette;
    }
};

const ImageItems = (props: {images: ImageInfo[]}) => {
    return (
        <div>
            {props.images.map((img) => {
                let imageItems = img.tags.map((t) => {
                    return {content: t, icon: IconForTag(t)} as InfoItemProps;
                });
                if (imageItems.length === 0) {
                    imageItems = null;
                }
                return <InfoItemRow key={img.image} label={<ThemeDiv className={`image image--${img.color || 'unknown'}`}>{img.image}</ThemeDiv>} items={imageItems} />;
            })}
        </div>
    );
};

interface Revision {
    number: number;
    replicaSets: RolloutReplicaSetInfo[];
    experiments: RolloutExperimentInfo[];
    analysisRuns: RolloutAnalysisRunInfo[];
}

const ProcessRevisions = (ri: RolloutInfo): Revision[] => {
    if (!ri) {
        return;
    }
    const map: {[key: number]: Revision} = {};

    const emptyRevision = {replicaSets: [], experiments: [], analysisRuns: []} as Revision;

    for (const rs of ri.replicaSets || []) {
        if (!map[rs.revision]) {
            map[rs.revision] = {...emptyRevision};
        }
        map[rs.revision].number = rs.revision;
        map[rs.revision].replicaSets = [...map[rs.revision].replicaSets, rs];
    }

    for (const ar of ri.analysisRuns || []) {
        if (!map[ar.revision]) {
            map[ar.revision] = {...emptyRevision};
        }
        map[ar.revision].number = ar.revision;
        map[ar.revision].analysisRuns = [...map[ar.revision].analysisRuns, ar];
    }

    const revisions: Revision[] = [];
    const prevRn = 0;
    Object.keys(map).forEach((key) => {
        const rn = parseInt(key);
        if (rn > prevRn) {
            revisions.unshift(map[rn]);
        } else {
            revisions.push(map[rn]);
        }
    });

    return revisions;
};

const RevisionWidget = (props: {revision: Revision; initCollapsed?: boolean; rollback: (revision: number) => void; current: boolean}) => {
    const {revision, initCollapsed} = props;
    const [collapsed, setCollapsed] = React.useState(initCollapsed);
    const icon = collapsed ? faChevronCircleDown : faChevronCircleUp;
    const images = parseImages(revision.replicaSets);
    return (
        <EffectDiv key={revision.number} className='revision'>
            <ThemeDiv className='revision__header'>
                Revision {revision.number}
                <div style={{marginLeft: 'auto', display: 'flex', alignItems: 'center'}}>
                    {!props.current && (
                        <ActionButton action={() => props.rollback(revision.number)} label='ROLLBACK' icon={faUndoAlt} style={{fontSize: '13px'}} indicateLoading shouldConfirm />
                    )}
                    <ThemeDiv className='revision__header__button' onClick={() => setCollapsed(!collapsed)}>
                        <FontAwesomeIcon icon={icon} />
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
    return (
        <ThemeDiv className='analysis'>
            <div>Analysis Runs</div>
            <div className='analysis__runs'>
                {analysisRuns.map((ar) => (
                    <Tooltip
                        content={
                            <React.Fragment>
                                <div>{ar.objectMeta.name}</div>
                                <div>Created at {formatTimestamp(JSON.stringify(ar.objectMeta.creationTimestamp))}</div>
                            </React.Fragment>
                        }>
                        <ThemeDiv className={`analysis__run analysis__run--${ar.status.toLowerCase() || 'unknown'}`} />
                    </Tooltip>
                ))}
            </div>
        </ThemeDiv>
    );
};

const ContainersWidget = (props: {
    containers: RolloutContainerInfo[];
    images: ImageInfo[];
    setImage: (container: string, image: string, tag: string) => void;
    editing: boolean;
    setEditing: (e: boolean) => void;
}) => {
    const {containers, images, setImage, editing, setEditing} = props;
    const inputMap: {[key: string]: string} = {};
    for (const container of containers) {
        inputMap[container.name] = '';
    }
    const [inputs, setInputs] = React.useState(inputMap);
    const [error, setError] = React.useState(false);

    return (
        <React.Fragment>
            <div style={{display: 'flex', alignItems: 'center', height: '2em'}}>
                <ThemeDiv className='info__title' style={{marginBottom: '0'}}>
                    Containers
                </ThemeDiv>

                {editing ? (
                    <div style={{marginLeft: 'auto', display: 'flex', alignItems: 'center'}}>
                        <ActionButton
                            icon={faTimes}
                            action={() => {
                                setEditing(false);
                                setError(false);
                            }}
                        />
                        <ActionButton
                            label={error ? 'ERROR' : 'SAVE'}
                            style={{marginRight: 0}}
                            icon={error ? faExclamationCircle : faSave}
                            action={() => {
                                for (const container of Object.keys(inputs)) {
                                    const split = inputs[container].split(':');
                                    if (split.length > 1) {
                                        const image = split[0];
                                        const tag = split[1];
                                        setImage(container, image, tag);
                                        setTimeout(() => {
                                            setEditing(false);
                                        }, 350);
                                    } else {
                                        setError(true);
                                    }
                                }
                            }}
                            shouldConfirm
                            indicateLoading={!error}
                        />
                    </div>
                ) : (
                    <FontAwesomeIcon icon={faPencilAlt} onClick={() => setEditing(true)} style={{cursor: 'pointer', marginLeft: 'auto'}} />
                )}
            </div>
            {containers.map((c, i) => (
                <ContainerWidget
                    key={`${c}-${i}`}
                    container={c}
                    images={images}
                    editing={editing}
                    setInput={(img) => {
                        const update = {...inputs};
                        update[c.name] = img;
                        setInputs(update);
                    }}
                />
            ))}
            {containers.length < 2 && (
                <ThemeDiv className='containers__few'>
                    <span style={{marginRight: '5px'}}>
                        <FontAwesomeIcon icon={faBoxes} />
                    </span>
                    Add more containers to fill this space!
                </ThemeDiv>
            )}
        </React.Fragment>
    );
};

const ContainerWidget = (props: {container: RolloutContainerInfo; images: ImageInfo[]; setInput: (image: string) => void; editing: boolean}) => {
    const {container, editing} = props;
    const [, , newImageInput] = useInput(container.image, (val) => props.setInput(val));

    return (
        <div style={{margin: '1em 0', display: 'flex', alignItems: 'center', whiteSpace: 'nowrap'}}>
            <div style={{paddingRight: '20px'}}>{container.name}</div>
            <div style={{width: '100%', display: 'flex', alignItems: 'center', height: '2em'}}>
                {!editing ? <InfoItem content={container.image} /> : <Autocomplete items={props.images.map((img) => img.image)} placeholder='New Image' {...newImageInput} />}
            </div>
        </div>
    );
};

const parseDuration = (duration: string): string => {
    const lastChar = duration[duration.length - 1];
    if (lastChar === 's' || lastChar === 'm' || lastChar === 'h') {
        return `${duration}`;
    }
    return `${duration}s`;
};

const Step = (props: {step: GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1CanaryStep; complete?: boolean; current?: boolean; last?: boolean}) => {
    let icon: IconDefinition;
    let content = '';
    let unit = '';
    if (props.step.setWeight) {
        icon = faWeight;
        content = `Set Weight: ${props.step.setWeight}`;
        unit = '%';
    }
    if (props.step.pause) {
        icon = faPauseCircle;
        if (props.step.pause.duration) {
            content = `Pause: ${parseDuration(`${props.step.pause.duration}`)}`;
        } else {
            content = 'Pause';
        }
    }
    if (props.step.analysis) {
        content = 'Analysis';
        icon = faChartBar;
    }
    if (props.step.setCanaryScale) {
        content = 'Canary Scale';
    }
    if (props.step.experiment) {
        content = 'Experiment';
        icon = faFlask;
    }

    return (
        <React.Fragment>
            <EffectDiv className={`steps__step ${props.complete ? 'steps__step--complete' : ''} ${props.current ? 'steps__step--current' : ''}`}>
                <FontAwesomeIcon icon={icon} /> {content}
                {unit}
            </EffectDiv>
            {!props.last && <ThemeDiv className='steps__connector' />}
        </React.Fragment>
    );
};
