import {faPlayCircle, IconDefinition} from '@fortawesome/free-regular-svg-icons';
import {faArrowCircleUp, faExclamationCircle, faRedoAlt, faSync} from '@fortawesome/free-solid-svg-icons';
import * as React from 'react';
import {RolloutAPIContext} from '../../shared/context/api';
import {ActionButton} from '../action-button/action-button';

export enum RolloutAction {
    Restart = 'Restart',
    Resume = 'Resume',
    Retry = 'Retry',
    Abort = 'Abort',
    PromoteFull = 'PromoteFull',
}

interface ActionProps {
    label: string;
    icon: IconDefinition;
    action: Function;
}

export const RolloutActionButton = (props: {action: RolloutAction; name: string; callback?: Function; indicateLoading: boolean}) => {
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
        [RolloutAction.Resume, {label: 'RESUME', icon: faPlayCircle, action: (): any => null}],
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
                action: (): any => null,
            },
        ],
        [
            RolloutAction.PromoteFull,
            {
                label: 'PROMOTE-FULL',
                icon: faArrowCircleUp,
                action: api.promoteRollout,
            },
        ],
    ]);

    const ap = actionMap.get(props.action);

    return (
        <ActionButton
            {...ap}
            action={() => {
                ap.action(props.name);
                if (props.callback) {
                    props.callback();
                }
            }}
            indicateLoading={props.indicateLoading}
        />
    );
};

export const RolloutActions = (props: {name: string}) => (
    <div style={{display: 'flex'}}>
        {Object.values(RolloutAction).map((action) => (
            <RolloutActionButton key={action} action={action as RolloutAction} name={props.name} indicateLoading />
        ))}
    </div>
);
