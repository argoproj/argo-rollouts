import {EffectDiv, InfoItemKind, InfoItemRow, Spinner, ThemeDiv, WaitFor} from 'argo-ui/v2';
import * as React from 'react';
import {Helmet} from 'react-helmet';
import {Key, KeybindingContext} from 'react-keyhooks';
import {useHistory, useParams} from 'react-router-dom';
import {
    GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1CanaryStep,
    GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1RolloutExperimentTemplate,
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
        <React.Fragment>
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
                                <InfoItemRow items={{content: rollout.step, icon: 'fa-shoe-prints'}} label='Step' />
                                <InfoItemRow items={{content: rollout.setWeight, icon: 'fa-balance-scale-right'}} label='Set Weight' />
                                <InfoItemRow items={{content: rollout.actualWeight, icon: 'fa-balance-scale'}} label='Actual Weight' />{' '}
                            </React.Fragment>
                        )}
                    </ThemeDiv>
                </ThemeDiv>
                <ThemeDiv className='info rollout__info'>
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
                                    rollback={interactive ? (r) => interactive.api.rolloutServiceUndoRollout({}, interactive.namespace, rollout.objectMeta.name, `${r}`) : null}
                                    current={i === 0}
                                />
                            ))}
                        </div>
                    </ThemeDiv>
                )}
                {(rollout?.strategy || '').toLowerCase() === 'canary' && rollout.steps && rollout.steps.length > 0 && (
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
        </React.Fragment>
    );
};

export const Rollout = () => {
    const {name} = useParams<{name: string}>();

    const [rollout, loading] = useWatchRollout(name, true);
    const api = React.useContext(RolloutAPIContext);
    const namespaceCtx = React.useContext(NamespaceContext);

    const {useKeybinding} = React.useContext(KeybindingContext);
    const editState = React.useState(false);
    const history = useHistory();

    useKeybinding(Key.L, () => {
        if (editState[0]) {
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
                    <RolloutWidget rollout={rollout} interactive={{api, editState, namespace: namespaceCtx.namespace}} />
                </WaitFor>
            </ThemeDiv>
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
    const map: {[key: number]: Revision} = {};

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

    return (
        <React.Fragment>
            <EffectDiv className={`steps__step ${props.complete ? 'steps__step--complete' : ''} ${props.current ? 'steps__step--current' : ''}`}>
                <div className={`steps__step-title ${props.step.experiment ? 'steps__step-title--experiment' : ''}`}>
                    <i className={`fa ${icon}`} /> {content}
                    {unit}
                </div>
                {props.step.experiment?.templates && (
                    <div className='steps__step__content'>
                        {props.step.experiment?.templates.map((template) => {
                            return <ExperimentWidget key={template.name} template={template} opened={openedTemplate === template.name} onToggle={setOpenedTemplate} />;
                        })}
                    </div>
                )}
            </EffectDiv>
            {!props.last && <ThemeDiv className='steps__connector' />}
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
    const icon = opened ? 'fa-chevron-circle-up' : 'fa-chevron-circle-down';
    return (
        <EffectDiv className='steps__step__content-body'>
            <ThemeDiv className={`steps__step__content-header ${opened ? 'steps__step__content-value' : ''}`}>
                {template.name}
                <ThemeDiv onClick={() => onToggle(opened ? '' : template.name)}>
                    <i className={`fa ${icon}`} />
                </ThemeDiv>
            </ThemeDiv>
            {opened && (
                <EffectDiv>
                    <div className='steps__step__content-title'>SPECREF</div>
                    <div className='steps__step__content-value'>{template.specRef}</div>
                    {template.weight && (
                        <Fragment>
                            <div className='steps__step__content-title'>WEIGHT</div> <div className='steps__step__content-value'>{template.weight}</div>
                        </Fragment>
                    )}
                </EffectDiv>
            )}
        </EffectDiv>
    );
};
