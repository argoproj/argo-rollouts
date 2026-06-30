import {logout} from './logout';

test('logout posts to /api/logout then redirects to login', async () => {
    const f = jest.fn().mockResolvedValue({ok: true, status: 200} as Response);
    (global as any).fetch = f;
    const redirect = jest.fn();

    await logout('/rollouts', redirect);

    const [url, init] = f.mock.calls[0];
    expect(url).toBe('/rollouts/api/logout');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('include');
    expect(redirect).toHaveBeenCalledWith('/rollouts/login');
});
