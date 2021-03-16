import {IconDefinition} from '@fortawesome/free-regular-svg-icons';
import {faArrowCircleUp, faExclamationCircle, faRedoAlt, faSync} from '@fortawesome/free-solid-svg-icons';
import * as React from 'react';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {RolloutAPIContext} from '../../shared/context/api';
import {ActionButton} from '../action-button/action-button';
import {RolloutStatus} from '../status-icon/status-icon';

export enum RolloutAction {
    Restart = 'Restart',
    Retry = 'Retry',
    Abort = 'Abort',
    PromoteFull = 'PromoteFull',
}

interface ActionProps {
    label: string;
    icon: IconDefinition;
    action: Function;
    disabled?: boolean;
}

export const RolloutActionButton = (props: {action: RolloutAction; rollout: RolloutInfo; callback?: Function; indicateLoading: boolean; disabled?: boolean}) => {
    const api = React.useContext(RolloutAPIContext);

    const actionMap = new Map<RolloutAction, ActionProps>([
        [
            RolloutAction.Restart,
            {
                label: 'RESTART',
                icon: faSync,
                action: api.restartRollout,
            },
        ],
        [
            RolloutAction.Retry,
            {
                label: 'RETRY',
                icon: faRedoAlt,
                action: (): any => null,
            },
        ],
        [
            RolloutAction.Abort,
            {
                label: 'ABORT',
                icon: faExclamationCircle,
                action: api.abortRollout,
            },
        ],
        [
            RolloutAction.PromoteFull,
            {
                label: 'PROMOTE-FULL',
                icon: faArrowCircleUp,
                action: api.promoteRollout,
                disabled: props.rollout.status !== RolloutStatus.Paused,
            },
        ],
    ]);

    const ap = actionMap.get(props.action);

    return (
        <ActionButton
            {...ap}
            action={() => {
                ap.action(props.rollout.objectMeta?.name || '');
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
