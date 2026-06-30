/** @jest-environment jsdom */
import * as React from 'react';
import {render, screen, fireEvent, waitFor} from '@testing-library/react';
import {LoginPage} from './login';

// AntD v5 uses window.matchMedia for responsive breakpoints; jsdom lacks it.
beforeAll(() => {
    Object.defineProperty(window, 'matchMedia', {
        writable: true,
        value: jest.fn().mockImplementation((query: string) => ({
            matches: false,
            media: query,
            onchange: null,
            addListener: jest.fn(),
            removeListener: jest.fn(),
            addEventListener: jest.fn(),
            removeEventListener: jest.fn(),
            dispatchEvent: jest.fn(),
        })),
    });
});

function mockFetch(status: number) {
    return jest.fn().mockResolvedValue({ok: status >= 200 && status < 300, status} as Response);
}

afterEach(() => {
    jest.restoreAllMocks();
});

test('renders username, password, and SSO link', () => {
    render(<LoginPage />);
    expect(screen.getByLabelText(/username/i)).toBeTruthy();
    expect(screen.getByLabelText(/password/i)).toBeTruthy();
    const sso = screen.getByText(/sso/i).closest('a');
    expect(sso).toBeTruthy();
    expect(sso!.getAttribute('href')).toContain('/auth/login');
});

test('successful login posts credentials and calls onSuccess', async () => {
    const f = mockFetch(200);
    (global as any).fetch = f;
    const onSuccess = jest.fn();
    render(<LoginPage onSuccess={onSuccess} />);

    fireEvent.change(screen.getByLabelText(/username/i), {target: {value: 'alice'}});
    fireEvent.change(screen.getByLabelText(/password/i), {target: {value: 's3cret'}});
    fireEvent.click(screen.getByRole('button', {name: /log in/i}));

    await waitFor(() => expect(onSuccess).toHaveBeenCalled());
    const [url, init] = f.mock.calls[0];
    expect(url).toContain('/api/login');
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('include');
    expect(JSON.parse(init.body)).toEqual({username: 'alice', password: 's3cret'});
});

test('failed login shows an error and does not call onSuccess', async () => {
    (global as any).fetch = mockFetch(401);
    const onSuccess = jest.fn();
    render(<LoginPage onSuccess={onSuccess} />);

    fireEvent.change(screen.getByLabelText(/username/i), {target: {value: 'x'}});
    fireEvent.change(screen.getByLabelText(/password/i), {target: {value: 'y'}});
    fireEvent.click(screen.getByRole('button', {name: /log in/i}));

    await waitFor(() => expect(screen.getByText(/invalid username or password/i)).toBeTruthy());
    expect(onSuccess).not.toHaveBeenCalled();
});
