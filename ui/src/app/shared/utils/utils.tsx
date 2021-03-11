import * as React from 'react';
import * as moment from 'moment';
import {faDove, faQuestion, faRunning, faSearch, faThumbsUp} from '@fortawesome/free-solid-svg-icons';
import {GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1ReplicaSetInfo} from '../../../models/rollout/generated';

export function useServerData<T>(getData: () => Promise<T>) {
    const [data, setData] = React.useState({} as T);
    React.useEffect(() => {
        const fx = async () => {
            const data = await getData();
            setData(data);
        };
        fx();
    }, [getData]);
    return data as T;
}

export function formatTimestamp(ts: string): string {
    const inputFormat = 'YYYY-MM-DD HH:mm:ss Z z';
    const m = moment(ts, inputFormat);
    if (!ts || !m.isValid()) {
        return 'Never';
    }
    return m.format('MMM D YYYY [at] hh:mm:ss');
}

export enum ImageTag {
    Canary = 'canary',
    Stable = 'stable',
    Active = 'active',
    Preview = 'preview',
    Unknown = 'unknown',
}

export const IconForTag = (t?: ImageTag) => {
    switch (t) {
        case ImageTag.Canary:
            return faDove;
        case ImageTag.Stable:
            return faThumbsUp;
        case ImageTag.Preview:
            return faSearch;
        case ImageTag.Active:
            return faRunning;
        default:
            return faQuestion;
    }
};

export const ParseTagsFromReplicaSet = (rs: GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1ReplicaSetInfo): ImageTag[] => {
    const tags = [] as ImageTag[];
    if (rs.canary) {
        tags.push(ImageTag.Canary);
    }
    if (rs.stable) {
        tags.push(ImageTag.Stable);
    }
    if (rs.active) {
        tags.push(ImageTag.Active);
    }
    if (rs.preview) {
        tags.push(ImageTag.Preview);
    }
    return tags;
};
