import {getActionErrorNotification} from './action-error';

const jsonResponse = (status: number, body: any) => new Response(JSON.stringify(body), {status, headers: {'Content-Type': 'application/json'}});

describe('Rollout action error notification', () => {
    it('reports missing permissions on a 403 response', async () => {
        const error = jsonResponse(403, {error: 'permission denied', code: 7, message: 'permission denied'});
        expect(await getActionErrorNotification(error, 'Promote')).toEqual({
            message: 'Permission Denied',
            description: "You don't have permission to promote this rollout. Please check your RBAC permissions.",
        });
    });

    it('shows the message from the response body', async () => {
        const error = jsonResponse(500, {error: 'rollouts.argoproj.io "example" not found', code: 5, message: 'rollouts.argoproj.io "example" not found'});
        expect(await getActionErrorNotification(error, 'Abort')).toEqual({
            message: 'Failed to abort rollout',
            description: 'rollouts.argoproj.io "example" not found',
        });
    });

    it('falls back to the error field of the response body', async () => {
        const error = jsonResponse(500, {error: 'something went wrong'});
        expect(await getActionErrorNotification(error, 'Retry')).toEqual({
            message: 'Failed to retry rollout',
            description: 'something went wrong',
        });
    });

    it('ignores message and error fields that are not strings', async () => {
        const error = jsonResponse(500, {error: {reason: 'nested'}, message: ['not', 'a', 'string']});
        expect(await getActionErrorNotification(error, 'Abort')).toEqual({
            message: 'Failed to abort rollout',
            description: 'An unexpected error occurred. Please try again.',
        });
    });

    it('falls back to a generic message when the response body is not JSON', async () => {
        const error = new Response('upstream connect error', {status: 502});
        expect(await getActionErrorNotification(error, 'Restart')).toEqual({
            message: 'Failed to restart rollout',
            description: 'An unexpected error occurred. Please try again.',
        });
    });

    it('shows the message of an error thrown before a response is received', async () => {
        expect(await getActionErrorNotification(new TypeError('Failed to fetch'), 'PromoteFull')).toEqual({
            message: 'Failed to promotefull rollout',
            description: 'Failed to fetch',
        });
    });

    it('falls back to a generic message for unknown errors', async () => {
        expect(await getActionErrorNotification(undefined, 'Promote')).toEqual({
            message: 'Failed to promote rollout',
            description: 'An unexpected error occurred. Please try again.',
        });
    });
});
