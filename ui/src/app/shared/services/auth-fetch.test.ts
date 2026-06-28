import {makeAuthFetch} from './auth-fetch';

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
