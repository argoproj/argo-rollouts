import * as React from 'react';
import {Rollout} from '../../../models/rollout/rollout';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {faDove, faPalette} from '@fortawesome/free-solid-svg-icons';

import './rollouts-list.scss';
import {useWatchRollouts} from '../../shared/services/rollout';

export const RolloutsList = () => {
    const rollouts = useWatchRollouts();
    return (
        <div className='rollouts-list'>
            {(rollouts || []).map((rollout) => (
                <RolloutWidget key={rollout.metadata?.uid} rollout={rollout} />
            ))}
        </div>
    );
};

export const RolloutWidget = (props: {rollout: Rollout}) => {
    const {rollout} = props;
    const strategy = rollout.spec?.strategy?.blueGreen ? 'BlueGreen' : 'Canary';
    return (
        <div className='rollouts-list__widget'>
            <header>{rollout.metadata?.name}</header>
            <div className='rollouts-list__widget__body'>
                <WidgetItem label={'Namespace'} content={rollout.metadata?.namespace} />
                <WidgetItem label={'Strategy'} content={strategy} icon={<FontAwesomeIcon icon={strategy === 'BlueGreen' ? faPalette : faDove} />} />
            </div>
        </div>
    );
};

const WidgetItem = (props: {label: string; content: string; icon?: JSX.Element}) => {
    const {label, content, icon} = props;
    return (
        <div className='rollouts-list__widget__item'>
            <label>{label}</label>
            <div className='rollouts-list__widget__item__content'>
                {icon && <span style={{marginRight: '5px'}}>{icon}</span>}
                {content}
            </div>
        </div>
    );
};
