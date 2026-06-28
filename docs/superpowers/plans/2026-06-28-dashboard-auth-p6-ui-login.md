# Dashboard Auth — Plan 6: UI Login Page Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the dashboard UI a login experience: an authenticating fetch wrapper that sends the session cookie and redirects to `/login` on `401`, a `LoginPage` (local username/password form + an "SSO login" link), a `/login` route reachable when unauthenticated, and a logout button — so a server-mode dashboard is usable from a browser.

**Architecture:** Four units in `ui/`. `auth-fetch.ts` provides `makeAuthFetch(baseFetch, onUnauthorized)` — a `FetchAPI` that adds `credentials: 'include'` and invokes `onUnauthorized` on a 401; it is wired into the generated `RolloutServiceApi` in `shared/context/api.tsx`. A jest-infra task adds `jest-environment-jsdom` + an SCSS module-mapper so React component tests run (there are none today). `LoginPage` is an Ant Design form posting to `/api/login` with a fallback "Login with SSO" link to `/auth/login`. `App.tsx` is restructured so the `/login` route renders OUTSIDE the existing `namespace &&` gate (an unauthenticated user has no namespace, so today's gate would hide the login page), and the header gets a logout button. The session token is an HttpOnly cookie the browser sends automatically — the UI never reads or stores it.

**Tech Stack:** React 17, TypeScript 5, react-router-dom 5.2, Ant Design v5, webpack dev server, jest 29 + ts-jest + @testing-library/react 11. Package manager: pnpm via `corepack pnpm` (pnpm is not on PATH; `corepack` is). Run jest/tsc via `ui/node_modules/.bin/jest` and `ui/node_modules/.bin/tsc`.

## Global Constraints

- All work is under `ui/`. Commands run from `ui/`. Use `node_modules/.bin/jest` and `node_modules/.bin/tsc`; install deps with `corepack pnpm add -D ...`.
- The session token is an HttpOnly cookie set by the backend (`argorollouts.token`). The UI MUST NOT read, store, or send it manually — it relies on `credentials: 'include'` so the browser attaches it. No token in localStorage.
- Base path: the app's base comes from the `<base href>` tag (`getApiBasePath()` in `api.tsx` strips the trailing slash). Login POST target, SSO link, and post-login redirect are all base-path-relative.
- `/api/login` and `/api/logout` are JSON HTTP endpoints (Plan 4b). `/auth/login` is a backend redirect endpoint (Plan 5) — the SSO link is a plain anchor/navigation to it, NOT a fetch.
- Backward compatibility: with `--auth-mode=none` the backend never returns 401 and never serves `/login` is harmless (the route just exists); the existing pages and behavior are unchanged. Do not alter rollout-list/detail behavior.
- Follow existing conventions: Ant Design components, co-located `*.scss`, theme colors from `config/theme.ts` (`#44505f`). Component tests use `@testing-library/react`.
- Component test files that render React need jsdom: add `/** @jest-environment jsdom */` at the top of those test files (Task 2 makes this work).

---

### Task 1: Authenticating fetch wrapper

**Files:**
- Create: `ui/src/app/shared/services/auth-fetch.ts`
- Create: `ui/src/app/shared/services/auth-fetch.test.ts`
- Modify: `ui/src/app/shared/context/api.tsx`

**Interfaces:**
- Produces:
  - `type Unauthorized = () => void`
  - `function makeAuthFetch(baseFetch: typeof fetch, onUnauthorized: Unauthorized): (url: string, init?: any) => Promise<Response>` — calls `baseFetch(url, {...init, credentials: 'include'})`; if the resolved response has `status === 401`, calls `onUnauthorized()` before returning the response.

- [ ] **Step 1: Write the failing test** (`auth-fetch.test.ts`, runs in default node env — no DOM)

```ts
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `node_modules/.bin/jest src/app/shared/services/auth-fetch.test.ts`
Expected: FAIL — cannot find module `./auth-fetch`.

- [ ] **Step 3: Write minimal implementation** (`auth-fetch.ts`)

```ts
// makeAuthFetch returns a fetch wrapper that sends cookies with every request
// and calls onUnauthorized() when the server responds 401, so the app can
// redirect to the login page. The session cookie is HttpOnly; the browser
// attaches it automatically because of credentials: 'include'.
export type Unauthorized = () => void;

export function makeAuthFetch(baseFetch: typeof fetch, onUnauthorized: Unauthorized): (url: string, init?: any) => Promise<Response> {
    return async (url: string, init?: any): Promise<Response> => {
        const response = await baseFetch(url, {...(init || {}), credentials: 'include'});
        if (response.status === 401) {
            onUnauthorized();
        }
        return response;
    };
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `node_modules/.bin/jest src/app/shared/services/auth-fetch.test.ts`
Expected: PASS (3 tests).

- [ ] **Step 5: Wire into `api.tsx`**

Modify `ui/src/app/shared/context/api.tsx` — pass an auth fetch (with a redirect-to-login callback) as the 3rd constructor arg of `RolloutServiceApi`. Replace the `RolloutAPI` line:

```tsx
import {makeAuthFetch} from '../services/auth-fetch';

const basePath = getApiBasePath();

// Redirect to the login page on 401. A full navigation re-bootstraps the app
// once the user has authenticated.
const redirectToLogin = () => {
    window.location.assign(`${basePath}/login`);
};

const authFetch = makeAuthFetch(window.fetch.bind(window), redirectToLogin);

export const RolloutAPI = new RolloutServiceApi(new Configuration({basePath}), basePath, authFetch);
```

- [ ] **Step 6: Typecheck + commit**

Run: `node_modules/.bin/tsc --noEmit -p tsconfig.json`
Expected: no errors.

```bash
git add ui/src/app/shared/services/auth-fetch.ts ui/src/app/shared/services/auth-fetch.test.ts ui/src/app/shared/context/api.tsx
git commit -m "feat(ui): authenticating fetch with 401 redirect to login"
```

---

### Task 2: Jest infrastructure for React component tests

**Files:**
- Modify: `ui/jest.config.js`
- Modify: `ui/package.json` (devDeps, via the install command)
- Create: `ui/src/app/shared/services/jsdom-smoke.test.tsx` (proves the infra, then kept as a guard)

**Interfaces:** none (build/test infra only).

- [ ] **Step 1: Add the dev dependencies**

Run: `corepack pnpm add -D jest-environment-jsdom@^29 identity-obj-proxy@^3`
Expected: both added to `package.json` devDependencies; lockfile updated.

- [ ] **Step 2: Write a failing jsdom smoke test** (`jsdom-smoke.test.tsx`)

```tsx
/** @jest-environment jsdom */
import * as React from 'react';
import {render, screen} from '@testing-library/react';

// Importing a stylesheet must not break the test (proves the scss moduleNameMapper).
import './auth-fetch'; // a real module in this dir; ensures resolution works

test('jsdom renders a React element', () => {
    render(<div>hello-jsdom</div>);
    expect(screen.getByText('hello-jsdom')).toBeTruthy();
});
```

- [ ] **Step 3: Run test to verify it fails**

Run: `node_modules/.bin/jest src/app/shared/services/jsdom-smoke.test.tsx`
Expected: FAIL — until `jest.config.js` maps scss and jsdom is available; without the docblock support / mapper it errors. (If it already passes after Step 1, proceed — the docblock supplies jsdom; the mapper is still needed for Task 3's scss import.)

- [ ] **Step 4: Configure jest** (`jest.config.js`)

```js
module.exports = {
    roots: ['<rootDir>/src'],
    testMatch: ['**/?(*.)+(spec|test).+(ts|tsx|js)'],
    transform: {
        '^.+\\.(ts|tsx)$': 'ts-jest',
    },
    moduleNameMapper: {
        '\\.(scss|sass|css)$': 'identity-obj-proxy',
    },
    modulePathIgnorePatterns: ['generated'],
};
```

(`testEnvironment` stays the default `node`; component test files opt into jsdom with the `/** @jest-environment jsdom */` docblock, so non-DOM tests like Task 1's keep the faster node env.)

- [ ] **Step 5: Run test to verify it passes**

Run: `node_modules/.bin/jest src/app/shared/services/jsdom-smoke.test.tsx`
Expected: PASS (1 test). Also confirm the Task 1 node test still passes: `node_modules/.bin/jest src/app/shared/services/auth-fetch.test.ts`.

- [ ] **Step 6: Commit**

```bash
git add ui/jest.config.js ui/package.json ui/pnpm-lock.yaml ui/src/app/shared/services/jsdom-smoke.test.tsx
git commit -m "test(ui): enable jsdom + scss mapper for component tests"
```

---

### Task 3: LoginPage component

**Files:**
- Create: `ui/src/app/components/login/login.tsx`
- Create: `ui/src/app/components/login/login.scss`
- Create: `ui/src/app/components/login/login.test.tsx`

**Interfaces:**
- Consumes: `getApiBasePath` from `shared/context/api`.
- Produces:
  - `interface LoginPageProps { onSuccess?: () => void }` (default `onSuccess` reloads to the base path so the authenticated app re-bootstraps).
  - `const LoginPage: React.FC<LoginPageProps>` — an Ant Design form (username, password, submit) posting JSON to `${base}/api/login` with `credentials: 'include'`; on success calls `onSuccess`; on failure shows "Invalid username or password"; includes an `<a href="${base}/auth/login">` "Login with SSO" link.

- [ ] **Step 1: Write the failing test** (`login.test.tsx`)

```tsx
/** @jest-environment jsdom */
import * as React from 'react';
import {render, screen, fireEvent, waitFor} from '@testing-library/react';
import {LoginPage} from './login';

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `node_modules/.bin/jest src/app/components/login/login.test.tsx`
Expected: FAIL — cannot find module `./login`.

- [ ] **Step 3: Write minimal implementation** (`login.tsx` + a minimal `login.scss`)

`login.scss`:

```scss
.rollouts-login {
    display: flex;
    justify-content: center;
    align-items: center;
    min-height: 100vh;
}

.rollouts-login__card {
    width: 320px;
}

.rollouts-login__sso {
    display: block;
    margin-top: 12px;
    text-align: center;
}
```

`login.tsx`:

```tsx
import * as React from 'react';
import {Button, Form, Input, Alert} from 'antd';
import {getApiBasePath} from '../../shared/context/api';
import './login.scss';

export interface LoginPageProps {
    onSuccess?: () => void;
}

export const LoginPage: React.FC<LoginPageProps> = ({onSuccess}) => {
    const base = getApiBasePath();
    const [error, setError] = React.useState<string | null>(null);
    const [submitting, setSubmitting] = React.useState(false);

    const handleFinish = async (values: {username: string; password: string}) => {
        setSubmitting(true);
        setError(null);
        try {
            const res = await fetch(`${base}/api/login`, {
                method: 'POST',
                credentials: 'include',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({username: values.username, password: values.password}),
            });
            if (!res.ok) {
                setError('Invalid username or password');
                return;
            }
            if (onSuccess) {
                onSuccess();
            } else {
                window.location.assign(`${base}/`);
            }
        } catch (e) {
            setError('Invalid username or password');
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <div className='rollouts-login'>
            <div className='rollouts-login__card'>
                {error && <Alert type='error' message={error} style={{marginBottom: 12}} />}
                <Form layout='vertical' onFinish={handleFinish}>
                    <Form.Item label='Username' name='username' rules={[{required: true, message: 'Username is required'}]}>
                        <Input autoFocus />
                    </Form.Item>
                    <Form.Item label='Password' name='password' rules={[{required: true, message: 'Password is required'}]}>
                        <Input.Password />
                    </Form.Item>
                    <Form.Item>
                        <Button type='primary' htmlType='submit' block loading={submitting}>
                            Log in
                        </Button>
                    </Form.Item>
                </Form>
                <a className='rollouts-login__sso' href={`${base}/auth/login`}>
                    Login with SSO
                </a>
            </div>
        </div>
    );
};
```

Note on the test's `getByLabelText`: Ant Design associates the `<label>` with the input via the form item; if the matcher does not find it, the implementer may add `htmlFor`/`id` or use `getByPlaceholderText` — adjust the component (add `placeholder='Username'`/`placeholder='Password'`) and/or the test accessor so the queries resolve. Keep the test asserting the same behavior.

- [ ] **Step 4: Run test to verify it passes**

Run: `node_modules/.bin/jest src/app/components/login/login.test.tsx`
Expected: PASS (3 tests).

- [ ] **Step 5: Typecheck + commit**

Run: `node_modules/.bin/tsc --noEmit -p tsconfig.json`
Expected: no errors.

```bash
git add ui/src/app/components/login/
git commit -m "feat(ui): local login page with SSO link"
```

---

### Task 4: Login route, logout button, dev proxy

**Files:**
- Modify: `ui/src/app/App.tsx` (add `/login` route outside the `namespace &&` gate)
- Modify: `ui/src/app/components/header/header.tsx` (logout button)
- Modify: `ui/src/app/webpack.dev.js` (proxy `/api/login`, `/api/logout`, `/auth`)
- Create: `ui/src/app/components/login/logout.test.tsx`

**Interfaces:**
- Produces: a `logout()` helper (POST `${base}/api/logout` then redirect to `${base}/login`) used by the header button.

- [ ] **Step 1: Write the failing test** (`logout.test.tsx`) — test the logout helper in isolation

```tsx
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `node_modules/.bin/jest src/app/components/login/logout.test.tsx`
Expected: FAIL — cannot find module `./logout`.

- [ ] **Step 3: Implement the logout helper** (`ui/src/app/components/login/logout.ts`)

```ts
// logout clears the session by calling the backend, then navigates to login.
export async function logout(base: string, redirect: (url: string) => void): Promise<void> {
    try {
        await fetch(`${base}/api/logout`, {method: 'POST', credentials: 'include'});
    } finally {
        redirect(`${base}/login`);
    }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `node_modules/.bin/jest src/app/components/login/logout.test.tsx`
Expected: PASS (1 test).

- [ ] **Step 5: Add the `/login` route in `App.tsx`**

Restructure the `App` return so `/login` renders regardless of `namespace` (an unauthenticated user has no namespace). Import `LoginPage`, and render the Router unconditionally with `/login` as the first route:

```tsx
import {LoginPage} from './components/login/login';

// ... inside App(), replace the `return ( namespace && ( ... ) )` with:
    return (
        <Router history={history}>
            <Switch>
                <Route path='/login' exact>
                    <ConfigProvider theme={theme}>
                        <LoginPage />
                    </ConfigProvider>
                </Route>
                <Route>
                    {namespace ? (
                        <NamespaceContext.Provider value={{namespace, availableNamespaces}}>
                            <KeybindingProvider>
                                <Switch>
                                    <Page
                                        exact
                                        path='/:namespace?'
                                        component={<RolloutsHome />}
                                        shortcuts={[
                                            {key: '/', description: 'Search'},
                                            {key: 'TAB', description: 'Search, navigate search items'},
                                            {key: ['fa-arrow-left', 'fa-arrow-right', 'fa-arrow-up', 'fa-arrow-down'], description: 'Navigate rollouts list', icon: true},
                                            {key: ['SHIFT', 'H'], description: 'Show help menu', combo: true},
                                        ]}
                                        changeNamespace={changeNamespace}
                                    />
                                    <Page path='/rollout/:namespace?/:name' component={<Rollout />} changeNamespace={changeNamespace} />
                                </Switch>
                            </KeybindingProvider>
                        </NamespaceContext.Provider>
                    ) : null}
                </Route>
            </Switch>
        </Router>
    );
```

(Keep the `KeybindingProvider`/`NamespaceContext` wrapping the authenticated routes; only `/login` is lifted out of the namespace gate. The `Router` now always renders so the 401 redirect to `/login` resolves.)

- [ ] **Step 6: Add a logout button to the header**

In `ui/src/app/components/header/header.tsx`, add a logout control (an Ant Design `Button` or a menu item) that calls `logout(getApiBasePath(), (url) => window.location.assign(url))`. Match the header's existing styling; place it on the right. Import `logout` from `../login/logout` and `getApiBasePath` from `../../shared/context/api`. Keep it unobtrusive — the header already has namespace controls; add the button without disturbing them.

- [ ] **Step 7: Update the webpack dev proxy** (`ui/src/app/webpack.dev.js`)

Extend the existing `devServer.proxy` so the auth endpoints reach the backend in dev (currently only `/api/v1` is proxied):

```js
        proxy: {
            '/api/v1': {target: 'http://localhost:3100', ...},
            '/api/login': {target: 'http://localhost:3100', secure: false},
            '/api/logout': {target: 'http://localhost:3100', secure: false},
            '/auth': {target: 'http://localhost:3100', secure: false},
        },
```

(Match the existing entry's option style — reuse whatever flags the `/api/v1` entry already sets, e.g. `secure`, `changeOrigin`. Do not remove or alter the `/api/v1` entry.)

- [ ] **Step 8: Typecheck + full UI test run**

Run: `node_modules/.bin/tsc --noEmit -p tsconfig.json && node_modules/.bin/jest`
Expected: no type errors; all UI tests pass (auth-fetch, jsdom-smoke, login, logout).

- [ ] **Step 9: Commit**

```bash
git add ui/src/app/App.tsx ui/src/app/components/header/header.tsx ui/src/app/webpack.dev.js ui/src/app/components/login/logout.ts ui/src/app/components/login/logout.test.tsx
git commit -m "feat(ui): login route, logout button, dev proxy for auth endpoints"
```

---

## Self-Review

**Spec coverage (vs design §6 UI sub-phase):**
- Authenticating fetch (cookie + 401 redirect) → Task 1. ✅
- Login page (local form) → Task 3. ✅
- SSO login link (to `/auth/login`) → Task 3. ✅
- Logout → Task 4. ✅
- `/login` reachable when unauthenticated (outside the namespace gate) → Task 4. ✅
- Component test infrastructure (jsdom + scss mapper) → Task 2. ✅
- Current-user display ("logged in as X") → NOT here; the token is HttpOnly and there is no `/api/userinfo` endpoint yet. Deferred (a small backend endpoint + header display, future).

**Placeholder scan:** No TBD/TODO; each step has complete code. The two adjustable spots (AntD label query in Task 3, dev-proxy option style in Task 4) are explicitly flagged for the implementer to match the live behavior. ✅

**Type consistency:** `makeAuthFetch`, `LoginPage`/`LoginPageProps`, `logout`, `getApiBasePath` consistent across tasks; all paths are `${base}`-relative. ✅

**Security / UX notes:**
- The UI never reads or stores the session token — it is HttpOnly and rides on `credentials: 'include'`. No XSS-exfiltratable token. The login error message is generic ("Invalid username or password"), matching the backend's anti-enumeration response.
- The SSO link is a plain navigation to the backend `/auth/login` (a redirect endpoint), NOT a fetch — the browser follows the OIDC redirect chain.
- 401 from ANY API call routes to `/login` via the wrapper, so an expired session lands the user back at login.
- Backward compatible: in `none` mode the backend never 401s and `/login` is simply an unused route; existing pages are untouched.

**Carried forward:**
- `/api/userinfo` + "logged in as" + per-user UI affordances.
- Hiding the SSO link when OIDC is not configured (needs a small public `/api/settings` exposing `oidcEnabled`); currently the link always shows.
- E2E (Cypress/Playwright) of the real login→dashboard flow against a running server.
