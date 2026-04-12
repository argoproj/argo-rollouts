import * as React from 'react';

const AUTH_TOKEN_KEY = 'auth_token';

interface AuthContextType {
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
    const [token, setTokenState] = React.useState<string | null>(localStorage.getItem(AUTH_TOKEN_KEY));
    const [authRequired, setAuthRequired] = React.useState(false);

    const setToken = (newToken: string | null) => {
        if (newToken) {
            localStorage.setItem(AUTH_TOKEN_KEY, newToken);
        } else {
            localStorage.removeItem(AUTH_TOKEN_KEY);
        }
        setTokenState(newToken);
    };

    const logout = () => {
        setToken(null);
        setAuthRequired(true);
    };

    return (
        <AuthContext.Provider value={{token, setToken, logout, authRequired, setAuthRequired}}>
            {props.children}
        </AuthContext.Provider>
    );
};

// createAuthFetch returns a fetch function that adds the Authorization header with the bearer token.
export const createAuthFetch = (token: string | null): typeof fetch => {
    return (input: RequestInfo | URL, init?: RequestInit) => {
        if (token) {
            const headers = new Headers(init?.headers);
            headers.set('Authorization', `Bearer ${token}`);
            return fetch(input, {...init, headers});
        }
        return fetch(input, init);
    };
};

// appendTokenToUrl adds the token as a query parameter for EventSource URLs
// (EventSource API does not support custom headers).
export const appendTokenToUrl = (url: string, token: string | null): string => {
    if (!token) {
        return url;
    }
    const separator = url.includes('?') ? '&' : '?';
    return `${url}${separator}token=${encodeURIComponent(token)}`;
};
