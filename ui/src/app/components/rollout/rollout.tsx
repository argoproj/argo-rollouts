import * as React from 'react';
import {Helmet} from 'react-helmet';
import {useParams} from 'react-router-dom';
import {
    GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1CanaryStep,
    GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1HeaderRoutingMatch,
    GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1RolloutExperimentTemplate,
    GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1SetMirrorRoute,
    RolloutReplicaSetInfo,
    RolloutRolloutInfo,
    RolloutServiceApi,
} from '../../../models/rollout/generated';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {NamespaceContext, RolloutAPIContext} from '../../shared/context/api';
import {useWatchRollout} from '../../shared/services/rollout';
import {ImageTag} from '../../shared/utils/utils';
import {RolloutStatus, StatusIcon} from '../status-icon/status-icon';
import {ContainersWidget} from './containers';
import {Revision, RevisionWidget} from './revision';
import './rollout.scss';
import {Fragment} from 'react';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {faChevronCircleDown, faChevronCircleUp, faCircleNotch} from '@fortawesome/free-solid-svg-icons';
import {InfoItemKind, InfoItemRow} from '../info-item/info-item';

const RolloutActions = React.lazy(() => import('../rollout-actions/rollout-actions'));
export interface ImageInfo {
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

export const parseImages = (replicaSets: RolloutReplicaSetInfo[]): ImageInfo[] => {
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

export type ReactStatePair = [boolean, React.Dispatch<React.SetStateAction<boolean>>];

export const RolloutWidget = (props: {rollout: RolloutRolloutInfo; interactive?: {editState: ReactStatePair; api: RolloutServiceApi; namespace: string}}) => {
    const {rollout, interactive} = props;
    const curStep = parseInt(rollout.step, 10) || (rollout.steps || []).length;
    const revisions = ProcessRevisions(rollout);

    const images = parseImages(rollout?.replicaSets || []);

    for (const img of images) {
        for (const container of rollout.containers || []) {
            if (img.image === container.image) {
                img.color = ImageColor.Blue;
            }
        }
    }

    return (
        <div style={{display: 'flex', margin: '0 auto'}}>
            <div style={{marginRight: '20px'}}>
                {(rollout?.strategy || '').toLowerCase() === 'canary' && rollout.steps && rollout.steps.length > 0 && <Steps rollout={rollout} curStep={curStep} />}
            </div>

            <div>
                <div className='rollout__row rollout__row--top'>
                    <div className='info rollout__info'>
                        <div className='info__title'>Summary</div>

                        <InfoItemRow
                            items={{content: rollout.strategy, icon: iconForStrategy(rollout.strategy as Strategy), kind: rollout.strategy?.toLowerCase() as InfoItemKind}}
                            label='Strategy'
                        />
                        <div className='rollout__info__section'>
                            {rollout.strategy === Strategy.Canary && (
                                <React.Fragment>
                                    <InfoItemRow items={{content: rollout.step, icon: 'fa-shoe-prints'}} label='Step' />
                                    <InfoItemRow items={{content: rollout.setWeight, icon: 'fa-balance-scale-right'}} label='Set Weight' />
                                    <InfoItemRow items={{content: rollout.actualWeight, icon: 'fa-balance-scale'}} label='Actual Weight' />{' '}
                                </React.Fragment>
                            )}
                        </div>
                    </div>
                    <div className='info rollout__info'>
                        <ContainersWidget
                            images={images}
                            containers={rollout.containers || []}
                            interactive={
                                interactive
                                    ? {
                                          editState: interactive.editState,
                                          setImage: (container, image, tag) => {
                                              interactive.api.rolloutServiceSetRolloutImage({}, interactive.namespace, rollout.objectMeta?.name, container, image, tag);
                                          },
                                      }
                                    : null
                            }
                        />
                    </div>
                </div>

                <div className='rollout__row rollout__row--bottom'>
                    {rollout.replicaSets && rollout.replicaSets.length > 0 && (
                        <div className='info rollout__info rollout__revisions'>
                            <div className='info__title'>Revisions</div>
                            <div style={{marginTop: '1em'}}>
                                {revisions.map((r, i) => (
                                    <RevisionWidget
                                        key={i}
                                        revision={r}
                                        initCollapsed={false}
                                        rollback={interactive ? (r) => interactive.api.rolloutServiceUndoRollout({}, interactive.namespace, rollout.objectMeta.name, `${r}`) : null}
                                        current={i === 0}
                                        message={rollout.message}
                                    />
                                ))}
                            </div>
                        </div>
                    )}
                </div>
            </div>
        </div>
    );
};

const Steps = (props: {rollout: RolloutInfo; curStep: number}) => (
    <div className='info steps'>
        <div className='info__title'>Steps</div>
        <div style={{marginTop: '1em'}}>
            {props.rollout.steps
                .filter((step) => Object.keys(step).length)
                .map((step, i, arr) => (
                    <Step key={`step-${i}`} step={step} complete={i < props.curStep} current={i === props.curStep} last={i === arr.length - 1} />
                ))}
        </div>
    </div>
);

export const Rollout = () => {
    const {name} = useParams<{name: string}>();

    const [rollout, loading] = useWatchRollout(name, true);
    const api = React.useContext(RolloutAPIContext);
    const namespaceCtx = React.useContext(NamespaceContext);

    const editState = React.useState(false);

    return (
        <div className='rollout'>
            <Helmet>
                <title>{name} / Argo Rollouts</title>
            </Helmet>
            <div className='rollout__toolbar'>
                <div className='rollout__header'>
                    <div style={{marginRight: '5px'}}>{name}</div> <StatusIcon status={rollout.status as RolloutStatus} />
                </div>
                <div className='rollout__toolbar__actions'>
                    <React.Suspense fallback={<FontAwesomeIcon icon={faCircleNotch} spin={true} />}>
                        <RolloutActions rollout={rollout} />
                    </React.Suspense>
                </div>
            </div>

            <div className='rollout__body'>{!loading && <RolloutWidget rollout={rollout} interactive={{api, editState, namespace: namespaceCtx.namespace}} />}</div>
        </div>
    );
};

const iconForStrategy = (s: Strategy) => {
    switch (s) {
        case Strategy.Canary:
            return 'fa-dove';
        case Strategy.BlueGreen:
            return 'fa-palette';
    }
};

const ProcessRevisions = (ri: RolloutInfo): Revision[] => {
    if (!ri) {
        return;
    }
    const map: {[key: string]: Revision} = {};

    const emptyRevision = {replicaSets: [], experiments: [], analysisRuns: []} as Revision;

    for (const rs of ri.replicaSets || []) {
        if (!map[rs.revision]) {
            map[rs.revision] = {...emptyRevision};
        }
        map[rs.revision].number = rs.revision;
        map[rs.revision].replicaSets = [...map[rs.revision]?.replicaSets, rs];
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
            if (map[rn]) {
                revisions.push(map[rn]);
            }
        }
    });

