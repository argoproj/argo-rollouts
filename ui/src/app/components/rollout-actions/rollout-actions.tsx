import * as React from 'react';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {NamespaceContext, RolloutAPIContext} from '../../shared/context/api';
import {formatTimestamp} from '../../shared/utils/utils';
import {RolloutStatus} from '../status-icon/status-icon';
import {ConfirmButton} from '../confirm-button/confirm-button';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {faArrowCircleUp, faChevronCircleUp, faExclamationCircle, faRedoAlt, faSync} from '@fortawesome/free-solid-svg-icons';
import {IconProp} from '@fortawesome/fontawesome-svg-core';

export enum RolloutAction {
    Restart = 'Restart',
    Retry = 'Retry',
    Abort = 'Abort',
    Promote = 'Promote',
    PromoteFull = 'PromoteFull',
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

    const restartedAt = formatTimestamp(props.rollout.restartedAt || '');
    const isDeploying = props.rollout.status === RolloutStatus.Progressing || props.rollout.status === RolloutStatus.Paused;

    const actionMap = new Map<RolloutAction, ActionData & {body?: any}>([
        [
            RolloutAction.Restart,
            {
                label: 'RESTART',
                icon: faSync,
                action: api.rolloutServiceRestartRollout,
                tooltip: restartedAt === 'Never' ? 'Never restarted' : `Last restarted ${restartedAt}`,
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.Retry,
            {
                label: 'RETRY',
                icon: faRedoAlt,
                action: api.rolloutServiceRetryRollout,
                disabled: props.rollout.status !== RolloutStatus.Degraded,
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.Abort,
            {
                label: 'ABORT',
                icon: faExclamationCircle,
                action: api.rolloutServiceAbortRollout,
                disabled: !isDeploying,
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.Promote,
            {
                label: 'PROMOTE',
                icon: faChevronCircleUp,
                action: api.rolloutServicePromoteRollout,
                body: {full: false},
                disabled: !isDeploying,
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.PromoteFull,
            {
                label: 'PROMOTE-FULL',
                icon: faArrowCircleUp,
                action: api.rolloutServicePromoteRollout,
                body: {full: true},
                disabled: !isDeploying,
                shouldConfirm: true,
            },
        ],
    ]);

    const ap = actionMap.get(props.action);

    const [loading, setLoading] = React.useState(false);

    return (
        <ConfirmButton
            style={{margin: '0 5px'}}
            skipconfirm={!ap.shouldConfirm}
            type='primary'
            onClick={async (e) => {
                setLoading(true);
                await ap.action(ap.body || {}, namespaceCtx.namespace, props.rollout.objectMeta?.name || '');
                if (props.callback) {
                    await props.callback();
                }
                setLoading(false);
            }}
            disabled={ap.disabled}
            loading={loading}
            tooltip={ap.tooltip}
            icon={<FontAwesomeIcon icon={ap.icon} style={{marginRight: '5px'}} />}>
            {props.action}
        </ConfirmButton>
    );
};

export const RolloutActions = (props: {rollout: RolloutInfo}) => (
    <div style={{display: 'flex'}}>
        {Object.values(RolloutAction).map((action) => (
            <RolloutActionButton key={action} action={action as RolloutAction} rollout={props.rollout} indicateLoading />
        ))}
    </div>
);

export default RolloutActions;
