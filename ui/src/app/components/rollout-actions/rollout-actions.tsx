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

// export const RolloutActionButton = (props: {action: RolloutAction; rollout: RolloutInfo; callback?: Function; indicateLoading: boolean; disabled?: boolean}) => {
export const RolloutActionButton = React.memo(
    ({action, rollout, callback, indicateLoading, disabled}: {action: RolloutAction; rollout: RolloutInfo; callback?: Function; indicateLoading: boolean; disabled?: boolean}) => {
        const [loading, setLoading] = React.useState(false);
        const api = React.useContext(RolloutAPIContext);
        const namespaceCtx = React.useContext(NamespaceContext);

        const restartedAt = formatTimestamp(rollout.restartedAt || '');
        const isDeploying = rollout.status === RolloutStatus.Progressing || rollout.status === RolloutStatus.Paused;

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
                    disabled: rollout.status !== RolloutStatus.Degraded,
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

        const ap = actionMap.get(action);

        // const [loading, setLoading] = React.useState(false);

        return (
            <ConfirmButton
                style={{margin: '0 5px'}}
                skipconfirm={ap.shouldConfirm ? undefined : true}
                type='primary'
                onClick={async (e) => {
                    setLoading(true);
                    await ap.action(ap.body || {}, namespaceCtx.namespace, rollout.objectMeta?.name || '');
                    if (callback) {
                        await callback();
                    }
                    setLoading(false);
                }}
                disabled={ap.disabled}
                loading={loading}
                tooltip={ap.tooltip}
                icon={<FontAwesomeIcon icon={ap.icon} style={{marginRight: '5px'}} />}
            >
                {action}
            </ConfirmButton>
        );
    },
);

export const RolloutActions = (props: {rollout: RolloutInfo}) => (
    <div style={{display: 'flex'}}>
        {Object.values(RolloutAction).map((action) => (
            <RolloutActionButton key={action} action={action as RolloutAction} rollout={props.rollout} indicateLoading />
        ))}
    </div>
);

export default RolloutActions;
