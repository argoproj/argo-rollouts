import {ListState} from '../../shared/utils/watch';

export type RolloutsHomeView = 'loading' | 'error' | 'rollouts' | 'empty';

export const getRolloutsHomeView = (state: ListState<unknown>): RolloutsHomeView => {
    if (state.loading) {
        return 'loading';
    }
    if (state.error) {
        return 'error';
    }
    return state.items.length > 0 ? 'rollouts' : 'empty';
};
