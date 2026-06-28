import * as React from 'react';
import {RolloutNamespaceInfo, RolloutServiceApi, Configuration} from '../../../models/rollout/generated';
import {makeAuthFetch, shouldRedirectToLogin} from '../services/auth-fetch';

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

// Redirect to the login page on 401. A full navigation re-bootstraps the app
// once the user has authenticated.
const redirectToLogin = () => {
    if (!shouldRedirectToLogin(window.location.pathname)) return;
    window.location.assign(`${basePath}/login`);
};

const authFetch = makeAuthFetch(window.fetch.bind(window), redirectToLogin);

export const RolloutAPI = new RolloutServiceApi(new Configuration({basePath}), basePath, authFetch);
export const RolloutAPIContext = React.createContext(RolloutAPI);

export const APIProvider = (props: {children: React.ReactNode}) => {
    return <RolloutAPIContext.Provider value={RolloutAPI}>{props.children}</RolloutAPIContext.Provider>;
};

export const NamespaceContext = React.createContext<RolloutNamespaceInfo>({namespace: '', availableNamespaces: []});
