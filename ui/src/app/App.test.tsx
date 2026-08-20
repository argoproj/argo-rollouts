import * as React from 'react';
import {render, screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import App from './App';
import {AUTH_COOKIE} from './shared/context/auth';

// The route components open EventSource streams and draw charts; neither is part of the auth flow.
jest.mock('./components/rollouts-home/rollouts-home', () => ({
    RolloutsHome: () => <div>rollouts home</div>,
}));
jest.mock('./components/rollout/rollout', () => ({
    Rollout: () => <div>rollout</div>,
}));

const VALID_TOKEN = 'valid-token';

const json = (body: any) => new Response(JSON.stringify(body), {status: 200, headers: {'Content-Type': 'application/json'}});
const unauthorized = () => new Response('missing bearer token', {status: 401});

// A stand-in for a dashboard in client auth mode: every API route needs a token that Kubernetes
// accepts, and the UI must send one on the namespace call it makes at start-up.
const clientModeServer = (accepted: string | null) =>
    jest.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const auth = new Headers(init?.headers).get('Authorization');
        const token = auth?.startsWith('Bearer ') ? auth.substring(7) : null;
        if (!token) {
            return unauthorized();
        }
        if (accepted !== null && token !== accepted) {
            return new Response('the provided token was rejected by the Kubernetes API server', {status: 401});
        }
        if (String(input).endsWith('/api/v1/namespace')) {
            return json({namespace: 'default', availableNamespaces: ['default']});
        }
        return json({rolloutsVersion: 'v1.0.0'});
    });

const clearCookies = () => {
    document.cookie = `${AUTH_COOKIE}=; Path=/; Expires=Thu, 01 Jan 1970 00:00:00 GMT`;
};

describe('App client auth mode', () => {
    beforeEach(() => {
        clearCookies();
        window.localStorage.clear();
    });

    afterEach(() => {
        jest.restoreAllMocks();
        clearCookies();
    });

    it('shows the login page when the server requires a token', async () => {
        global.fetch = clientModeServer(VALID_TOKEN) as any;

        render(<App />);

        expect(await screen.findByRole('button', {name: 'Login'})).toBeTruthy();
    });

    it('loads the dashboard after a valid token is entered', async () => {
        global.fetch = clientModeServer(VALID_TOKEN) as any;

        render(<App />);
        await userEvent.type(await screen.findByLabelText('Bearer token'), VALID_TOKEN);
        await userEvent.click(screen.getByRole('button', {name: 'Login'}));

        // this is the regression: the namespace call has to carry the token, or the app renders
        // a blank page forever
        expect(await screen.findByText('rollouts home')).toBeTruthy();
        expect(document.cookie).toContain(`${AUTH_COOKIE}=${VALID_TOKEN}`);
    });

    it('reports an error and stays on the login page when the token is rejected', async () => {
        global.fetch = clientModeServer(VALID_TOKEN) as any;

        render(<App />);
        await userEvent.type(await screen.findByLabelText('Bearer token'), 'not-a-real-token');
        await userEvent.click(screen.getByRole('button', {name: 'Login'}));

        expect(await screen.findByText(/token was rejected/i)).toBeTruthy();
        expect(screen.getByRole('button', {name: 'Login'})).toBeTruthy();
    });

    it('returns to the login page when a stored token stops being accepted', async () => {
        document.cookie = `${AUTH_COOKIE}=expired-token; Path=/`;
        global.fetch = clientModeServer(VALID_TOKEN) as any;

        render(<App />);

        expect(await screen.findByText(/token was rejected/i)).toBeTruthy();
    });

    it('sends the token on every API request once logged in', async () => {
        const fetchMock = clientModeServer(VALID_TOKEN);
        document.cookie = `${AUTH_COOKIE}=${VALID_TOKEN}; Path=/`;
        global.fetch = fetchMock as any;

        render(<App />);
        await screen.findByText('rollouts home');

        expect(fetchMock).toHaveBeenCalled();
        for (const [, init] of fetchMock.mock.calls) {
            expect(new Headers(init?.headers).get('Authorization')).toBe(`Bearer ${VALID_TOKEN}`);
        }
    });

    it('logging out clears the cookie and returns to the login page', async () => {
        document.cookie = `${AUTH_COOKIE}=${VALID_TOKEN}; Path=/`;
        global.fetch = clientModeServer(VALID_TOKEN) as any;

        render(<App />);
        await screen.findByText('rollouts home');

        await userEvent.click(screen.getByRole('button', {name: /logout/i}));

        expect(await screen.findByRole('button', {name: 'Login'})).toBeTruthy();
        expect(document.cookie).not.toContain(VALID_TOKEN);
    });
});

describe('App server auth mode', () => {
    // the default mode is unauthenticated: no login page should ever appear
    it('loads the dashboard without a token', async () => {
        global.fetch = jest.fn(async (input: RequestInfo | URL) => {
            if (String(input).endsWith('/api/v1/namespace')) {
                return json({namespace: 'default', availableNamespaces: ['default']});
            }
            return json({rolloutsVersion: 'v1.0.0'});
        }) as any;

        render(<App />);

        expect(await screen.findByText('rollouts home')).toBeTruthy();
        expect(screen.queryByRole('button', {name: 'Login'})).toBeNull();
    });

    it('shows the error instead of a blank page when the namespace call fails', async () => {
        global.fetch = jest.fn(async () => new Response('connection refused', {status: 500})) as any;

        render(<App />);

        expect(await screen.findByText('Could not load the dashboard')).toBeTruthy();
        expect(screen.getByText(/connection refused/)).toBeTruthy();
    });
});
