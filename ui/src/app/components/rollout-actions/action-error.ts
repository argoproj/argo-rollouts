export interface ActionErrorNotification {
    message: string;
    description: string;
}

const unexpectedErrorDescription = 'An unexpected error occurred. Please try again.';

// The generated API client rejects with the fetch Response when the server responds with a
// non-2xx status, and with an Error when the request itself fails.
const isResponse = (error: any): error is Response => typeof error?.status === 'number' && typeof error?.json === 'function';

const asString = (value: any): string => (typeof value === 'string' ? value : '');

// Error bodies from the API server have the form {"error": "...", "code": 5, "message": "..."}.
const getResponseMessage = async (response: Response): Promise<string> => {
    try {
        const body = await response.json();
        return asString(body?.message) || asString(body?.error);
    } catch {
        return '';
    }
};

export const getActionErrorNotification = async (error: any, actionName: string): Promise<ActionErrorNotification> => {
    const action = actionName.toLowerCase();
    if (isResponse(error)) {
        if (error.status === 403) {
            return {
                message: 'Permission Denied',
                description: `You don't have permission to ${action} this rollout. Please check your RBAC permissions.`,
            };
        }
        return {message: `Failed to ${action} rollout`, description: (await getResponseMessage(error)) || unexpectedErrorDescription};
    }
    return {message: `Failed to ${action} rollout`, description: asString(error?.message) || unexpectedErrorDescription};
};
