export interface ActionErrorMessage {
    title: string;
    description: string;
}

// The generated API client throws the fetch Response itself on non-2xx status codes.
const isResponse = (error: any): error is Response => typeof error?.status === 'number' && typeof error?.text === 'function';

const responseMessage = async (response: Response): Promise<string> => {
    try {
        const body = (await response.text()).trim();
        try {
            return JSON.parse(body)?.message || body;
        } catch {
            return body;
        }
    } catch {
        return '';
    }
};

export const actionErrorMessage = async (error: any, actionName: string): Promise<ActionErrorMessage> => {
    const action = actionName.toLowerCase();
    const status = isResponse(error) ? error.status : error?.response?.status;

    if (status === 403) {
        return {
            title: 'Permission Denied',
            description: `You don't have permission to ${action} this rollout.`,
        };
    }

    const message = isResponse(error) ? await responseMessage(error) : error?.response?.data?.message || error?.message;
    return {
        title: `Failed to ${action} rollout`,
        description: message || 'An unexpected error occurred. Please try again.',
    };
};
