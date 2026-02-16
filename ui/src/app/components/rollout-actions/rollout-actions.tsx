import * as React from 'react';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {NamespaceContext, RolloutAPIContext} from '../../shared/context/api';
import {formatTimestamp} from '../../shared/utils/utils';
import {RolloutStatus} from '../status-icon/status-icon';
import {ConfirmButton} from '../confirm-button/confirm-button';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {faArrowCircleUp, faChevronCircleUp, faExclamationCircle, faPause, faPlay, faRedoAlt, faSync} from '@fortawesome/free-solid-svg-icons';
import {IconProp} from '@fortawesome/fontawesome-svg-core';
import {notification} from 'antd';

export enum RolloutAction {
    Restart = 'Restart',
    Retry = 'Retry',
    Abort = 'Abort',
    Promote = 'Promote',
    PromoteFull = 'PromoteFull',
    Pause = 'Pause',
    Resume = 'Resume',
}

interface ActionData {
    label: string;
    icon: IconProp;
    action: (body: any, namespace: string, name: string) => Promise<any>;
    tooltip?: string;
    disabled?: boolean;
    shouldConfirm?: boolean;
}

export const RolloutActionButton = (props: {action: RolloutAction; rollout: RolloutInfo; callback?: Function; indicateLoading: boolean; disabled?: boolean}) => {
    const api = React.useContext(RolloutAPIContext);
    const namespaceCtx = React.useContext(NamespaceContext);

    const isPaused = props.rollout.status === RolloutStatus.Paused;
    const isProgressing = props.rollout.status === RolloutStatus.Progressing;
    const isDeploying = isProgressing || isPaused;

    const isProgrammaticallyPaused = isPaused && (props.rollout.message?.includes('Pause') || props.rollout.message?.includes('Inconclusive'));
    const isManuallyPaused = isPaused && props.rollout.message === 'manually paused';
    const canBePaused = isProgressing;

    const restartedAt = formatTimestamp(props.rollout.restartedAt || '');

    const actionMap = new Map<RolloutAction, ActionData & {body?: any}>([
        [
            RolloutAction.Restart,
            {
                label: 'RESTART',
                icon: faSync,
                action: api.rolloutServiceRestartRollout.bind(api),
                tooltip: restartedAt === 'Never' ? 'Never restarted' : `Last restarted ${restartedAt}`,
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.Retry,
            {
                label: 'RETRY',
                icon: faRedoAlt,
                action: api.rolloutServiceRetryRollout.bind(api),
                disabled: props.rollout.status !== RolloutStatus.Degraded,
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.Abort,
            {
                label: 'ABORT',
                icon: faExclamationCircle,
                action: api.rolloutServiceAbortRollout.bind(api),
                disabled: !isDeploying,
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.Promote,
            {
                label: 'PROMOTE',
                icon: faChevronCircleUp,
                action: api.rolloutServicePromoteRollout.bind(api),
                body: {full: false},
                disabled: !isProgrammaticallyPaused,
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.PromoteFull,
            {
                label: 'PROMOTE-FULL',
                icon: faArrowCircleUp,
                action: api.rolloutServicePromoteRollout.bind(api),
                body: {full: true},
                disabled: !isDeploying,
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.Pause,
            {
                label: 'PAUSE',
                icon: faPause,
                action: api.rolloutServicePauseRollout.bind(api),
                body: {paused: true},
                disabled: !canBePaused,
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.Resume,
            {
                label: 'RESUME',
                icon: faPlay,
                action: api.rolloutServicePauseRollout.bind(api),
                body: {paused: false},
                disabled: !isManuallyPaused,
                shouldConfirm: true,
            },
        ],
    ]);

    const ap = actionMap.get(props.action);

    const [loading, setLoading] = React.useState(false);

    const handleActionError = (error: any, actionName: string) => {
        console.error(`Error executing ${actionName}:`, error);
        
        let errorTitle = `Failed to ${actionName.toLowerCase()} rollout`;
        let errorContent = '';
        
        if (error?.response?.status === 403) {
            errorTitle = 'Permission Denied';
            errorContent = `You don't have permission to ${actionName.toLowerCase()} this rollout. Please check your RBAC permissions.`;
        } else if (error?.response?.data?.message) {
            errorContent = error.response.data.message;
        } else if (error?.message) {
            errorContent = error.message;
        } else {
            errorContent = 'An unexpected error occurred. Please try again.';
        }

        notification.error({
            message: errorTitle,
            description: errorContent,
            duration: 8,
            placement: 'bottomRight',
        });
    };

    return (
        <ConfirmButton
            style={{margin: '0 5px'}}
            skipconfirm={!ap.shouldConfirm}
            type='primary'
            onClick={async (e) => {
                setLoading(true);
                try {
                    await ap.action(ap.body || {}, namespaceCtx.namespace, props.rollout.objectMeta?.name || '');
                    if (props.callback) {
                        await props.callback();
                    }
                } catch (error) {
                    handleActionError(error, props.action);
                } finally {
                    setLoading(false);
                }
            }}
            disabled={ap.disabled}
            loading={loading}
            tooltip={ap.tooltip}
            icon={<FontAwesomeIcon icon={ap.icon} style={{marginRight: '5px'}} />}
        >
            {ap.label}
        </ConfirmButton>
    );
};

export const RolloutActions = (props: {rollout: RolloutInfo}) => {
    const isPaused = props.rollout.status === RolloutStatus.Paused;
    const isProgrammaticallyPaused = isPaused && (props.rollout.message?.includes('Pause') || props.rollout.message?.includes('Inconclusive'));

    return (
        <div style={{display: 'flex'}}>
            <RolloutActionButton action={RolloutAction.Restart} rollout={props.rollout} indicateLoading />
            <RolloutActionButton action={RolloutAction.Retry} rollout={props.rollout} indicateLoading />
            <RolloutActionButton action={RolloutAction.Abort} rollout={props.rollout} indicateLoading />
            <RolloutActionButton action={RolloutAction.Pause} rollout={props.rollout} indicateLoading />
            <RolloutActionButton action={RolloutAction.Resume} rollout={props.rollout} indicateLoading />
            {isProgrammaticallyPaused && <RolloutActionButton action={RolloutAction.Promote} rollout={props.rollout} indicateLoading />}
            <RolloutActionButton action={RolloutAction.PromoteFull} rollout={props.rollout} indicateLoading />
        </div>
    );
};

export default RolloutActions;
