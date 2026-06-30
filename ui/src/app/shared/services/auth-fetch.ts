// makeAuthFetch returns a fetch wrapper that sends cookies with every request
// and calls onUnauthorized() when the server responds 401, so the app can
// redirect to the login page. The session cookie is HttpOnly; the browser
// attaches it automatically because of credentials: 'include'.
export type Unauthorized = () => void;

export function makeAuthFetch(baseFetch: typeof fetch, onUnauthorized: Unauthorized): (url: string, init?: any) => Promise<Response> {
    return async (url: string, init?: any): Promise<Response> => {
        const response = await baseFetch(url, {...(init || {}), credentials: 'include'});
        if (response.status === 401) {
            onUnauthorized();
        }
        return response;
    };
}

// shouldRedirectToLogin returns false when the browser is already on the login
// page, to avoid an infinite reload loop when a bootstrap API call 401s there.
export function shouldRedirectToLogin(pathname: string): boolean {
    return !pathname.replace(/\/+$/, '').endsWith('/login');
}
