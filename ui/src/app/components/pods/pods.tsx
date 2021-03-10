import {faQuestionCircle} from '@fortawesome/free-regular-svg-icons';
import {faCheck, faCircleNotch, faClipboard, faExclamationTriangle, faTimes} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';
import {GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1ReplicaSetInfo} from '../../../models/rollout/generated';
import {Pod} from '../../../models/rollout/rollout';
import {IconForTag, ParseTagsFromReplicaSet} from '../../shared/utils/utils';
import {InfoItem} from '../info-item/info-item';
import {Menu} from '../menu/menu';
import {ThemeDiv} from '../theme-div/theme-div';
import {Tooltip} from '../tooltip/tooltip';

import './pods.scss';

export const PodIcon = (props: {status: string}) => {
    const {status} = props;
    let icon, className;
    let spin = false;
    if (status.startsWith('Init:')) {
        icon = faCircleNotch;
        spin = true;
    }
    if (status.startsWith('Signal:') || status.startsWith('ExitCode:')) {
        icon = faTimes;
    }
    if (status.endsWith('Error') || status.startsWith('Err')) {
        icon = faExclamationTriangle;
    }

    switch (status) {
        case 'Pending':
        case 'Terminating':
        case 'ContainerCreating':
            icon = faCircleNotch;
            className = 'pending';
            spin = true;
            break;
        case 'Running':
        case 'Completed':
            icon = faCheck;
            className = 'success';
            break;
        case 'Failed':
        case 'InvalidImageName':
        case 'CrashLoopBackOff':
            className = 'failure';
            icon = faTimes;
            break;
        case 'ImagePullBackOff':
        case 'RegistryUnavailable':
            className = 'warning';
            icon = faExclamationTriangle;
            break;
        default:
            className = 'unknown';
            icon = faQuestionCircle;
    }

    return (
        <ThemeDiv className={`pod-icon pod-icon--${className}`}>
            <FontAwesomeIcon icon={icon} spin={spin} />
        </ThemeDiv>
    );
};

export const ReplicaSet = (props: {rs: GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1ReplicaSetInfo}) => {
    const rsName = props.rs.objectMeta.name;
    const tags = ParseTagsFromReplicaSet(props.rs);
    return (
        <ThemeDiv className='pods'>
            {rsName && (
                <ThemeDiv className='pods__header'>
                    {rsName}
                    <div className='pods__header__tags'>
                        {tags.map((t) => (
                            <InfoItem key={t} icon={IconForTag(t)} content={t} />
                        ))}
                    </div>
                </ThemeDiv>
            )}
            <ThemeDiv className='pods__container'>
                {(props.rs.pods || []).map((pod, i) => (
                    <PodWidget key={pod.objectMeta.uid} pod={pod} />
                ))}
            </ThemeDiv>
        </ThemeDiv>
    );
};

export const PodWidget = (props: {pod: Pod}) => (
    <Menu items={[{label: 'Copy Name', action: () => navigator.clipboard.writeText(props.pod.objectMeta?.name), icon: faClipboard}]}>
        <Tooltip content={props.pod.objectMeta?.name}>
            <PodIcon status={props.pod.status} />
        </Tooltip>
    </Menu>
);
