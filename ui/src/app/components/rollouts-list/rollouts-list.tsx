import {Autocomplete, EffectDiv, InfoItemKind, InfoItemRow, Spinner, ThemeDiv, useAutocomplete, WaitFor} from 'argo-ui/v2';
import * as React from 'react';
import {Key, KeybindingContext, useNav} from 'react-keyhooks';
import {Link, useHistory} from 'react-router-dom';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {NamespaceContext} from '../../shared/context/api';
import {useWatchRollout, useWatchRollouts} from '../../shared/services/rollout';
import {useClickOutside} from '../../shared/utils/utils';
import {ParsePodStatus, PodStatus, ReplicaSets} from '../pods/pods';
import {RolloutAction, RolloutActionButton} from '../rollout-actions/rollout-actions';
import {RolloutStatus, StatusIcon} from '../status-icon/status-icon';
import './rollouts-list.scss';

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
    const [searchString, setSearchString, searchInput] = useAutocomplete('');
    const searchParam = new URLSearchParams(window.location.search).get('q');
    React.useEffect(() => {
      if (searchParam && searchParam != searchString) {
        setSearchString(searchParam);
      }
    }, []);

    const {useKeybinding, keybindingState} = React.useContext(KeybindingContext);

    // ignore H key when typing
    const hGroup = keybindingState.groupForKey[Key.H];
    const showHelpMenu = keybindingState.groups[hGroup][Key.H].action;
    keybindingState.groups[hGroup][Key.H].action = () => {
        if (searchInput.inputref.current === document.activeElement) {
            return false;
        } else {
            return showHelpMenu();
        }
    };

    useKeybinding(Key.RIGHT, () => nav(1));
    useKeybinding(Key.LEFT, () => nav(-1));
    useKeybinding(Key.ESCAPE, () => {
        reset();
        if (searchString && searchString !== '') {
            setSearchString('');
            return true;
        } else {
            return false;
        }
    });

    const rolloutNames = useRolloutNames(rollouts);
    const history = useHistory();

    useKeybinding(Key.SLASH, () => {
        if (!searchString) {
            if (searchInput.inputref.current) {
                searchInput.inputref.current.focus();
            }
            return true;
        }
        return false;
    });

    useKeybinding(Key.ENTER, () => {
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
        if (searchString) {
          history.replace(`/${namespaceCtx.namespace}?q=${searchString}`);
        }
    }, [searchString, rollouts]);

    const namespaceCtx = React.useContext(NamespaceContext);

    return (
        <div className='rollouts-list'>
            <WaitFor loading={loading}>
                {(rollouts || []).length > 0 ? (
                    <React.Fragment>
                        <ThemeDiv className='rollouts-list__toolbar'>
                            <div className='rollouts-list__search-container'>
                                <Autocomplete
                                    items={rolloutNames}
                                    className='rollouts-list__search'
                                    placeholder='Search...'
                                    style={{marginBottom: '1.5em'}}
                                    onItemClick={(item) => history.push(`/rollout/${namespaceCtx.namespace}/${item}`)}
                                    icon='fa-search'
                                    {...searchInput}
                                />
                            </div>
                        </ThemeDiv>
                        <div className='rollouts-list__rollouts-container'>
                            {(filteredRollouts.sort((a, b) => (a.objectMeta.name < b.objectMeta.name ? -1 : 1)) || []).map((rollout, i) => (
                                <RolloutWidget key={rollout.objectMeta?.uid} rollout={rollout} selected={i === pos} deselect={() => reset()} />
                            ))}
                        </div>
                    </React.Fragment>
                ) : (
                    <EmptyMessage namespace={namespaceCtx.namespace} />
                )}
            </WaitFor>
        </div>
    );
};

const EmptyMessage = (props: {namespace: string}) => {
    const CodeLine = (props: {children: string}) => {
        return <pre onClick={() => navigator.clipboard.writeText(props.children)}>{props.children}</pre>;
    };
    return (
        <ThemeDiv className='rollouts-list__empty-message'>
            <h1>No Rollouts to display!</h1>
            <div style={{textAlign: 'center', marginBottom: '1em'}}>
                <div>Make sure you are running the API server in the correct namespace. Your current namespace is: </div>
                <div style={{fontSize: '20px'}}>
                    <b>{props.namespace}</b>
                </div>
            </div>
            <div>
                To create a new Rollout and Service, run
                <CodeLine>kubectl apply -f https://raw.githubusercontent.com/argoproj/argo-rollouts/master/docs/getting-started/basic/rollout.yaml</CodeLine>
                <CodeLine>kubectl apply -f https://raw.githubusercontent.com/argoproj/argo-rollouts/master/docs/getting-started/basic/service.yaml</CodeLine>
                or follow the{' '}
                <a href='https://argo-rollouts.readthedocs.io/en/stable/getting-started/' target='_blank' rel='noreferrer'>
                    Getting Started guide
                </a>
                .
            </div>
        </ThemeDiv>
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
        <EffectDiv className={`rollouts-list__widget ${props.selected ? 'rollouts-list__widget--selected' : ''}`} innerref={ref}>
            <Link to={`/rollout/${rollout.objectMeta?.namespace}/${rollout.objectMeta?.name}`} className='rollouts-list__widget__container'>
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
                        items={{content: rollout.strategy, icon: rollout.strategy === 'BlueGreen' ? 'fa-palette' : 'fa-dove', kind: rollout.strategy.toLowerCase() as InfoItemKind}}
                    />
                    {(rollout.strategy || '').toLocaleLowerCase() === 'canary' && <InfoItemRow label={'Weight'} items={{content: rollout.setWeight, icon: 'fa-weight'}} />}
                </ThemeDiv>
                <WaitFor loading={(rollout.replicaSets || []).length < 1} loader={<Spinner />}>
                    <ReplicaSets replicaSets={rollout.replicaSets} showRevisions />
                </WaitFor>
                <div className='rollouts-list__widget__actions'>
                    <RolloutActionButton action={RolloutAction.Restart} rollout={rollout} callback={() => subscribe(true)} indicateLoading />
                    <RolloutActionButton action={RolloutAction.Promote} rollout={rollout} callback={() => subscribe(true)} indicateLoading />
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
                <i
                    className={`rollouts-list__widget__refresh fa ${loading ? 'fa-circle-notch' : 'fa-redo-alt'} ${loading ? 'fa-spin' : ''}`}
                    style={{marginRight: '10px', fontSize: '14px'}}
                    onClick={(e) => {
                        props.refresh();
                        setLoading(true);
                        e.preventDefault();
                    }}
                />
                <StatusIcon status={rollout.status as RolloutStatus} />
            </span>
        </header>
    );
};
