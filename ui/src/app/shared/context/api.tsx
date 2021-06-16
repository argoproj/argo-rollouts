import * as React from 'react';
import {RolloutServiceApi} from '../../../models/rollout/generated';

export const RolloutAPI = new RolloutServiceApi();
export const RolloutAPIContext = React.createContext(RolloutAPI);

export const APIProvider = (props: {children: React.ReactNode}) => {
    return <RolloutAPIContext.Provider value={RolloutAPI}>{props.children}</RolloutAPIContext.Provider>;
};

export const NAMESPACE_KEY = 'namespace';
const init = window.localStorage.getItem(NAMESPACE_KEY);
interface NamespaceContextProps {
    namespace: string;
    availableNamespaces?: Array<string>;
    set: (ns: string) => void;
}

export const NamespaceContext = React.createContext({namespace : init} as NamespaceContextProps);

export const NamespaceProvider = (props: {children: React.ReactNode}) => {
    const [namespace, setNamespace] = React.useState(init);
    const [availableNamespaces, setAvailableNamespaces] = React.useState([]);
    React.useEffect(() => {
        console.log("update namespace in local storage: " + namespace)
        window.localStorage.setItem(NAMESPACE_KEY, namespace);
    }, [namespace]);
    React.useEffect(() => {
        const getNs = async () => {
            const nsInfo = await RolloutAPI.rolloutServiceGetNamespace();
            setAvailableNamespaces(nsInfo.availableNamespaces);
            let localNamespace = localStorage.getItem("namespace");
            setNamespace(localNamespace ? localNamespace : nsInfo.namespace)
        };
        getNs();
    })
    return <NamespaceContext.Provider value={ {
        namespace: namespace,
        availableNamespaces: availableNamespaces,
        set: (ns) => setNamespace(ns)
    } }>{props.children}</NamespaceContext.Provider>
}
