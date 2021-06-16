import * as React from 'react';
import {RolloutNamespaceInfo, RolloutServiceApi} from '../../../models/rollout/generated';

export const RolloutAPI = new RolloutServiceApi();
export const RolloutAPIContext = React.createContext(RolloutAPI);

export const APIProvider = (props: {children: React.ReactNode}) => {
    return <RolloutAPIContext.Provider value={RolloutAPI}>{props.children}</RolloutAPIContext.Provider>;
};

export const NamespaceContext = React.createContext<RolloutNamespaceInfo>({namespace: '', availableNamespaces: []});
