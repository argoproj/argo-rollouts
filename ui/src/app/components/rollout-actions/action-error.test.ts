import {actionErrorMessage} from './action-error';

const response = (status: number, body: string) => ({status, text: () => Promise.resolve(body)});

describe('actionErrorMessage', () => {
    it('reports a permission error when the API client throws a 403 response', async () => {
        expect(await actionErrorMessage(response(403, 'denied'), 'Promote')).toEqual({
            title: 'Permission Denied',
            description: "You don't have permission to promote this rollout.",
        });
    });

    it('uses the message of a JSON error body', async () => {
        const body = JSON.stringify({code: 5, message: 'rollouts.argoproj.io "demo" not found'});
        expect(await actionErrorMessage(response(500, body), 'Promote')).toEqual({
            title: 'Failed to promote rollout',
            description: 'rollouts.argoproj.io "demo" not found',
        });
    });

    it('uses a plain text error body', async () => {
        expect((await actionErrorMessage(response(502, 'bad gateway'), 'Abort')).description).toBe('bad gateway');
    });

    it('falls back to a generic message on an empty body', async () => {
        expect((await actionErrorMessage(response(500, ''), 'Retry')).description).toBe('An unexpected error occurred. Please try again.');
    });

    it('uses the message of a non-HTTP error', async () => {
        expect((await actionErrorMessage(new TypeError('Failed to fetch'), 'Restart')).description).toBe('Failed to fetch');
    });
});
