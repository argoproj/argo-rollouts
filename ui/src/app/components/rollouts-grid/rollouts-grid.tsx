import * as React from 'react';
import {useHistory} from 'react-router-dom';
import {Key, KeybindingContext, useNav} from 'react-keyhooks';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {RolloutGridWidget} from '../rollout-grid-widget/rollout-grid-widget';
import './rollouts-grid.scss';

export const RolloutsGrid = ({
    rollouts,
    onFavoriteChange,
    favorites,
}: {
    rollouts: RolloutInfo[];
    onFavoriteChange: (rolloutName: string, isFavorite: boolean) => void;
    favorites: {[key: string]: boolean};
}) => {
    const [itemsPerRow, setItemsPerRow] = React.useState(0);
    const rolloutsGridRef = React.useRef(null);

    const orderedRollouts = React.useMemo(() => {
        const isFavorite = (rollout: RolloutInfo) => favorites[rollout.objectMeta?.name] || false;
        return [...rollouts].sort((a, b) => {
            if (isFavorite(a) && !isFavorite(b)) {
                return -1;
            } else if (!isFavorite(a) && isFavorite(b)) {
                return 1;
            } else {
                return 0;
            }
        });
    }, [rollouts, favorites]);

    // Calculate the number of items per row for keyboard navigation
    React.useEffect(() => {
        const rolloutsGrid = rolloutsGridRef.current;

        const updateItemsPerRow = () => {
            if (rolloutsGrid) {
                const rolloutsListWidget = document.querySelector('.rollouts-list__widget');
                if (!rolloutsListWidget) {
                    return;
                }
                const containerWidth = rolloutsGrid.clientWidth;
                const widgetWidth = parseInt(getComputedStyle(rolloutsListWidget).getPropertyValue('width'), 10);
                const widgetPadding = parseInt(getComputedStyle(rolloutsListWidget).getPropertyValue('padding'), 10);
                const itemsPerRowValue = Math.floor(containerWidth / (widgetWidth + widgetPadding * 2));
                setItemsPerRow(itemsPerRowValue);
            }
        };

        updateItemsPerRow();

        window.addEventListener('resize', updateItemsPerRow);

        return () => {
            window.removeEventListener('resize', updateItemsPerRow);
        };
    }, []);

    const history = useHistory();
    const [pos, nav, reset] = useNav(orderedRollouts.length);
    const resetRef = React.useRef(reset);
    resetRef.current = reset;
    const deselect = React.useCallback(() => resetRef.current(), []);
    const {useKeybinding} = React.useContext(KeybindingContext);

    useKeybinding(Key.RIGHT, () => nav(1));
    useKeybinding(Key.LEFT, () => nav(-1));
    useKeybinding(Key.UP, () => nav(-itemsPerRow));
    useKeybinding(Key.DOWN, () => nav(itemsPerRow));
    useKeybinding(Key.ENTER, () => {
        if (pos !== undefined) {
            history.push(`/rollout/${orderedRollouts[pos].objectMeta?.name}`);
            return true;
        }
        return false;
    });

    return (
        <div className='rollouts-grid' ref={rolloutsGridRef}>
            {orderedRollouts.map((rollout, i) => (
                <RolloutGridWidget
                    key={rollout.objectMeta?.uid}
                    rollout={rollout}
                    selected={i === pos}
                    deselect={deselect}
                    isFavorite={favorites[rollout.objectMeta?.name] || false}
                    onFavoriteChange={onFavoriteChange}
                />
            ))}
        </div>
    );
};
