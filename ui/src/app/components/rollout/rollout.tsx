import * as React from 'react';
import {useParams} from 'react-router-dom';
import {Helmet} from 'react-helmet';

import './rollout.scss';
import {RolloutActions} from '../rollout-actions/rollout-actions';
import {RolloutStatus, StatusIcon} from '../status-icon/status-icon';
import {ThemeDiv} from '../theme-div/theme-div';
import {useWatchRollout} from '../../shared/services/rollout';
import {InfoItemProps, InfoItemRow} from '../info-item/info-item';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {faBalanceScale, faBalanceScaleRight, faCheck, faClock, faDove, faHistory, faPalette, faShoePrints} from '@fortawesome/free-solid-svg-icons';
import {ReplicaSet} from '../pods/pods';
import {formatTimestamp, IconForTag, ImageTag} from '../../shared/utils/utils';
import {RolloutAPIContext} from '../../shared/context/api';
import {FormResetFactory, Input, useInput} from '../input/input';
import {ActionButton} from '../action-button/action-button';
import {WaitFor} from '../wait-for/wait-for';
import {
    GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1AnalysisRunInfo,
    GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1ExperimentInfo,
    GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1ReplicaSetInfo,
} from '../../../models/rollout/generated';
interface ImageInfo {
    image: string;
    tags: ImageTag[];
}

enum Strategy {
    Canary = 'Canary',
    BlueGreen = 'BlueGreen',
}

const parseImages = (r: RolloutInfo): ImageInfo[] => {
    const images: {[key: string]: ImageInfo} = {};
    (r.replicaSets || []).forEach((rs) => {
        (rs.images || []).forEach((img) => {
            const newImage: ImageInfo = {
                image: img,
                tags: [],
            };
            if (rs.canary) {
                newImage.tags.push(ImageTag.Canary);
            }
            if (rs.stable) {
                newImage.tags.push(ImageTag.Stable);
            }
            if (rs.active) {
                newImage.tags.push(ImageTag.Active);
            }
            if (rs.preview) {
                newImage.tags.push(ImageTag.Preview);
            }
            if (images[img]) {
                images[img].tags = [...newImage.tags, ...images[img].tags];
            } else {
                images[img] = newImage;
            }
        });
    });
    return Object.values(images);
};

export const Rollout = () => {
    const {name} = useParams<{name: string}>();

    const [rollout, loading] = useWatchRollout(name, true);
    const api = React.useContext(RolloutAPIContext);

    ProcessRevisions(rollout);

    return (
        <div className='rollout'>
            <Helmet>
                <title>{name} / Argo Rollouts</title>
            </Helmet>
            <ThemeDiv className='rollout__toolbar'>
                <RolloutActions name={name} />
            </ThemeDiv>

            <ThemeDiv className='rollout__body'>
                <WaitFor loading={loading}>
                    <ThemeDiv className='rollout__header'>
                        {name} <StatusIcon status={rollout.status as RolloutStatus} />
                    </ThemeDiv>
                    <ThemeDiv className='rollout__info'>
                        <div className='rollout__info__title'>Summary</div>

                        <InfoItemRow content={{content: rollout.strategy, icon: iconForStrategy(rollout.strategy as Strategy)}} label='Strategy' />
                        <InfoItemRow content={{content: rollout.generation, icon: faHistory}} label='Generation' />
                        <InfoItemRow content={{content: formatTimestamp(rollout.restartedAt), icon: faClock}} label='Restarted At' />
                        <ThemeDiv className='rollout__info__section'>
                            {rollout.strategy === Strategy.Canary && (
                                <React.Fragment>
                                    <InfoItemRow content={{content: rollout.step, icon: faShoePrints}} label='Step' />
                                    <InfoItemRow content={{content: rollout.setWeight, icon: faBalanceScaleRight}} label='Set Weight' />
                                    <InfoItemRow content={{content: rollout.actualWeight, icon: faBalanceScale}} label='Actual Weight' />{' '}
                                </React.Fragment>
                            )}
                        </ThemeDiv>

                        <ThemeDiv className='rollout__info__section'>
                            <h3>IMAGES</h3>
                            <ImageItems images={parseImages(rollout)} />
                        </ThemeDiv>

                        <h3 style={{marginBottom: '1em'}}>SET IMAGE</h3>
                        <SetImageForm setImage={(container, image, tag) => api.setRolloutImage(name, container, image, tag)} />
                    </ThemeDiv>
                    {rollout.replicaSets && rollout.replicaSets.length > 0 && (
                        <ThemeDiv className='rollout__info'>
                            <div className='rollout__info__title'>Replica Sets</div>
                            {(rollout.replicaSets || []).map((rs) => (
                                <div key={rs.objectMeta.uid}>{(rs.pods || []).length > 0 && <ReplicaSet rs={rs} />}</div>
                            ))}
                        </ThemeDiv>
                    )}
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

const SetImageForm = (props: {setImage: (container: string, image: string, tag: string) => void}) => {
    const [container, setContainer, containerInput] = useInput('');
    const [image, setImage, imageInput] = useInput('');
    const [tag, setTag, tagInput] = useInput('');

    const isDisabled = !(container !== '' && image !== '' && tag !== '');

    const resetAll = FormResetFactory([setContainer, setImage, setTag]);

    return (
        <div>
            <div style={{display: 'flex', alignItems: 'center', marginBottom: '1em'}}>
                <Input {...containerInput} placeholder='Container' />
                <div style={{width: '20px', textAlign: 'center', flexShrink: 0}}>=</div>
                <Input {...imageInput} placeholder='Image' />
                <div style={{width: '20px', textAlign: 'center', flexShrink: 0}}>:</div>
                <Input {...tagInput} placeholder='Tag' />
                <span style={{marginLeft: '7px'}}>
                    <ActionButton
                        label='SET'
                        action={() => {
                            props.setImage(container, image, tag);
                            resetAll();
                        }}
                        indicateLoading
                        disabled={isDisabled}
                        icon={faCheck}
                    />
                </span>
            </div>
            {!isDisabled && (
                <div>
                    You are setting: <b>{`${container}=${image}:${tag}`}</b>
                </div>
            )}
        </div>
    );
};

const ImageItems = (props: {images: ImageInfo[]}) => {
    return (
        <div>
            {props.images.map((img) => {
                let imageItems = img.tags.map((t) => {
                    return {content: t, icon: IconForTag(t)} as InfoItemProps;
                });
                if (imageItems.length === 0) {
                    imageItems = [{icon: IconForTag()}];
                }
                return <InfoItemRow key={img.image} label={img.image} content={imageItems} />;
            })}
        </div>
    );
};

interface Revision {
    number: number;
    replicaSets: GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1ReplicaSetInfo[];
    experiments: GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1ExperimentInfo[];
    analysisRuns: GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1AnalysisRunInfo[];
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
        map[rs.revision].replicaSets.push(rs);
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
