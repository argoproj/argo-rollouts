// describeApiError turns whatever the generated API client rejected with into a message that is
// worth showing a user. The generated client rejects with the raw Response, which has no `message`,
// so without this every failure renders as "undefined".
export const describeApiError = async (e: any): Promise<string> => {
    if (e instanceof Response || typeof e?.status === 'number') {
        let body = '';
        try {
            body = (await e.clone().text()).trim();
        } catch {
            body = '';
        }
        try {
            const parsed = JSON.parse(body);
            body = parsed.message || parsed.error || body;
        } catch {
            // body was not JSON, use it as-is
        }
        const status = `${e.status}${e.statusText ? ` ${e.statusText}` : ''}`;
        return body ? `${status}: ${body}` : status;
    }
    return e?.message || 'An unexpected error occurred.';
};
