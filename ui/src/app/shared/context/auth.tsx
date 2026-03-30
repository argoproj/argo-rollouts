import * as React from 'react';

const TOKEN_KEY = 'auth_token';

export interface AuthContextType {
    token: string | null;
    setToken: (token: string | null) => void;
    logout: () => void;
    authRequired: boolean;
    setAuthRequired: (required: boolean) => void;
}

export const AuthContext = React.createContext<AuthContextType>({
    token: null,
    setToken: () => {},
    logout: () => {},
    authRequired: false,
    setAuthRequired: () => {},
});

export const AuthProvider = (props: {children: React.ReactNode}) => {
    const [token, setTokenState] = React.useState<string | null>(window.localStorage.getItem(TOKEN_KEY));
    const [authRequired, setAuthRequired] = React.useState(false);

    const setToken = React.useCallback((newToken: string | null) => {
        setTokenState(newToken);
        if (newToken) {
            window.localStorage.setItem(TOKEN_KEY, newToken);
        } else {
            window.localStorage.removeItem(TOKEN_KEY);
        }
    }, []);

    const logout = React.useCallback(() => {
        setToken(null);
        setAuthRequired(true);
    }, [setToken]);

    const contextValue = React.useMemo(
        () => ({token, setToken, logout, authRequired, setAuthRequired}),
        [token, setToken, logout, authRequired]
    );

    return (
        <AuthContext.Provider value={contextValue}>
            {props.children}
        </AuthContext.Provider>
    );
};

// createAuthFetch wraps the global fetch to add the Authorization header when a token is available
export const createAuthFetch = (token: string | null) => {
    return (url: string, init?: any): Promise<Response> => {
        if (token) {
            const headers = init?.headers instanceof Headers ? init.headers : new Headers(init?.headers);
            headers.set('Authorization', `Bearer ${token}`);
            init = {...init, headers};
        }
        return fetch(url, init);
    };
};

// appendTokenToUrl adds the token as a query parameter for EventSource URLs
// (EventSource does not support custom headers)
export const appendTokenToUrl = (url: string, token: string | null): string => {
    if (!token) {
        return url;
    }
    const separator = url.includes('?') ? '&' : '?';
    return `${url}${separator}token=${encodeURIComponent(token)}`;
};
