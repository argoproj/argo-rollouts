import {Header} from './components/header/header';
import {createBrowserHistory} from 'history';
import * as React from 'react';
import {KeybindingProvider} from 'react-keyhooks';
import {Route, Router, Switch} from 'react-router-dom';
import './App.scss';
import {NamespaceContext, RolloutAPI} from './shared/context/api';
import {Modal} from './components/modal/modal';
import {Rollout} from './components/rollout/rollout';
import {RolloutsList} from './components/rollouts-list/rollouts-list';
import {Shortcut, Shortcuts} from './components/shortcuts/shortcuts';
import {ConfigProvider} from 'antd';
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
                                    setShowShortcuts(true);
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

const App = () => {
    const [namespace, setNamespace] = React.useState(init);
    const [availableNamespaces, setAvailableNamespaces] = React.useState([]);
    React.useEffect(() => {
        try {
            RolloutAPI.rolloutServiceGetNamespace()
                .then((info) => {
                    if (!info) {
                        throw new Error();
                    }
                    if (!namespace) {
                        setNamespace(info.namespace);
                    }
                    setAvailableNamespaces(info.availableNamespaces);
                })
                .catch((e) => {
                    setAvailableNamespaces([namespace]);
                });
        } catch (e) {
            setAvailableNamespaces([namespace]);
        }
    }, []);
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
                                component={<RolloutsList />}
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

export default App;
