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
