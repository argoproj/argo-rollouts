import {Header} from './components/header/header';
import {LoginPage} from './components/login/login';
import {createBrowserHistory} from 'history';
import * as React from 'react';
import {KeybindingProvider} from 'react-keyhooks';
import {Route, Router, Switch} from 'react-router-dom';
import './App.scss';
import {NamespaceContext, AuthAwareAPIProvider, RolloutAPIContext} from './shared/context/api';
import {AuthContext, AuthProvider} from './shared/context/auth';
import {Modal} from './components/modal/modal';
import {Rollout} from './components/rollout/rollout';
import {RolloutsHome} from './components/rollouts-home/rollouts-home';
import {Shortcut, Shortcuts} from './components/shortcuts/shortcuts';
import {ConfigProvider, notification} from 'antd';
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

const AppContent = () => {
    const [namespace, setNamespace] = React.useState(init);
    const [availableNamespaces, setAvailableNamespaces] = React.useState([]);
    const {token, authRequired, setAuthRequired} = React.useContext(AuthContext);
    const api = React.useContext(RolloutAPIContext);

    React.useEffect(() => {
        try {
            api.rolloutServiceGetNamespace()
                .then((info) => {
                    if (!info) {
                        throw new Error();
                    }
                    if (!namespace) {
                        setNamespace(info.namespace);
                    }
                    setAvailableNamespaces(info.availableNamespaces);
                    setAuthRequired(false);
                })
                .catch((e) => {
                    if (e?.status === 401) {
                        setAuthRequired(true);
                        return;
                    }
                    setAvailableNamespaces([namespace]);
                });
        } catch (e) {
            setAvailableNamespaces([namespace]);
            console.error('Error fetching namespaces:', e);
            notification.error({
                message: 'Error fetching namespaces',
                description: e.message || 'An unexpected error occurred while fetching namespaces.',
                duration: 8,
                placement: 'bottomRight',
            });
        }
    }, [token]);

    if (authRequired) {
        return (
            <ConfigProvider theme={theme}>
                <LoginPage />
            </ConfigProvider>
        );
    }

    const changeNamespace = (val: string) => {
        setNamespace(val);
        window.localStorage.setItem(NAMESPACE_KEY, val);
    };

    return (
        namespace && (
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
        )
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
