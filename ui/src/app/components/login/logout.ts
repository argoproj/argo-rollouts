// logout clears the session by calling the backend, then navigates to login.
export async function logout(base: string, redirect: (url: string) => void): Promise<void> {
    try {
        await fetch(`${base}/api/logout`, {method: 'POST', credentials: 'include'});
    } finally {
        redirect(`${base}/login`);
    }
}
