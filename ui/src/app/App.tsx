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
import {Key, useKeyListener} from 'react-keyhooks';
<<<<<<< HEAD
import {Shortcut, Shortcuts} from './components/shortcuts/shortcuts';
import {Modal} from './components/modal/modal';
import {faArrowDown, faArrowLeft, faArrowRight, faArrowUp} from '@fortawesome/free-solid-svg-icons';
=======
>>>>>>> master

const bases = document.getElementsByTagName('base');
const base = bases.length > 0 ? bases[0].getAttribute('href') || '/' : '/';
export const history = createBrowserHistory({basename: base});

<<<<<<< HEAD
const Page = (props: {path: string; component: React.ReactNode; exact?: boolean; shortcuts?: Shortcut[]}) => {
    const useKeyPress = useKeyListener();
    const [showShortcuts, setShowShortcuts] = React.useState(false);
    useKeyPress(
        [Key.SHIFT, Key.H],
        () => {
            setShowShortcuts(!showShortcuts);
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
=======
const Page = (props: {path: string; component: React.ReactNode; exact?: boolean}) => {
    return (
        <ThemeDiv className='rollouts'>
>>>>>>> master
            <Route path={props.path} exact={props.exact}>
                <React.Fragment>
                    <Header />
                    {props.component}
                </React.Fragment>
            </Route>
        </ThemeDiv>
    );
};

const App = () => {
<<<<<<< HEAD
=======
    const useKeyPress = useKeyListener();
    useKeyPress(Key.Q, () => {
        // TODO: Provide help menu for keyboard shortcuts
        return false;
    });
>>>>>>> master
    return (
        <ThemeProvider>
            <NamespaceProvider>
                <Router history={history}>
                    <Switch>
                        <Redirect exact={true} path='/' to='/rollouts' />

<<<<<<< HEAD
                        <Page
                            exact
                            path='/rollouts'
                            component={<RolloutsList />}
                            shortcuts={[
                                {key: '/', description: 'Search'},
                                {key: 'TAB', description: 'Search, navigate search items'},
                                {key: [faArrowLeft, faArrowRight, faArrowUp, faArrowDown], description: 'Navigate rollouts list'},
                            ]}
                        />
=======
                        <Page exact path='/rollouts' component={<RolloutsList />} />
>>>>>>> master
                        <Page path='/rollout/:name' component={<Rollout />} />

                        <Redirect path='*' to='/' />
                    </Switch>
                </Router>
            </NamespaceProvider>
        </ThemeProvider>
    );
};

export default App;
