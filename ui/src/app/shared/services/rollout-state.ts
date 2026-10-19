import {ListState} from '../utils/watch';

export const loadingListState = <T>(): ListState<T> => ({items: [], loading: true, error: null});

export const loadedListState = <T>(items: T[]): ListState<T> => ({items, loading: false, error: null});

export const failedListState = <T>(error: unknown): ListState<T> & {error: Error} => ({
    items: [],
    loading: false,
    error: error instanceof Error ? error : new Error('An unexpected error occurred while fetching rollouts.'),
});
