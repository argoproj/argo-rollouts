import {ThemeDiv, ThemeProvider} from 'argo-ui/v2';
import {Header} from './components/header/header';
import {createBrowserHistory} from 'history';
import * as React from 'react';
import {Key, KeybindingContext, KeybindingProvider} from 'react-keyhooks';
import {Redirect, Route, Router, Switch} from 'react-router-dom';
import './App.scss';
import {NamespaceContext, RolloutAPI} from './shared/context/api';
import {Modal} from './components/modal/modal';
import {Rollout} from './components/rollout/rollout';
import {RolloutsList} from './components/rollouts-list/rollouts-list';
import {Shortcut, Shortcuts} from './components/shortcuts/shortcuts';

const bases = document.getElementsByTagName('base');
const base = bases.length > 0 ? bases[0].getAttribute('href') || '/' : '/';
export const history = createBrowserHistory({basename: base});

const Page = (props: {path: string; component: React.ReactNode; exact?: boolean; shortcuts?: Shortcut[]; changeNamespace: (val: string) => void}) => {
    const {useKeybinding} = React.useContext(KeybindingContext);
    const [showShortcuts, setShowShortcuts] = React.useState(false);
    useKeybinding(
        [Key.SHIFT, Key.H],
        () => {
            if (props.shortcuts) {
                setShowShortcuts(!showShortcuts);
            }
            return false;
        },
        true
    );
    return (
        <ThemeDiv className='rollouts'>
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
        </ThemeDiv>
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
        <ThemeProvider>
            {namespace && (
                <NamespaceContext.Provider value={{namespace, availableNamespaces}}>
                    <KeybindingProvider>
                        <Router history={history}>
                            <Switch>
                                <Redirect exact={true} path='/' to='/rollouts' />

                                <Page
                                    exact
                                    path='/rollouts'
                                    component={<RolloutsList />}
                                    shortcuts={[
                                        {key: '/', description: 'Search'},
                                        {key: 'TAB', description: 'Search, navigate search items'},
                                        {key: ['fa-arrow-left', 'fa-arrow-right', 'fa-arrow-up', 'fa-arrow-down'], description: 'Navigate rollouts list', icon: true},
                                        {key: ['SHIFT', 'H'], description: 'Show help menu', combo: true},
                                    ]}
                                    changeNamespace={changeNamespace}
                                />
                                <Page path='/rollout/:name' component={<Rollout />} changeNamespace={changeNamespace} />

                                <Redirect path='*' to='/' />
                            </Switch>
                        </Router>
                    </KeybindingProvider>
                </NamespaceContext.Provider>
            )}
        </ThemeProvider>
    );
};

export default App;
