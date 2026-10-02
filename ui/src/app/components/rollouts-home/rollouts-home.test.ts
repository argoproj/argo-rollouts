import {getRolloutsHomeView} from './rollouts-home-state';
import {ListState} from '../../shared/utils/watch';
import {RolloutInfo} from '../../../models/rollout/rollout';

const state = (overrides: Partial<ListState<RolloutInfo>>): ListState<RolloutInfo> => ({
    items: [],
    loading: false,
    error: null,
    ...overrides,
});

describe('getRolloutsHomeView', () => {
    it('shows loading while the initial request is pending', () => {
        expect(getRolloutsHomeView(state({loading: true}))).toBe('loading');
    });

    it('shows the empty state after a successful empty response', () => {
        expect(getRolloutsHomeView(state({items: []}))).toBe('empty');
    });

    it('shows Rollouts after a successful non-empty response', () => {
        expect(getRolloutsHomeView(state({items: [{} as RolloutInfo]}))).toBe('rollouts');
    });

    it('shows a persistent error after a failed response', () => {
        expect(getRolloutsHomeView(state({error: new Error('request failed')}))).toBe('error');
    });
});
