import {faCircleNotch, faClock, faDove, faHistory, faPalette, faRedoAlt} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';
import {Link} from 'react-router-dom';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {useWatchRollout, useWatchRollouts} from '../../shared/services/rollout';
import {formatTimestamp} from '../../shared/utils/utils';
import {InfoItemRow} from '../info-item/info-item';
import {RolloutStatus, StatusIcon} from '../status-icon/status-icon';
import {WaitFor} from '../wait-for/wait-for';
import {Key, useKeyListener, useNav} from '@rbreeze/react-keypress';
import './rollouts-list.scss';
import {ThemeDiv} from '../theme-div/theme-div';
import {RolloutAction, RolloutActionButton} from '../rollout-actions/rollout-actions';
import {ReplicaSet} from '../pods/pods';

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
                {(rollouts.sort((a, b) => (a.objectMeta.name < b.objectMeta.name ? -1 : 1)) || []).map((rollout, i) => (
                    <RolloutWidget key={rollout.objectMeta?.uid} rollout={rollout} selected={i === pos} />
                ))}
            </WaitFor>
        </div>
    );
};

export const RolloutWidget = (props: {rollout: RolloutInfo; selected?: boolean}) => {
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
                <ThemeDiv className='rollouts-list__widget__body'>
                    <InfoItemRow label={'Strategy'} content={{content: rollout.strategy, icon: rollout.strategy === 'BlueGreen' ? faPalette : faDove}} />
                    <InfoItemRow label={'Generation'} content={{content: `${rollout.generation || 0}`, icon: faHistory}} />
                    <InfoItemRow label={'Restarted At'} content={{content: formatTimestamp(rollout.restartedAt as string) || 'Never', icon: faClock}} />
                </ThemeDiv>
                {rollout.replicaSets?.map(
                    (rsInfo) =>
                        rsInfo.pods &&
                        rsInfo.pods.length > 0 && (
                            <div className='rollouts-list__widget__pods' key={rsInfo.objectMeta.uid}>
                                <ReplicaSet rs={rsInfo} />
                            </div>
                        )
                )}
                <div className='rollouts-list__widget__actions'>
                    <RolloutActionButton action={RolloutAction.Restart} name={rollout.objectMeta?.name} callback={() => subscribe(true)} indicateLoading />
                    <RolloutActionButton action={RolloutAction.PromoteFull} name={rollout.objectMeta?.name} callback={() => subscribe(true)} indicateLoading />
                </div>
            </Link>
        </ThemeDiv>
    );
};

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
