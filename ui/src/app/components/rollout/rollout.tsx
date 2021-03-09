import * as React from 'react';
import {useParams} from 'react-router-dom';
import {Helmet} from 'react-helmet';

import './rollout.scss';
import {RolloutActions} from '../rollout-actions/rollout-actions';
import {RolloutStatus, StatusIcon} from '../status-icon/status-icon';
import {ThemeDiv} from '../theme-div/theme-div';
import {useWatchRollout} from '../../shared/services/rollout';
import {InfoItemRow} from '../info-item/info-item';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {faBalanceScale, faBalanceScaleRight, faDove, faPalette, faRunning, faSearch, faShoePrints, faThumbsUp} from '@fortawesome/free-solid-svg-icons';

interface ImageInfo {
    image: string;
    tags: ImageTag[];
}

enum ImageTag {
    Canary = 'canary',
    Stable = 'stable',
    Active = 'active',
    Preview = 'preview',
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

    const rollout = useWatchRollout(name, true);

    return (
        <div className='rollout'>
            <Helmet>
                <title>{name} / Argo Rollouts</title>
            </Helmet>
            <ThemeDiv className='rollout__toolbar'>
                <RolloutActions />
            </ThemeDiv>
            <ThemeDiv className='rollout__body'>
                <ThemeDiv className='rollout__header'>
                    {name} <StatusIcon status={rollout.status as RolloutStatus} />
                </ThemeDiv>
                <ThemeDiv className='rollout__info'>
                    <InfoItemRow content={{content: rollout.strategy, icon: iconForStrategy(rollout.strategy as Strategy)}} label='Strategy' />

                    {rollout.strategy === Strategy.Canary && (
                        <React.Fragment>
                            <InfoItemRow content={{content: rollout.step, icon: faShoePrints}} label='Step' />
                            <InfoItemRow content={{content: rollout.setWeight, icon: faBalanceScaleRight}} label='Set Weight' />
                            <InfoItemRow content={{content: rollout.actualWeight, icon: faBalanceScale}} label='Actual Weight' />{' '}
                        </React.Fragment>
                    )}

                    <h3>IMAGES</h3>
                    <ImageItems images={parseImages(rollout)} />
                </ThemeDiv>
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

const iconForTag = (t: ImageTag) => {
    switch (t) {
        case ImageTag.Canary:
            return faDove;
        case ImageTag.Stable:
            return faThumbsUp;
        case ImageTag.Preview:
            return faSearch;
        case ImageTag.Active:
            return faRunning;
    }
};

const ImageItems = (props: {images: ImageInfo[]}) => {
    return (
        <div>
            {props.images.map((img) => {
                const imageItems = img.tags.map((t) => {
                    return {content: t, icon: iconForTag(t)};
                });
                return <InfoItemRow label={img.image} content={imageItems} />;
            })}
        </div>
    );
};
