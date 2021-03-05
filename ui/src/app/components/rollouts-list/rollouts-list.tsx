import {faCheck, faClock, faDove, faHistory, faPalette, faPlayCircle, faSync, faTimes} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';
import {Link} from 'react-router-dom';
import {Rollout} from '../../../models/rollout/rollout';
import {useWatchRollouts} from '../../shared/services/rollout';
import {formatTimestamp, latestCondition} from '../../shared/utils/utils';
import {ActionButton} from '../action-button/action-button';
import {InfoItemRow} from '../info-item/info-item';
import {conditionIcon} from '../status-icon/status-icon';
import './rollouts-list.scss';

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
        <Link className='rollouts-list__widget' to={`/rollout/${rollout.metadata?.name}`}>
            <WidgetHeader rollout={rollout} />
            <div className='rollouts-list__widget__body'>
                <InfoItemRow label={'Strategy'} content={strategy} icon={<FontAwesomeIcon icon={strategy === 'BlueGreen' ? faPalette : faDove} />} />
                <InfoItemRow label={'Generation'} content={rollout.status?.observedGeneration} icon={<FontAwesomeIcon icon={faHistory} />} />
                <InfoItemRow label={'Restarted At'} content={formatTimestamp(rollout.status?.restartedAt as string) || 'Never'} icon={<FontAwesomeIcon icon={faClock} />} />
            </div>
            <div className='rollouts-list__widget__pods'>
                <Pods />
            </div>
            <div className='rollouts-list__widget__actions'>
                <ActionButton label={'RESTART'} action={() => null} icon={<FontAwesomeIcon icon={faSync} />} />
                <ActionButton label={'RESUME'} action={() => null} icon={<FontAwesomeIcon icon={faPlayCircle} />} />
            </div>
        </Link>
    );
};

const Pods = () => {
    const pods = Array(3);
    pods.fill({status: true});
    pods.push({status: false});
    pods.push({status: false});
    return (
        <div className='pods'>
            {pods.map((pod, i) => (
                <Pod status={pod.status} key={i} />
            ))}
        </div>
    );
};

const Pod = (props: {status: boolean}) => (
    <div className={`pod pod--${props.status ? 'available' : 'errored'}`}>
        <FontAwesomeIcon icon={props.status ? faCheck : faTimes} />
    </div>
);

const WidgetHeader = (props: {rollout: Rollout}) => {
    const {rollout} = props;
    return (
        <header>
            {rollout.metadata?.name}
            <span style={{marginLeft: 'auto'}}>{conditionIcon(latestCondition(rollout.status?.conditions || []))}</span>
        </header>
    );
};
