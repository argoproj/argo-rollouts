import {Header} from './components/header/header';
import {Login} from './components/login/login';
import {createBrowserHistory} from 'history';
import * as React from 'react';
import {KeybindingProvider} from 'react-keyhooks';
import {Route, Router, Switch} from 'react-router-dom';
import './App.scss';
import {NamespaceContext, RolloutAPIContext} from './shared/context/api';
import {AuthAwareAPIProvider} from './shared/context/api';
import {AuthContext, AuthProvider, isUnauthorized} from './shared/context/auth';
import {describeApiError} from './shared/utils/api-error';
import {Modal} from './components/modal/modal';
import {Rollout} from './components/rollout/rollout';
import {RolloutsHome} from './components/rollouts-home/rollouts-home';
import {Shortcut, Shortcuts} from './components/shortcuts/shortcuts';
import {Button, ConfigProvider, Result, Spin} from 'antd';
import {theme} from '../config/theme';

const bases = document.getElementsByTagName('base');
const base = bases.length > 0 ? bases[0].getAttribute('href') || '/' : '/';
export const history = createBrowserHistory({basename: base});

const Page = (props: {path: string; component: React.ReactNode; exact?: boolean; shortcuts?: Shortcut[]; changeNamespace: (val: string) => void}) => {
    const [showShortcuts, setShowShortcuts] = React.useState(false);
    return (
        <ConfigProvider theme={theme}>
            <div className='rollouts'>
                {showShortcuts && (
                    <Modal hide={() => setShowShortcuts(false)}>
                        <Shortcuts shortcuts={props.shortcuts} />
                    </Modal>
                )}
                <Route path={props.path} exact={props.exact}>
                    <React.Fragment>
                        <Header
                            changeNamespace={props.changeNamespace}
                            pageHasShortcuts={!!props.shortcuts}
                            showHelp={() => {
                                if (props.shortcuts) {
                                    setShowShortcuts(!showShortcuts);
                                }
                            }}
                            hideHelp={() => {
                                if (props.shortcuts) {
                                    setShowShortcuts(false);
                                }
                            }}
                        />
                        {props.component}
                    </React.Fragment>
                </Route>
            </div>
        </ConfigProvider>
    );
};

export const NAMESPACE_KEY = 'namespace';
const init = window.localStorage.getItem(NAMESPACE_KEY);

type LoadState = 'loading' | 'ready' | 'unauthenticated' | 'error';

const AppContent = () => {
    const {token} = React.useContext(AuthContext);
    // the API client from context carries the bearer token; the bare RolloutAPI singleton does not
    const api = React.useContext(RolloutAPIContext);
    const [namespace, setNamespace] = React.useState(init);
    const [availableNamespaces, setAvailableNamespaces] = React.useState([]);
    const [state, setState] = React.useState<LoadState>('loading');
    const [errorMessage, setErrorMessage] = React.useState<string>(null);
    const [retryCount, setRetryCount] = React.useState(0);

    React.useEffect(() => {
        let cancelled = false;
        setState('loading');
        setErrorMessage(null);

        api.rolloutServiceGetNamespace()
            .then((info) => {
                if (cancelled) {
                    return;
                }
                if (!info) {
                    throw new Error('The server returned an empty namespace response.');
                }
                setNamespace((current) => current || info.namespace);
                setAvailableNamespaces(info.availableNamespaces || []);
                setState('ready');
            })
            .catch(async (e) => {
                const message = await describeApiError(e);
                if (cancelled) {
                    return;
                }
                if (isUnauthorized(e)) {
                    // a token that was present and still got a 401 was rejected by Kubernetes
                    setErrorMessage(token ? 'The token was rejected: it is invalid, expired, or not accepted by the Kubernetes API server.' : null);
                    setState('unauthenticated');
                    return;
                }
                console.error('Error fetching namespaces:', e);
                setErrorMessage(message);
                setState('error');
            });

        return () => {
            cancelled = true;
        };
    }, [api, token, retryCount]);

    const changeNamespace = (val: string) => {
        setNamespace(val);
        window.localStorage.setItem(NAMESPACE_KEY, val);
    };

    if (state === 'unauthenticated') {
        return <Login error={errorMessage} />;
    }

    if (state === 'loading') {
        return (
            <div className='app-status'>
                <Spin size='large' />
            </div>
        );
    }

    // never render nothing: a blank page is indistinguishable from a hung dashboard
    if (state === 'error' || !namespace) {
        return (
            <div className='app-status'>
                <Result
                    status='error'
                    title='Could not load the dashboard'
                    subTitle={errorMessage || 'The server did not report a namespace to display.'}
                    extra={
                        <Button type='primary' onClick={() => setRetryCount((c) => c + 1)}>
                            Retry
                        </Button>
                    }
                />
            </div>
        );
    }

    return (
        <NamespaceContext.Provider value={{namespace, availableNamespaces}}>
            <KeybindingProvider>
                <Router history={history}>
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
                </Router>
            </KeybindingProvider>
        </NamespaceContext.Provider>
    );
};

const App = () => {
    return (
        <AuthProvider>
            <AuthAwareAPIProvider>
                <AppContent />
            </AuthAwareAPIProvider>
        </AuthProvider>
    );
};

export default App;
