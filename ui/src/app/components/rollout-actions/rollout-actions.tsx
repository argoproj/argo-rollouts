import {faArrowCircleUp, faChevronCircleUp, faExclamationCircle, faRedoAlt, faSync} from '@fortawesome/free-solid-svg-icons';
import * as React from 'react';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {NamespaceContext, RolloutAPIContext} from '../../shared/context/api';
import {formatTimestamp} from '../../shared/utils/utils';
import {ActionButton, ActionButtonProps} from 'argo-ux';
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
    const namespace = React.useContext(NamespaceContext);

    const restartedAt = formatTimestamp(props.rollout.restartedAt || '');

    const actionMap = new Map<RolloutAction, ActionButtonProps & {body?: any}>([
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
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.Abort,
            {
                label: 'ABORT',
                icon: faExclamationCircle,
                action: api.rolloutServiceAbortRollout,
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
                disabled: props.rollout.status !== RolloutStatus.Paused,
                shouldConfirm: true,
            },
        ],
        [
            RolloutAction.PromoteFull,
            {
                label: 'PROMOTE-FULL',
                icon: faArrowCircleUp,
                body: {full: true},
                action: api.rolloutServicePromoteRollout,
                disabled: props.rollout.status !== RolloutStatus.Paused,
                shouldConfirm: true,
            },
        ],
    ]);

    const ap = actionMap.get(props.action);

    return (
        <ActionButton
            {...ap}
            action={() => {
                ap.action(ap.body || {}, namespace, props.rollout.objectMeta?.name || '');
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
