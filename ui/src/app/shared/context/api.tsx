import * as React from 'react';
import {RolloutServiceApi} from '../../../models/rollout/generated';

export const RolloutAPI = new RolloutServiceApi();
export const RolloutAPIContext = React.createContext(RolloutAPI);

export const APIProvider = (props: {children: React.ReactNode}) => {
    return <RolloutAPIContext.Provider value={RolloutAPI}>{props.children}</RolloutAPIContext.Provider>;
};

export const NamespaceContext = React.createContext('');

export const NamespaceProvider = (props: {children: React.ReactNode}) => {
    const [namespace, setNamespace] = React.useState('');
    React.useEffect(() => {
        const getNs = async () => {
            const nsInfo = await RolloutAPI.rolloutServiceGetNamespace(); 
            setNamespace(nsInfo.namespace);
        };
        getNs();
    })
    return <NamespaceContext.Provider value={namespace}>{props.children}</NamespaceContext.Provider>
}
