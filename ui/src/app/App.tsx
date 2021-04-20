import * as React from 'react';

import './App.scss';
import {Header} from './components/header/header';
import {RolloutsList} from './components/rollouts-list/rollouts-list';
import {Redirect, Route, Router, Switch} from 'react-router-dom';
import {Rollout} from './components/rollout/rollout';
import {createBrowserHistory} from 'history';
import {ThemeProvider} from './shared/context/theme';
import {ThemeDiv} from './components/theme-div/theme-div';
import {NamespaceProvider} from './shared/context/api';
import {Key} from 'react-keyhooks';
import {Shortcut, Shortcuts} from './components/shortcuts/shortcuts';
import {Modal} from './components/modal/modal';
import {faArrowDown, faArrowLeft, faArrowRight, faArrowUp} from '@fortawesome/free-solid-svg-icons';
import {KeybindingContext, KeybindingProvider} from 'react-keyhooks';

const bases = document.getElementsByTagName('base');
const base = bases.length > 0 ? bases[0].getAttribute('href') || '/' : '/';
export const history = createBrowserHistory({basename: base});

const Page = (props: {path: string; component: React.ReactNode; exact?: boolean; shortcuts?: Shortcut[]}) => {
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

const App = () => {
    return (
        <ThemeProvider>
            <NamespaceProvider>
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
                                    {key: [faArrowLeft, faArrowRight, faArrowUp, faArrowDown], description: 'Navigate rollouts list'},
                                    {key: ['SHIFT', 'H'], description: 'Show help menu', combo: true},
                                ]}
                            />
                            <Page path='/rollout/:name' component={<Rollout />} />

                            <Redirect path='*' to='/' />
                        </Switch>
                    </Router>
                </KeybindingProvider>
            </NamespaceProvider>
        </ThemeProvider>
    );
};

export default App;
