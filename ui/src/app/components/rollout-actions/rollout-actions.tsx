import * as React from 'react';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {NamespaceContext, RolloutAPIContext} from '../../shared/context/api';
import {formatTimestamp} from '../../shared/utils/utils';
import {ActionButton, ActionButtonProps} from 'argo-ui/v2';
import {RolloutStatus} from '../status-icon/status-icon';

export enum RolloutAction {
    Restart = 'Restart',
    Retry = 'Retry',
    Abort = 'Abort',
    Promote = 'Promote',
    PromoteFull = 'PromoteFull',
}

export const RolloutActionButton = (props: {action: RolloutAction; rollout: RolloutInfo; callback?: Function; indicateLoading: boolean; disabled?: boolean}) => {
    const api = React.useContext(RolloutAPIContext);
    const namespaceCtx = React.useContext(NamespaceContext);

    const restartedAt = formatTimestamp(props.rollout.restartedAt || '');
    const isDeploying = props.rollout.status === RolloutStatus.Progressing || props.rollout.status === RolloutStatus.Paused

    const actionMap = new Map<RolloutAction, ActionButtonProps & {body?: any}>([
        [
            RolloutAction.Restart,
            {
                label: 'RESTART',
                icon: 'fa-sync',
                action: api.rolloutServiceRestartRollout,
                tooltip: restartedAt === 'Never' ? 'Never restarted' : `Last restarted ${restartedAt}`,
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.Retry,
            {
                label: 'RETRY',
                icon: 'fa-redo-alt',
                action: api.rolloutServiceRetryRollout,
                disabled: props.rollout.status !== RolloutStatus.Degraded,
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.Abort,
            {
                label: 'ABORT',
                icon: 'fa-exclamation-circle',
                action: api.rolloutServiceAbortRollout,
                disabled: !isDeploying,
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.Promote,
            {
                label: 'PROMOTE',
                icon: 'fa-chevron-circle-up',
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
                icon: 'fa-arrow-circle-up',
                action: api.rolloutServicePromoteRollout,
                body: {full: true},
                disabled: !isDeploying,
                shouldConfirm: true,
            },
        ],
    ]);

    const ap = actionMap.get(props.action);

    return (
        <ActionButton
            {...ap}
            action={() => {
                ap.action(ap.body || {}, namespaceCtx.namespace, props.rollout.objectMeta?.name || '');
                if (props.callback) {
                    props.callback();
                }
            }}
            indicateLoading={props.indicateLoading}
        />
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
