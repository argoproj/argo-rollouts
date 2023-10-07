import * as React from 'react';

import {useHistory, useLocation} from 'react-router-dom';

import {AutoComplete} from 'antd';
import {Tooltip} from 'antd';

import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {faTableList, faTableCellsLarge} from '@fortawesome/free-solid-svg-icons';

import {RolloutInfo} from '../../../models/rollout/rollout';
import {StatusCount} from '../status-count/status-count';
import './rollouts-toolbar.scss';

export type Filters = {
    showRequiresAttention: boolean;
    showFavorites?: boolean;
    name: string;
    displayMode?: string;
    status: {
        [key: string]: boolean;
    };
};

interface StatusCount {
    [key: string]: number;
}

export const defaultDisplayMode = 'table';

export const RolloutsToolbar = ({
    rollouts,
    favorites,
    onFilterChange,
}: {
    rollouts: RolloutInfo[];
    favorites: {[key: string]: boolean};
    onFilterChange: (filters: Filters) => void;
}) => {
    const history = useHistory();
    const location = useLocation();
    const searchParams = new URLSearchParams(window.location.search);
    const [filters, setFilters] = React.useState<Filters>({
        showRequiresAttention: searchParams.get('showRequiresAttention') === 'true',
        showFavorites: searchParams.get('showFavorites') === 'true',
        name: searchParams.get('name') || '',
        displayMode: searchParams.get('displayMode') || defaultDisplayMode,
        status: {
            Progressing: searchParams.get('Progressing') === 'true',
            Degraded: searchParams.get('Degraded') === 'true',
            Paused: searchParams.get('Paused') === 'true',
            Healthy: searchParams.get('Healthy') === 'true',
        },
    });
    // Ensure that the filters are updated when the URL changes
    onFilterChange(filters);

    const handleFilterChange = (newFilters: Filters) => {
        setFilters(newFilters);
        onFilterChange(newFilters);
    };

    const handleNameFilterChange = (value: string) => {
        const newFilters = {
            ...filters,
            name: value,
        };
        const searchParams = new URLSearchParams(location.search);
        if (value) {
            searchParams.set('name', value);
        } else {
            searchParams.delete('name');
        }
        history.push({search: searchParams.toString()});
        handleFilterChange(newFilters);
    };

    const handleShowRequiresAttentionChange = (event: React.MouseEvent<HTMLButtonElement>) => {
        const newFilters = {
            ...filters,
            showRequiresAttention: !filters.showRequiresAttention,
        };
        const searchParams = new URLSearchParams(location.search);
        if (!filters.showRequiresAttention) {
            searchParams.set('showRequiresAttention', 'true');
        } else {
            searchParams.delete('showRequiresAttention');
        }
        history.push({search: searchParams.toString()});
        handleFilterChange(newFilters);
    };

    const handleShowFavoritesChange = (event: React.MouseEvent<HTMLButtonElement>) => {
        const newFilters = {
            ...filters,
            showFavorites: !filters.showFavorites,
        };
        const searchParams = new URLSearchParams(location.search);
        if (!filters.showFavorites) {
            searchParams.set('showFavorites', 'true');
        } else {
            searchParams.delete('showFavorites');
        }
        history.push({search: searchParams.toString()});
        handleFilterChange(newFilters);
    };

    const handleDisplayModeChange = (event: React.MouseEvent<HTMLButtonElement>) => {
        const newFilters = {
            ...filters,
            displayMode: event.currentTarget.id,
        };
        const searchParams = new URLSearchParams(location.search);
        if (event.currentTarget.id !== defaultDisplayMode) {
            searchParams.set('displayMode', event.currentTarget.id);
        } else {
            searchParams.delete('displayMode');
        }
        history.push({search: searchParams.toString()});
        handleFilterChange(newFilters);
    };

    const handleStatusFilterChange = (event: React.MouseEvent<HTMLButtonElement>) => {
        const newFilters = {
            ...filters,
            status: {
                ...filters.status,
                [event.currentTarget.id]: !filters.status[event.currentTarget.id],
            },
        };
        const searchParams = new URLSearchParams(location.search);
        if (event.currentTarget.id) {
            searchParams.set(event.currentTarget.id, 'true');
        } else {
            searchParams.delete(event.currentTarget.id);
        }
        history.push({search: searchParams.toString()});
        handleFilterChange(newFilters);
    };

    const statusCounts: StatusCount = React.useMemo(() => {
        const counts: StatusCount = {
            Progressing: 0,
            Degraded: 0,
            Paused: 0,
            Healthy: 0,
        };
        rollouts.forEach((r) => {
            counts[r.status]++;
        });

        return counts;
    }, [rollouts]);

    const needsAttentionCount: number = React.useMemo(() => {
        const pausedRollouts = rollouts.filter((r) => r.status === 'Paused' && r.message !== 'CanaryPauseStep');
        return statusCounts['Degraded'] + pausedRollouts.length;
    }, [rollouts, statusCounts]);

    const favoriteCount: number = React.useMemo(() => {
        return rollouts.filter((r) => favorites[r.objectMeta.name]).length;
    }, [rollouts, favorites]);

    return (
        <div className='rollouts-toolbar'>
            <div className='rollouts-toolbar_status-filters'>
                <Tooltip title={'Show Only Favorites'}>
                    <button className={`rollouts-toolbar_status-button`} onClick={handleShowFavoritesChange}>
                        <StatusCount status={'Favorite'} count={favoriteCount} defaultIcon='fa-star' active={filters.showFavorites} />
                    </button>
                </Tooltip>
                <Tooltip title={'Show Only Rollouts Requiring Attention'}>
                    <button className='rollouts-toolbar_status-button' onClick={handleShowRequiresAttentionChange}>
                        <StatusCount status={'NeedsAttention'} count={needsAttentionCount} active={filters.showRequiresAttention} />
                    </button>
                </Tooltip>
                {Object.keys(statusCounts).map((status: string) => {
                    return (
                        <Tooltip key={status} title={'Show Only ' + status + ' Rollouts'}>
                            <button id={status} className='rollouts-toolbar_status-button' onClick={handleStatusFilterChange}>
                                <StatusCount key={status} status={status} count={statusCounts[status]} active={filters.status[status]} />
                            </button>
                        </Tooltip>
                    );
                })}
            </div>
            <div className='rollouts-toolbar_display-modes'>
                <button id='table' className={`rollouts-toolbar_mode-button ${filters.displayMode === 'table' ? 'active' : ''}`} onClick={handleDisplayModeChange}>
                    <FontAwesomeIcon icon={faTableList} />
                </button>
                <button id='grid' className={`rollouts-toolbar_mode-button ${filters.displayMode === 'grid' ? 'active' : ''}`} onClick={handleDisplayModeChange}>
                    <FontAwesomeIcon icon={faTableCellsLarge} />
                </button>
            </div>
            <AutoComplete className='rollouts-toolbar_search-container' autoFocus placeholder='Filter by name' value={filters.name} onChange={handleNameFilterChange} />
        </div>
    );
};
