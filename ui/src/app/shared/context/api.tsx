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

// createRolloutAPI creates a RolloutServiceApi with optional auth token
const createRolloutAPI = (token: string | null): RolloutServiceApi => {
    const basePath = getApiBasePath();
    const authFetch = createAuthFetch(token);
    return new RolloutServiceApi(new Configuration({basePath}), basePath, authFetch);
};

const basePath = getApiBasePath();
export const RolloutAPI = new RolloutServiceApi(new Configuration({ basePath }));
export const RolloutAPIContext = React.createContext(RolloutAPI);

export const AuthAwareAPIProvider = (props: {children: React.ReactNode}) => {
    const {token} = React.useContext(AuthContext);
    const api = React.useMemo(() => createRolloutAPI(token), [token]);

    return <RolloutAPIContext.Provider value={api}>{props.children}</RolloutAPIContext.Provider>;
};

export const NamespaceContext = React.createContext<RolloutNamespaceInfo>({namespace: '', availableNamespaces: []});
