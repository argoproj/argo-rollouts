import {faCircleNotch, faClock, faDove, faHistory, faPalette, faPlayCircle, faRedoAlt} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';
import {Link} from 'react-router-dom';
import {RolloutServiceApi} from '../../../models/rollout/generated';
import {Pod, RolloutInfo} from '../../../models/rollout/rollout';
import {useWatchRollout, useWatchRollouts} from '../../shared/services/rollout';
import {formatTimestamp} from '../../shared/utils/utils';
import {ActionButton} from '../action-button/action-button';
import {InfoItemRow} from '../info-item/info-item';
import {PodIcon, RolloutStatus, StatusIcon} from '../status-icon/status-icon';
import {Tooltip} from '../tooltip/tooltip';
import {WaitFor} from '../wait-for/wait-for';
import {Key, useKeyListener, useNav} from '@rbreeze/react-keypress';
import './rollouts-list.scss';
import {ThemeDiv} from '../theme-div/theme-div';
import {Actions} from '../rollout-actions/rollout-actions';

export const RolloutsList = () => {
    const [rollouts, loading] = useWatchRollouts();
    const [pos, nav, reset] = useNav(rollouts.length);

    const useKeyPress = useKeyListener();

    useKeyPress(Key.RIGHT, () => nav(1));
    useKeyPress(Key.LEFT, () => nav(-1));
    useKeyPress(Key.ESCAPE, () => {
        reset();
        return true;
    });

    return (
        <div className='rollouts-list'>
            <WaitFor loading={loading}>
                {(rollouts || []).map((rollout, i) => (
                    <RolloutWidget key={rollout.objectMeta?.uid} rollout={rollout} selected={i === pos} />
                ))}
            </WaitFor>
        </div>
    );
};

export const RolloutWidget = (props: {rollout: RolloutInfo; selected?: boolean}) => {
    const api = new RolloutServiceApi();
    const [watching, subscribe] = React.useState(false);
    let rollout = props.rollout;
    const ACTION_WATCH_TIMEOUT = 20000;
    React.useEffect(() => {
        setTimeout(() => {
            subscribe(false);
        }, ACTION_WATCH_TIMEOUT);
    }, [watching]);
    useWatchRollout(props.rollout?.objectMeta?.name, watching, ACTION_WATCH_TIMEOUT, (r: RolloutInfo) => (rollout = r));
    return (
        <ThemeDiv className={`rollouts-list__widget ${props.selected ? 'rollouts-list__widget--selected' : ''}`}>
            <Link to={`/rollout/${rollout.objectMeta?.name}`}>
                <WidgetHeader rollout={rollout} refresh={() => subscribe(true)} />
                <div className='rollouts-list__widget__body'>
                    <InfoItemRow label={'Strategy'} content={{content: rollout.strategy, icon: rollout.strategy === 'BlueGreen' ? faPalette : faDove}} />
                    <InfoItemRow label={'Generation'} content={{content: `${rollout.updated}`, icon: faHistory}} />
                    <InfoItemRow label={'Restarted At'} content={{content: formatTimestamp(rollout.restartedAt as string) || 'Never', icon: faClock}} />
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
                    <ActionButton
                        label={'RESTART'}
                        indicateLoading
                        action={() => Actions.Restart.action(api, rollout.objectMeta?.name || '', () => subscribe(true))}
                        icon={Actions.Restart.icon}
                    />
                    <ActionButton label={'RESUME'} action={(): any => null} icon={faPlayCircle} />
                </div>
            </Link>
        </ThemeDiv>
    );
};

const Pods = (props: {pods: Pod[]}) => {
    return (
        <ThemeDiv className='pods'>
            {props.pods.map((pod, i) => (
                <PodWidget key={pod.objectMeta.uid} pod={pod} />
            ))}
        </ThemeDiv>
    );
};

const PodWidget = (props: {pod: Pod}) => (
    <Tooltip content={props.pod.objectMeta?.name}>
        <PodIcon status={props.pod.status} />
    </Tooltip>
);

const WidgetHeader = (props: {rollout: RolloutInfo; refresh: () => void}) => {
    const {rollout} = props;
    const [loading, setLoading] = React.useState(false);
    React.useEffect(() => {
        setTimeout(() => setLoading(false), 500);
    }, [loading]);
    return (
        <header>
            {rollout.objectMeta?.name}
            <span style={{marginLeft: 'auto', display: 'flex', alignItems: 'center'}}>
                <FontAwesomeIcon
                    icon={loading ? faCircleNotch : faRedoAlt}
                    style={{marginRight: '10px', fontSize: '14px'}}
                    className='rollouts-list__widget__refresh'
                    onClick={(e) => {
                        props.refresh();
                        setLoading(true);
                        e.preventDefault();
                    }}
                    spin={loading}
                />
                <StatusIcon status={rollout.status as RolloutStatus} />
            </span>
        </header>
    );
};
