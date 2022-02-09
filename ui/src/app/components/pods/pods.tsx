import {Menu, ThemeDiv, Tooltip, WaitFor, InfoItem} from 'argo-ui/v2';
import * as React from 'react';
import * as moment from 'moment';
import {Duration, Ticker} from 'argo-ui';
import {RolloutReplicaSetInfo} from '../../../models/rollout/generated';
import {Pod} from '../../../models/rollout/rollout';
import {ReplicaSetStatus, ReplicaSetStatusIcon} from '../status-icon/status-icon';
import './pods.scss';

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

export const PodIcon = (props: {status: string}) => {
    const {status} = props;
    let icon;
    let spin = false;
    if (status.startsWith('Init:')) {
        icon = 'fa-circle-notch';
        spin = true;
    }
    if (status.startsWith('Signal:') || status.startsWith('ExitCode:')) {
        icon = 'fa-times';
    }
    if (status.endsWith('Error') || status.startsWith('Err')) {
        icon = 'fa-exclamation-triangle';
    }

    const className = ParsePodStatus(status);

    switch (className) {
        case PodStatus.Pending:
            icon = 'fa-circle-notch';
            spin = true;
            break;
        case PodStatus.Success:
            icon = 'fa-check';
            break;
        case PodStatus.Failed:
            icon = 'fa-times';
            break;
        case PodStatus.Warning:
            icon = 'fa-exclamation-triangle';
            break;
        default:
            spin = false;
            icon = 'fa-question-circle';
            break;
    }

    return (
        <ThemeDiv className={`pod-icon pod-icon--${className}`}>
            <i className={`fa ${icon} ${spin ? 'fa-spin' : ''}`} />
        </ThemeDiv>
    );
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
                                    const time = moment(props.rs.scaleDownDeadline).diff(now, 'second');
                                    return time <= 0 ? null : (
                                        <Tooltip content={<span>Scaledown in <Duration durationMs={time}/></span>}>
                                            <InfoItem content={<Duration durationMs={time}/> as any} icon='fa fa-clock'></InfoItem>
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
                            <PodWidget key={pod.objectMeta?.uid} pod={pod} />
                        ))}
                    </WaitFor>
                </ThemeDiv>
            )}
        </ThemeDiv>
    );
};

export const PodWidget = (props: {pod: Pod}) => (
    <Menu items={[{label: 'Copy Name', action: () => navigator.clipboard.writeText(props.pod.objectMeta?.name), icon: 'fa-clipboard'}]}>
        <Tooltip
            content={
                <div>
                    <div>Status: {props.pod.status}</div>
                    <div>{props.pod.objectMeta?.name}</div>
                </div>
            }>
            <PodIcon status={props.pod.status} />
        </Tooltip>
    </Menu>
);
