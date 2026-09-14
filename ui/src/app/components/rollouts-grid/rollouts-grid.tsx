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

    const handleFavoriteChange = (rolloutName: string, isFavorite: boolean) => {
        onFavoriteChange(rolloutName, isFavorite);
    };

    const orderedRollouts = rollouts
        .map((rollout) => {
            return {
                ...rollout,
                key: rollout.objectMeta?.uid,
                favorite: favorites[rollout.objectMeta?.name] || false,
            };
        })
        .sort((a, b) => {
            if (a.favorite && !b.favorite) {
                return -1;
            } else if (!a.favorite && b.favorite) {
                return 1;
            } else {
                return 0;
            }
        });

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
                    deselect={() => reset()}
                    isFavorite={favorites[rollout.objectMeta?.name] || false}
                    onFavoriteChange={handleFavoriteChange}
                />
            ))}
        </div>
    );
};
