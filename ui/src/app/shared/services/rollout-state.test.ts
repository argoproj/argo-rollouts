import {RolloutInfo} from '../../../models/rollout/rollout';
import {failedListState, loadedListState, loadingListState} from './rollout-state';

const rollout = {objectMeta: {name: 'example'}} as RolloutInfo;

describe('Rollouts list state', () => {
    it('is loading while the initial request is pending', () => {
        expect(loadingListState<RolloutInfo>()).toEqual({items: [], loading: true, error: null});
    });

    it('finishes loading after a successful empty response', () => {
        expect(loadedListState<RolloutInfo>([])).toEqual({items: [], loading: false, error: null});
    });

    it('contains Rollouts after a successful non-empty response', () => {
        expect(loadedListState([rollout])).toEqual({items: [rollout], loading: false, error: null});
    });

    it('finishes loading and retains an error after a failed response', () => {
        const error = new Error('request failed');
        expect(failedListState<RolloutInfo>(error)).toEqual({items: [], loading: false, error});
    });
});
