import * as React from 'react';

// The token is kept in a cookie rather than local storage so that EventSource/SSE requests are
// authenticated too: EventSource cannot set an Authorization header, and putting the token in the
// query string leaks it into logs and browser history. This mirrors Argo Workflows
// (https://github.com/argoproj/argo-workflows/pull/2058).
export const AUTH_COOKIE = 'authorization';

// Scope the cookie to the dashboard's base path so a --root-path dashboard does not clobber the
// cookie of another app served from the same host.
const cookiePath = (): string => {
    const path = new URL(document.baseURI).pathname;
    return path === '' ? '/' : path;
};

export const getAuthToken = (): string | null => {
    const prefix = `${AUTH_COOKIE}=`;
    for (const cookie of document.cookie.split(';')) {
        const trimmed = cookie.trim();
        if (trimmed.startsWith(prefix)) {
            return decodeURIComponent(trimmed.substring(prefix.length)) || null;
        }
    }
    return null;
};

const writeAuthCookie = (token: string | null) => {
    const attrs = [`Path=${cookiePath()}`, 'SameSite=Strict'];
    if (window.location.protocol === 'https:') {
        attrs.push('Secure');
    }
    if (!token) {
        attrs.push('Expires=Thu, 01 Jan 1970 00:00:00 GMT');
    }
    // session cookie: the token is gone once the browser closes
    document.cookie = `${AUTH_COOKIE}=${token ? encodeURIComponent(token) : ''}; ${attrs.join('; ')}`;
};

interface AuthContextType {
    token: string | null;
    login: (token: string) => void;
    logout: () => void;
}

export const AuthContext = React.createContext<AuthContextType>({
    token: null,
    login: () => {},
    logout: () => {},
});

export const AuthProvider = (props: {children: React.ReactNode}) => {
    const [token, setTokenState] = React.useState<string | null>(getAuthToken());

    const setToken = React.useCallback((newToken: string | null) => {
        writeAuthCookie(newToken);
        setTokenState(newToken);
    }, []);

    const value = React.useMemo(
        () => ({
            token,
            login: (newToken: string) => setToken(newToken),
            logout: () => setToken(null),
        }),
        [token, setToken]
    );

    return <AuthContext.Provider value={value}>{props.children}</AuthContext.Provider>;
};

// createAuthFetch returns a fetch function that adds the Authorization header with the bearer
// token. The cookie alone would authenticate the request, but sending the header keeps the API
// usable from clients that do not carry cookies.
export const createAuthFetch = (token: string | null): typeof fetch => {
    return (input: RequestInfo | URL, init?: RequestInit) => {
        if (!token) {
            return fetch(input, init);
        }
        const headers = new Headers(init?.headers);
        headers.set('Authorization', `Bearer ${token}`);
        return fetch(input, {...init, headers});
    };
};

// isUnauthorized reports whether a rejected API call failed because the request was not
// authenticated. The generated client rejects with the raw Response.
export const isUnauthorized = (e: any): boolean => e?.status === 401;