    return revisions;
};

const parseDuration = (duration: string): string => {
    const lastChar = duration[duration.length - 1];
    if (lastChar === 's' || lastChar === 'm' || lastChar === 'h') {
        return `${duration}`;
    }
    return `${duration}s`;
};

const Step = (props: {step: GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1CanaryStep; complete?: boolean; current?: boolean; last?: boolean}) => {
    const [openedTemplate, setOpenedTemplate] = React.useState('');
    const [openCanary, setOpenCanary] = React.useState(false);
    const [openAnalysis, setOpenAnalysis] = React.useState(false);
    const [openHeader, setOpenHeader] = React.useState(false);
    const [openMirror, setOpenMirror] = React.useState(false);

    let icon: string;
    let content = '';
    let unit = '';
    if (props.step.setWeight) {
        icon = 'fa-weight';
        content = `Set Weight: ${props.step.setWeight}`;
        unit = '%';
    }
    if (props.step.pause) {
        icon = 'fa-pause-circle';
        if (props.step.pause.duration) {
            content = `Pause: ${parseDuration(`${props.step.pause.duration}`)}`;
        } else {
            content = 'Pause';
        }
    }
    if (props.step.analysis) {
        content = 'Analysis';
        icon = 'fa-chart-bar';
    }
    if (props.step.setCanaryScale) {
        content = 'Canary Scale';
    }
    if (props.step.experiment) {
        content = 'Experiment';
        icon = 'fa-flask';
    }

    if (props.step.setMirrorRoute) {
        content = `Set Mirror: ${props.step.setMirrorRoute.name}`;
        if (!props.step.setMirrorRoute.match) {
            content = `Remove Mirror: ${props.step.setMirrorRoute.name}`;
        }
    }

    if (props.step.setHeaderRoute) {
        content = `Set Header: ${props.step.setHeaderRoute.name}`;
        if (!props.step.setHeaderRoute.match) {
            content = `Remove Header: ${props.step.setHeaderRoute.name}`;
        }
    }

    return (
        <React.Fragment>
            <div style={{zIndex: 1}} className={`steps__step ${props.complete ? 'steps__step--complete' : ''} ${props.current ? 'steps__step--current' : ''}`}>
                <div
                    className={`steps__step-title ${
                        props.step.experiment ||
                        (props.step.setCanaryScale && openCanary) ||
                        (props.step.analysis && openAnalysis) ||
                        (props.step.setHeaderRoute && openHeader) ||
                        (props.step.setMirrorRoute && openMirror)
                            ? 'steps__step-title--experiment'
                            : ''
                    }`}>
                    {icon && <i className={`fa ${icon}`} />} {content}
                    {unit}
                    {props.step.setCanaryScale && (
                        <div style={{marginLeft: 'auto'}} onClick={() => setOpenCanary(!openCanary)}>
                            <i className={`fa ${openCanary ? 'fa-chevron-circle-up' : 'fa-chevron-circle-down'}`} />
                        </div>
                    )}
                    {props.step.analysis && (
                        <div style={{marginLeft: 'auto'}} onClick={() => setOpenAnalysis(!openAnalysis)}>
                            <i className={`fa ${openAnalysis ? 'fa-chevron-circle-up' : 'fa-chevron-circle-down'}`} />
                        </div>
                    )}
                    {props.step.setHeaderRoute && props.step.setHeaderRoute.match && (
                        <div style={{marginLeft: 'auto'}} onClick={() => setOpenHeader(!openHeader)}>
                            <i className={`fa ${openCanary ? 'fa-chevron-circle-up' : 'fa-chevron-circle-down'}`} />
                        </div>
                    )}
                    {props.step.setMirrorRoute && props.step.setMirrorRoute.match && (
                        <div style={{marginLeft: 'auto'}} onClick={() => setOpenMirror(!openMirror)}>
                            <i className={`fa ${openCanary ? 'fa-chevron-circle-up' : 'fa-chevron-circle-down'}`} />
                        </div>
                    )}
                </div>
                {props.step.experiment?.templates && (
                    <div className='steps__step__content'>
                        {props.step.experiment?.templates.map((template) => {
                            return <ExperimentWidget key={template.name} template={template} opened={openedTemplate === template.name} onToggle={setOpenedTemplate} />;
                        })}
                    </div>
                )}

                {props.step.analysis?.templates && openAnalysis && (
                    <div className='steps__step__content'>
                        <div style={{paddingLeft: 15, marginTop: 12, marginBottom: 8, color: 'rgba(0,0,0, 0.5)'}}>Templates</div>
                        <ul>
                            {props.step.analysis?.templates.map((template) => {
                                return (
                                    <div style={{paddingLeft: 15, fontWeight: 600}} key={template.templateName}>
                                        <li>{template.templateName}</li>
                                    </div>
                                );
                            })}
                        </ul>
                    </div>
                )}
                {props.step?.setCanaryScale && openCanary && <WidgetItem values={props.step.setCanaryScale} />}
                {props.step?.setHeaderRoute && openHeader && <WidgetItemSetHeader values={props.step.setHeaderRoute.match} />}
                {props.step?.setMirrorRoute && openMirror && <WidgetItemSetMirror value={props.step.setMirrorRoute} />}
            </div>
            {!props.last && <div className='steps__connector' />}
        </React.Fragment>
    );
};

