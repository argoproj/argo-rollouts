import {ThemeDiv, WaitFor, InfoItem} from 'argo-ui/v2';
import * as React from 'react';
import * as moment from 'moment';
import {Duration, Ticker} from 'argo-ui';
import {RolloutReplicaSetInfo} from '../../../models/rollout/generated';
import {ReplicaSetStatus, ReplicaSetStatusIcon} from '../status-icon/status-icon';
import './pods.scss';
import {Dropdown, MenuProps, Tooltip} from 'antd';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {IconDefinition, faCheck, faCircleNotch, faClipboard, faExclamationTriangle, faQuestionCircle, faTimes} from '@fortawesome/free-solid-svg-icons';

export enum PodStatus {
    Pending = 'pending',
    Success = 'success',
    Failed = 'failure',
    Warning = 'warning',
    Unknown = 'unknown',
}

export const ParsePodStatus = (status: string): PodStatus => {
    switch (status) {
        case 'Pending':
        case 'Terminating':
        case 'ContainerCreating':
            return PodStatus.Pending;
        case 'Running':
        case 'Completed':
        case 'Successful':
            return PodStatus.Success;
        case 'Failed':
        case 'InvalidImageName':
        case 'CrashLoopBackOff':
            return PodStatus.Failed;
        case 'ImagePullBackOff':
        case 'RegistryUnavailable':
            return PodStatus.Warning;
        default:
            return PodStatus.Unknown;
    }
};

export const ReplicaSets = (props: {replicaSets: RolloutReplicaSetInfo[]; showRevisions?: boolean}) => {
    const {replicaSets} = props;
    if (!replicaSets || replicaSets.length < 1) {
        return <div>No replica sets!</div>;
    }

    return (
        <div>
            {replicaSets?.map(
                (rsInfo) =>
                    rsInfo.pods &&
                    rsInfo.pods.length > 0 && (
                        <div key={rsInfo.objectMeta.uid} style={{marginBottom: '1em'}}>
                            <ReplicaSet rs={rsInfo} showRevision={props.showRevisions} />
                        </div>
                    )
            )}
        </div>
    );
};

export const ReplicaSet = (props: {rs: RolloutReplicaSetInfo; showRevision?: boolean}) => {
    const rsName = props.rs.objectMeta.name;
    return (
        <ThemeDiv className='pods'>
            {rsName && (
                <ThemeDiv className='pods__header'>
                    <span style={{marginRight: '5px'}}>{rsName}</span> <ReplicaSetStatusIcon status={props.rs.status as ReplicaSetStatus} />
                    {props.showRevision && <div style={{marginLeft: 'auto'}}>Revision {props.rs.revision}</div>}
                    {props.rs.scaleDownDeadline && (
                        <div style={{marginLeft: 'auto'}}>
                            <Ticker>
                                {(now) => {
                                    const time = moment(props.rs.scaleDownDeadline).diff(now.toDate(), 'second');
                                    return time <= 0 ? null : (
                                        <Tooltip
                                            title={
                                                <span>
                                                    Scaledown in <Duration durationMs={time} />
                                                </span>
                                            }>
                                            <InfoItem content={(<Duration durationMs={time} />) as any} icon='fa fa-clock'></InfoItem>
                                        </Tooltip>
                                    );
                                }}
                            </Ticker>
                        </div>
                    )}
                </ThemeDiv>
            )}

            {props.rs.pods && props.rs.pods.length > 0 && (
                <ThemeDiv className='pods__container'>
                    <WaitFor loading={(props.rs.pods || []).length < 1}>
                        {props.rs.pods.map((pod, i) => (
                            <PodWidget
                                key={pod.objectMeta?.uid}
                                name={pod.objectMeta?.name}
                                status={pod.status}
                                tooltip={
                                    <div>
                                        <div>Status: {pod.status}</div>
                                        <div>{pod.objectMeta?.name}</div>
                                    </div>
                                }
                            />
                        ))}
                    </WaitFor>
                </ThemeDiv>
            )}
        </ThemeDiv>
    );
};

const CopyMenu = (name: string): MenuProps['items'] => {
    return [
        {
            key: 1,
            label: (
                <div onClick={() => navigator.clipboard.writeText(name)}>
                    <FontAwesomeIcon icon={faClipboard} style={{marginRight: '5px'}} /> Copy Name
                </div>
            ),
        },
    ];
};

export const PodWidget = ({name, status, tooltip, customIcon}: {name: string; status: string; tooltip: React.ReactNode; customIcon?: IconDefinition}) => {
    let icon: IconDefinition;
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

    const className = ParsePodStatus(status);

    if (customIcon) {
        icon = customIcon;
    } else {
        switch (className) {
            case PodStatus.Pending:
                icon = faCircleNotch;
                spin = true;
                break;
            case PodStatus.Success:
                icon = faCheck;
                break;
            case PodStatus.Failed:
                icon = faTimes;
                break;
            case PodStatus.Warning:
                icon = faExclamationTriangle;
                break;
            default:
                spin = false;
                icon = faQuestionCircle;
                break;
        }
    }

    return (
        <Dropdown menu={{items: CopyMenu(name)}} trigger={['click']}>
            <Tooltip title={tooltip}>
                <div className={`pod-icon pod-icon--${className}`}>
                    <FontAwesomeIcon icon={icon} spin={spin} />
                </div>
            </Tooltip>
        </Dropdown>
    );
};
