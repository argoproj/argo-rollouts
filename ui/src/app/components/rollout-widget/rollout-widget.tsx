import * as React from 'react';
import {Link} from 'react-router-dom';

import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {faCircleNotch, faRedoAlt} from '@fortawesome/free-solid-svg-icons';
import {Tooltip} from 'antd';

import {ParsePodStatus, PodStatus, ReplicaSets} from '../pods/pods';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {useWatchRollout} from '../../shared/services/rollout';
import {useClickOutside} from '../../shared/utils/utils';
import {InfoItemKind, InfoItemRow} from '../info-item/info-item';
import {RolloutAction, RolloutActionButton} from '../rollout-actions/rollout-actions';
import {RolloutStatus, StatusIcon} from '../status-icon/status-icon';
import './rollout-widget.scss';

export const isInProgress = (rollout: RolloutInfo): boolean => {
    for (const rs of rollout.replicaSets || []) {
        for (const p of rs.pods || []) {
            const status = ParsePodStatus(p.status);
            if (status === PodStatus.Pending) {
                return true;
            }
        }
    }
    return false;
};

export const RolloutWidget = (props: {rollout: RolloutInfo; deselect: () => void; selected?: boolean}) => {
    const [watching, subscribe] = React.useState(false);
    let rollout = props.rollout;
    useWatchRollout(props.rollout?.objectMeta?.name, watching, null, (r: RolloutInfo) => (rollout = r));
    const ref = React.useRef(null);
    useClickOutside(ref, props.deselect);

    React.useEffect(() => {
        if (watching) {
            const to = setTimeout(() => {
                if (!isInProgress(rollout)) {
                    subscribe(false);
                }
            }, 5000);
            return () => clearTimeout(to);
        }
    }, [watching, rollout]);

    return (
        <Link
            to={`/rollout/${rollout.objectMeta?.namespace}/${rollout.objectMeta?.name}`}
            className={`rollouts-list__widget ${props.selected ? 'rollouts-list__widget--selected' : ''}`}
            ref={ref}
        >
            <WidgetHeader
                rollout={rollout}
                refresh={() => {
                    subscribe(true);
                    setTimeout(() => {
                        subscribe(false);
                    }, 1000);
                }}
            />
            <div className='rollouts-list__widget__body'>
                <InfoItemRow
                    label={'Strategy'}
                    items={{content: rollout.strategy, icon: rollout.strategy === 'BlueGreen' ? 'fa-palette' : 'fa-dove', kind: rollout.strategy.toLowerCase() as InfoItemKind}}
                />
                {(rollout.strategy || '').toLocaleLowerCase() === 'canary' && <InfoItemRow label={'Weight'} items={{content: rollout.setWeight, icon: 'fa-weight'}} />}
            </div>
            {/* {(rollout.replicaSets || []).length < 1 && <ReplicaSets replicaSets={rollout.replicaSets} showRevisions />} */}
            <ReplicaSets replicaSets={rollout.replicaSets} showRevisions />
            <div className='rollouts-list__widget__message'>{rollout.message !== 'CanaryPauseStep' && rollout.message}</div>
            <div className='rollouts-list__widget__actions'>
                <RolloutActionButton action={RolloutAction.Restart} rollout={rollout} callback={() => subscribe(true)} indicateLoading />
                <RolloutActionButton action={RolloutAction.Promote} rollout={rollout} callback={() => subscribe(true)} indicateLoading />
            </div>
        </Link>
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
                <Tooltip title='Refresh'>
                    <FontAwesomeIcon
                        icon={loading ? faCircleNotch : faRedoAlt}
                        spin={loading}
                        className={`rollouts-list__widget__refresh`}
                        style={{marginRight: '10px', fontSize: '14px'}}
                        onClick={(e) => {
                            props.refresh();
                            setLoading(true);
                            e.preventDefault();
                        }}
                    />
                </Tooltip>
                <StatusIcon status={rollout.status as RolloutStatus} />
            </span>
        </header>
    );
};
