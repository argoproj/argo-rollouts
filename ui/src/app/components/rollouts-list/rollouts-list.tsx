import {faCircleNotch, faDove, faPalette, faRedoAlt, faWeight} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';
import {Link, useHistory} from 'react-router-dom';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {useWatchRollout, useWatchRollouts} from '../../shared/services/rollout';
import {InfoItemKind, InfoItemRow} from '../info-item/info-item';
import {RolloutStatus, StatusIcon} from '../status-icon/status-icon';
import {Spinner, WaitFor} from '../wait-for/wait-for';
import {Key, useKeyListener, useNav} from 'react-keyhooks';
import './rollouts-list.scss';
import {ThemeDiv} from '../theme-div/theme-div';
import {RolloutAction, RolloutActionButton} from '../rollout-actions/rollout-actions';
import {ParsePodStatus, PodStatus, ReplicaSet} from '../pods/pods';
import {EffectDiv} from '../effect-div/effect-div';
import {Autocomplete} from '../autocomplete/autocomplete';
import {useInput} from '../input/input';

const useRolloutNames = (rollouts: RolloutInfo[]) => {
    const parseNames = (rl: RolloutInfo[]) => (rl || []).map((r) => r.objectMeta?.name || '');

    const [rolloutNames, setRolloutNames] = React.useState(parseNames(rollouts));
    React.useEffect(() => {
        setRolloutNames(parseNames(rollouts));
    }, [rollouts]);

    return rolloutNames;
};

export const RolloutsList = () => {
    const rolloutsList = useWatchRollouts();
    const rollouts = rolloutsList.items;
    const loading = rolloutsList.loading;
    const [filteredRollouts, setFilteredRollouts] = React.useState(rollouts);
    const [pos, nav, reset] = useNav(filteredRollouts.length);
    const [searchString, setSearchString, searchInput] = useInput('');

    const useKeyPress = useKeyListener();

    useKeyPress(Key.RIGHT, () => nav(1));
    useKeyPress(Key.LEFT, () => nav(-1));
    useKeyPress(Key.ESCAPE, () => {
        reset();
        setSearchString('');
        return true;
    });

    const rolloutNames = useRolloutNames(rollouts);
    const history = useHistory();

    useKeyPress(Key.ENTER, () => {
        if (pos > -1) {
            history.push(`/rollout/${filteredRollouts[pos].objectMeta?.name}`);
            return true;
        }
        return false;
    });

    React.useEffect(() => {
        const filtered = (rollouts || []).filter((r) => (r.objectMeta?.name || '').includes(searchString));
        if ((filtered || []).length > 0) {
            setFilteredRollouts(filtered);
        }
    }, [searchString, rollouts]);

    return (
        <div className='rollouts-list'>
            <WaitFor loading={loading}>
                <div style={{width: '100%'}}>
                    <div className='rollouts-list__search-container'>
                        <Autocomplete
                            items={rolloutNames}
                            className='rollouts-list__search'
                            placeholder='Search...'
                            inputStyle={{paddingTop: '0.75em', paddingBottom: '0.75em'}}
                            style={{marginBottom: '1.5em'}}
                            onItemClick={(item) => history.push(`/rollout/${item}`)}
                            {...searchInput}
                        />
                    </div>
                </div>
                {(filteredRollouts.sort((a, b) => (a.objectMeta.name < b.objectMeta.name ? -1 : 1)) || []).map((rollout, i) => (
                    <RolloutWidget key={rollout.objectMeta?.uid} rollout={rollout} selected={i === pos} />
                ))}
            </WaitFor>
        </div>
    );
};

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

export const RolloutWidget = (props: {rollout: RolloutInfo; selected?: boolean}) => {
    const [watching, subscribe] = React.useState(false);
    let rollout = props.rollout;
    useWatchRollout(props.rollout?.objectMeta?.name, watching, null, (r: RolloutInfo) => (rollout = r));

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
        <EffectDiv className={`rollouts-list__widget ${props.selected ? 'rollouts-list__widget--selected' : ''}`}>
            <Link to={`/rollout/${rollout.objectMeta?.name}`} className='rollouts-list__widget__container'>
                <WidgetHeader
                    rollout={rollout}
                    refresh={() => {
                        subscribe(true);
                        setTimeout(() => {
                            subscribe(false);
                        }, 1000);
                    }}
                />
                <ThemeDiv className='rollouts-list__widget__body'>
                    <InfoItemRow
                        label={'Strategy'}
                        items={{content: rollout.strategy, icon: rollout.strategy === 'BlueGreen' ? faPalette : faDove, kind: rollout.strategy.toLowerCase() as InfoItemKind}}
                    />
                    {(rollout.strategy || '').toLocaleLowerCase() === 'canary' && <InfoItemRow label={'Weight'} items={{content: rollout.setWeight, icon: faWeight}} />}
                </ThemeDiv>
                <WaitFor loading={(rollout.replicaSets || []).length < 1} loader={<Spinner />}>
                    {rollout.replicaSets?.map(
                        (rsInfo) =>
                            rsInfo.pods &&
                            rsInfo.pods.length > 0 && (
                                <div className='rollouts-list__widget__pods' key={rsInfo.objectMeta.uid}>
                                    <ReplicaSet rs={rsInfo} />
                                </div>
                            )
                    )}
                </WaitFor>
                <div className='rollouts-list__widget__actions'>
                    <RolloutActionButton action={RolloutAction.Restart} rollout={rollout} callback={() => subscribe(true)} indicateLoading />
                    <RolloutActionButton action={RolloutAction.PromoteFull} rollout={rollout} callback={() => subscribe(true)} indicateLoading />
                </div>
            </Link>
        </EffectDiv>
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
