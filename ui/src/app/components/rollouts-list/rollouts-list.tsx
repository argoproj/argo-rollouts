import {faClock, faDove, faHistory, faPalette, faPlayCircle, faSync} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';
import {Link} from 'react-router-dom';
import {RolloutServiceApi} from '../../../models/rollout/generated';
import {Pod, RolloutInfo} from '../../../models/rollout/rollout';
import {useWatchRollouts} from '../../shared/services/rollout';
import {formatTimestamp} from '../../shared/utils/utils';
import {ActionButton} from '../action-button/action-button';
import {InfoItemRow} from '../info-item/info-item';
import {PodIcon, RolloutStatus, StatusIcon} from '../status-icon/status-icon';
import {WaitFor} from '../wait-for/wait-for';
import './rollouts-list.scss';

export const RolloutsList = () => {
    const [rollouts, loading] = useWatchRollouts();
    console.log(rollouts);
    return (
        <div className='rollouts-list'>
            <WaitFor loading={loading}>
                {(rollouts || []).map((rollout) => (
                    <RolloutWidget key={rollout.objectMeta?.uid} rollout={rollout} />
                ))}
            </WaitFor>
        </div>
    );
};

export const RolloutWidget = (props: {rollout: RolloutInfo}) => {
    const {rollout} = props;
    const api = new RolloutServiceApi();
    return (
        <Link className='rollouts-list__widget' to={`/rollout/${rollout.objectMeta?.name}`}>
            <WidgetHeader rollout={rollout} />
            <div className='rollouts-list__widget__body'>
                <InfoItemRow label={'Strategy'} content={rollout.strategy} icon={<FontAwesomeIcon icon={rollout.strategy === 'BlueGreen' ? faPalette : faDove} />} />
                <InfoItemRow label={'Generation'} content={`${rollout.updated}`} icon={<FontAwesomeIcon icon={faHistory} />} />
                <InfoItemRow label={'Restarted At'} content={formatTimestamp(rollout.restartedAt as string) || 'Never'} icon={<FontAwesomeIcon icon={faClock} />} />
            </div>
            {rollout.replicaSets?.map(
                (rsInfo) =>
                    rsInfo.pods &&
                    rsInfo.pods.length > 0 && (
                        <div className='rollouts-list__widget__pods' key={rsInfo.objectMeta.uid}>
                            <Pods pods={rsInfo.pods || []} />
                        </div>
                    )
            )}
            <div className='rollouts-list__widget__actions'>
                <ActionButton label={'RESTART'} action={() => api.restartRollout(rollout.objectMeta.name)} icon={<FontAwesomeIcon icon={faSync} />} />
                <ActionButton label={'RESUME'} action={() => null} icon={<FontAwesomeIcon icon={faPlayCircle} />} />
            </div>
        </Link>
    );
};

const Pods = (props: {pods: Pod[]}) => {
    return (
        <div className='pods'>
            {props.pods.map((pod, i) => (
                <PodWidget key={pod.objectMeta.uid} status={pod.status} />
            ))}
        </div>
    );
};

const PodWidget = (props: {status: string}) => <PodIcon status={props.status} />;

const WidgetHeader = (props: {rollout: RolloutInfo}) => {
    const {rollout} = props;
    return (
        <header>
            {rollout.objectMeta?.name}
            <span style={{marginLeft: 'auto'}}>
                <StatusIcon status={rollout.status as RolloutStatus} />
            </span>
        </header>
    );
};
