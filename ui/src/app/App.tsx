import * as React from 'react';

import './App.scss';
import {Header} from './components/header/header';
import {RolloutsList} from './components/rollouts-list/rollouts-list';
import {Redirect, Route, Router, Switch} from 'react-router-dom';
import {Rollout} from './components/rollout/rollout';
import {createBrowserHistory} from 'history';
import {ThemeProvider} from './shared/context/theme';
import {ThemeDiv} from './components/theme-div/theme-div';
import {APIProvider} from './shared/context/api';

const bases = document.getElementsByTagName('base');
const base = bases.length > 0 ? bases[0].getAttribute('href') || '/' : '/';
export const history = createBrowserHistory({basename: base});

const Page = (props: {path: string; component: React.ReactNode; exact?: boolean}) => {
    return (
        <ThemeDiv className='rollouts'>
            <Route path={props.path} exact={props.exact}>
                <React.Fragment>
                    <Header />
                    {props.component}
                </React.Fragment>
            </Route>
        </ThemeDiv>
    );
};

const App = () => (
    <ThemeProvider>
        <APIProvider>
            <Router history={history}>
                <Switch>
                    <Redirect exact={true} path='/' to='/rollouts' />

                    <Page exact path='/rollouts' component={<RolloutsList />} />
                    <Page path='/rollout/:name' component={<Rollout />} />

                    <Redirect path='*' to='/' />
                </Switch>
            </Router>
        </APIProvider>
    </ThemeProvider>
);

export default App;
