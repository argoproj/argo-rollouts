import {makeAuthFetch, shouldRedirectToLogin} from './auth-fetch';

function resp(status: number): Response {
    return {status} as Response;
}

test('adds credentials: include to every request', async () => {
    const calls: any[] = [];
    const base = (async (url: string, init?: any) => {
        calls.push({url, init});
        return resp(200);
    }) as unknown as typeof fetch;

    const f = makeAuthFetch(base, () => undefined);
    await f('/api/x', {method: 'GET'});

    expect(calls[0].init.credentials).toBe('include');
    expect(calls[0].init.method).toBe('GET');
});

test('invokes onUnauthorized on 401', async () => {
    const base = (async () => resp(401)) as unknown as typeof fetch;
    let called = 0;
    const f = makeAuthFetch(base, () => {
        called++;
    });
    const r = await f('/api/x');
    expect(called).toBe(1);
    expect(r.status).toBe(401);
});

test('does not invoke onUnauthorized on success', async () => {
    const base = (async () => resp(200)) as unknown as typeof fetch;
    let called = 0;
    const f = makeAuthFetch(base, () => {
        called++;
    });
    await f('/api/x');
    expect(called).toBe(0);
});

describe('shouldRedirectToLogin', () => {
    test('returns false for /login', () => {
        expect(shouldRedirectToLogin('/login')).toBe(false);
    });
    test('returns false for foo/login', () => {
        expect(shouldRedirectToLogin('foo/login')).toBe(false);
    });
    test('returns false for foo/login/ (trailing slash stripped)', () => {
        expect(shouldRedirectToLogin('foo/login/')).toBe(false);
    });
    test('returns false for /rollouts/login', () => {
        expect(shouldRedirectToLogin('/rollouts/login')).toBe(false);
    });
    test('returns true for /rollouts', () => {
        expect(shouldRedirectToLogin('/rollouts')).toBe(true);
    });
    test('returns true for /rollouts/rollout/x', () => {
        expect(shouldRedirectToLogin('/rollouts/rollout/x')).toBe(true);
    });
    test('returns true for empty string', () => {
        expect(shouldRedirectToLogin('')).toBe(true);
    });
});