const ExperimentWidget = ({
    template,
    opened,
    onToggle,
}: {
    template: GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1RolloutExperimentTemplate;
    opened: boolean;
    onToggle: (name: string) => void;
}) => {
    const icon = opened ? faChevronCircleUp : faChevronCircleDown;
    return (
        <div className='steps__step__content-body'>
            <div className='steps__step__content-header'>
                {template.name}
                <FontAwesomeIcon icon={icon} onClick={() => onToggle(opened ? '' : template.name)} style={{cursor: 'pointer'}} />
            </div>
            {opened && <WidgetItem values={{specRef: template.specRef, weight: template.weight}} />}
        </div>
    );
};

const WidgetItem = ({values}: {values: Record<string, any>}) => {
    return (
        <div>
            {Object.keys(values).map((val) => {
                if (!values[val]) return null;
                return (
                    <Fragment key={val}>
                        <div className='steps__step__content-title'>{val.toUpperCase()}</div>
                        <div className='steps__step__content-value'>{String(values[val])}</div>
                    </Fragment>
                );
            })}
        </div>
    );
};

const WidgetItemSetMirror = ({value}: {value: GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1SetMirrorRoute}) => {
    if (!value) return null;
    return (
        <div>
            <Fragment key={value.name}>
                <div className='steps__step__content-title'>Name</div>
                <div className='steps__step__content-value'>{value.name}</div>
                <div className='steps__step__content-title'>Percentage</div>
                <div className='steps__step__content-value'>{value.percentage}</div>
                {Object.values(value.match).map((val, index) => {
                    if (!val) return null;
                    let stringMatcherValue = '';
                    let stringMatcherType = '';
                    let fragments = [];
                    if (val.path != null) {
                        if (val.path.exact != null) {
                            stringMatcherValue = val.path.exact;
                            stringMatcherType = 'Exact';
                        }
                        if (val.path.prefix != null) {
                            stringMatcherValue = val.path.prefix;
                            stringMatcherType = 'Prefix';
                        }
                        if (val.path.regex != null) {
                            stringMatcherValue = val.path.regex;
                            stringMatcherType = 'Regex';
                        }
                        fragments.push(
                            <Fragment key={value.name}>
                                <div className='steps__step__content-title'>
                                    {index} - Path ({stringMatcherType})
                                </div>
                                <div className='steps__step__content-value'>{stringMatcherValue}</div>
                            </Fragment>
                        );
                    }
                    if (val.method != null) {
                        if (val.method.exact != null) {
                            stringMatcherValue = val.method.exact;
                            stringMatcherType = 'Exact';
                        }
                        if (val.method.prefix != null) {
                            stringMatcherValue = val.method.prefix;
                            stringMatcherType = 'Prefix';
                        }
                        if (val.method.regex != null) {
                            stringMatcherValue = val.method.regex;
                            stringMatcherType = 'Regex';
                        }
                        fragments.push(
                            <Fragment key={value.name}>
                                <div className='steps__step__content-title'>
                                    {index} - Method ({stringMatcherType})
                                </div>
                                <div className='steps__step__content-value'>{stringMatcherValue}</div>
                            </Fragment>
                        );
                    }
                    return fragments;
                })}
            </Fragment>
        </div>
    );
};

const WidgetItemSetHeader = ({values}: {values: GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1HeaderRoutingMatch[]}) => {
    if (!values) return null;
    return (
        <div>
            {values.map((record) => {
                if (!record.headerName) return null;
                if (!record.headerValue) return null;

                let headerValue = '';
                let headerValueType = '';
                if (record.headerValue.regex) {
                    headerValue = record.headerValue.regex;
                    headerValueType = 'Regex';
                }
                if (record.headerValue.prefix) {
                    headerValue = record.headerValue.prefix;
                    headerValueType = 'Prefix';
                }
                if (record.headerValue.exact) {
                    headerValue = record.headerValue.exact;
                    headerValueType = 'Exact';
                }
                return (
                    <Fragment key={record.headerName}>
                        <div className='steps__step__content-title'>Name</div>
                        <div className='steps__step__content-value'>{record.headerName}</div>
                        <div className='steps__step__content-title'>{headerValueType}</div>
                        <div className='steps__step__content-value'>{headerValue}</div>
                    </Fragment>
                );
            })}
        </div>
    );
};
