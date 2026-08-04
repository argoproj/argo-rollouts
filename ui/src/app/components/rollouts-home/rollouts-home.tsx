import * as React from 'react';

import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {faCircleNotch} from '@fortawesome/free-solid-svg-icons';

import {NamespaceContext} from '../../shared/context/api';
import {useWatchRollouts} from '../../shared/services/rollout';
import {RolloutsToolbar, defaultDisplayMode, Filters} from '../rollouts-toolbar/rollouts-toolbar';
import {RolloutsTable} from '../rollouts-table/rollouts-table';
import {RolloutsGrid} from '../rollouts-grid/rollouts-grid';
import './rollouts-home.scss';

export const RolloutsHome = () => {
    const rolloutsList = useWatchRollouts();
    const rollouts = rolloutsList.items;
    const loading = rolloutsList.loading;
    const namespaceCtx = React.useContext(NamespaceContext);

    const [filters, setFilters] = React.useState<Filters>({
        showRequiresAttention: false,
        showFavorites: false,
        name: '',
        displayMode: defaultDisplayMode,
        status: {
            progressing: false,
            degraded: false,
            paused: false,
            healthy: false,
        },
    });

    const handleFilterChange = (newFilters: Filters) => {
        setFilters(newFilters);
    };

    const [favorites, setFavorites] = React.useState(() => {
        const favoritesStr = localStorage.getItem('rolloutsFavorites');
        return favoritesStr ? JSON.parse(favoritesStr) : {};
    });

    const handleFavoriteChange = (rolloutName: string, isFavorite: boolean) => {
        const newFavorites = {...favorites};
        if (isFavorite) {
            newFavorites[rolloutName] = true;
        } else {
            delete newFavorites[rolloutName];
        }
        setFavorites(newFavorites);
        localStorage.setItem('rolloutsFavorites', JSON.stringify(newFavorites));
    };

    const filteredRollouts = React.useMemo(() => {
        return rollouts.filter((r) => {
            // If no filters are set, show all rollouts
            if (filters.name === '' && !filters.showFavorites && !filters.showRequiresAttention && !Object.values(filters.status).some((value) => value === true)) {
                return true;
            }

            const statusFiltersSet = Object.values(filters.status).some((value) => value === true);
            const nameFilterSet = filters.name !== '';

            let favoritesMatches = false;
            let requiresAttentionMatches = false;
            let statusMatches = false;
            let nameMatches = false;
            
            if (filters.showFavorites && favorites[r.objectMeta.name]) {
                favoritesMatches = true;
            }
            if (filters.showRequiresAttention && (r.status === 'Unknown' || r.status === 'Degraded' || (r.status === 'Paused' && r.message !== 'CanaryPauseStep'))) {
                requiresAttentionMatches = true;
            }
            if (statusFiltersSet && filters.status[r.status]) {
                statusMatches = true;
            }
            
            for (let term of filters.name.split(',').map((t) => t.trim())) {
                if (term === '') continue; // Skip empty terms

                if (term.includes(':')) {
                    // Filter by label
                    const [key, value] = term.split(':');
                    if (value.startsWith('"') && value.endsWith('"')) {
                        const exactValue = value.substring(1, value.length - 1);
                        if (r.objectMeta.labels && r.objectMeta.labels[key] && r.objectMeta.labels[key] === exactValue) {
                            nameMatches = true;
                            break;
                        }
                    } else if (r.objectMeta.labels && r.objectMeta.labels[key] && r.objectMeta.labels[key].includes(value)) {
                        nameMatches = true;
                        break;
                    }
                } else {
                    // Filter by name
                    const isNegated = term.startsWith('!');
                    term = term.replace(/^!/, '');

                    const isExact = term.startsWith('"') && term.endsWith('"');
                    term = term.replace(/"/g, '');

                    if (isExact) {
                        if (isNegated) {
                            if (r.objectMeta.name !== term) {
                                nameMatches = true;
                                continue;
                            }
                        } else {
                            if (r.objectMeta.name === term) {
                                nameMatches = true;
                                break;
                            }
                        }
                    } else {
                        if (isNegated) {
                            if (!r.objectMeta.name.includes(term)) {
                                nameMatches = true;
                                break;
                            }
                        } else {
                            if (r.objectMeta.name.includes(term)) {
                                nameMatches = true;
                                break;
                            }
                        }
                    }
                }
            }

            return (
                (!nameFilterSet || nameMatches) && 
                (!filters.showFavorites || favoritesMatches) &&
                (!filters.showRequiresAttention || requiresAttentionMatches) &&
                (!statusFiltersSet || statusMatches)
            );
        });
    }, [rollouts, filters, favorites]);

    return (
        <div className='rollouts-home'>
            <RolloutsToolbar rollouts={rollouts} favorites={favorites} onFilterChange={handleFilterChange} />
            <div className='rollouts-list'>
                {loading ? (
                    <div style={{fontSize: '20px', padding: '20px', margin: '0 auto'}}>
                        <FontAwesomeIcon icon={faCircleNotch} spin={true} style={{marginRight: '5px'}} />
                        Loading...
                    </div>
                ) : (rollouts || []).length > 0 ? (
                    <React.Fragment>
                        {filters.displayMode === 'table' && <RolloutsTable rollouts={filteredRollouts} onFavoriteChange={handleFavoriteChange} favorites={favorites} />}
                        {filters.displayMode !== 'table' && <RolloutsGrid rollouts={filteredRollouts} onFavoriteChange={handleFavoriteChange} favorites={favorites} />}
                    </React.Fragment>
                ) : (
                    <EmptyMessage namespace={namespaceCtx.namespace} />
                )}
            </div>
        </div>
    );
};

const EmptyMessage = (props: {namespace: string}) => {
    const CodeLine = (props: {children: string}) => {
        return (
        <pre onClick={() => navigator.clipboard.writeText(props.children)}
            onKeyDown={() => navigator.clipboard.writeText(props.children)}
            >{props.children}</pre>);
    };
    return (
        <div className='rollouts-list__empty-message'>
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
        </div>
    );
};
