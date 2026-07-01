import * as React from 'react';
import {RolloutNamespaceInfo, RolloutServiceApi, Configuration} from '../../../models/rollout/generated';
import {AuthContext, createAuthFetch} from './auth';

// Get the base path from document.baseURI
// The generated API client already includes /api in its paths, so we just need the base
let cachedBasePath: string | null = null;

const getApiBasePath = (): string => {
    if (cachedBasePath === null) {
        const baseURI = new URL(document.baseURI);
        cachedBasePath = baseURI.pathname.replace(/\/$/, '');
    }
    return cachedBasePath;
};

// Export the base path function for use in other modules
export { getApiBasePath };


const basePath = getApiBasePath();
export const RolloutAPI = new RolloutServiceApi(new Configuration({ basePath }));
export const RolloutAPIContext = React.createContext(RolloutAPI);

export const APIProvider = (props: {children: React.ReactNode}) => {
    return <RolloutAPIContext.Provider value={RolloutAPI}>{props.children}</RolloutAPIContext.Provider>;
};

// AuthAwareAPIProvider creates an API client that injects the bearer token into all requests.
export const AuthAwareAPIProvider = (props: {children: React.ReactNode}) => {
    const {token} = React.useContext(AuthContext);
    const api = React.useMemo(() => {
        const authFetch = createAuthFetch(token);
        return new RolloutServiceApi(new Configuration({basePath: getApiBasePath()}), getApiBasePath(), authFetch);
    }, [token]);

    return <RolloutAPIContext.Provider value={api}>{props.children}</RolloutAPIContext.Provider>;
};

export const NamespaceContext = React.createContext<RolloutNamespaceInfo>({namespace: '', availableNamespaces: []});
