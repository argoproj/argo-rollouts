import {Header} from './components/header/header';
import {createBrowserHistory} from 'history';
import * as React from 'react';
import {KeybindingProvider} from 'react-keyhooks';
import {Redirect, Route, Router, Switch} from 'react-router-dom';
import './App.scss';
import {NamespaceContext, RolloutAPI} from './shared/context/api';
import {Modal} from './components/modal/modal';
import {Rollout} from './components/rollout/rollout';
import {RolloutsHome} from './components/rollouts-home/rollouts-home';
import {Shortcut, Shortcuts} from './components/shortcuts/shortcuts';
import {ConfigProvider} from 'antd';
import {theme} from '../config/theme';

import {initializeApp} from 'firebase/app';
import {getAuth, GoogleAuthProvider, signInWithPopup, onAuthStateChanged} from 'firebase/auth';

const emails = ['karan.parmar@pharmeasy.in', 'kavin@pharmeasy.in', 'rushi@pharmeasy.in', 'pushparaj@pharmeasy.in'];

// Your web app's Firebase configuration
const firebaseConfig = {
    apiKey: process.env.REACT_FIREBASE_API_KEY,
    authDomain: process.env.REACT_FIREBASE_AUTH_DOMAIN,
    projectId: process.env.REACT_FIREBASE_PROJECT_ID,
    storageBucket: process.env.REACT_FIREBASE_STORAGE_BUCKET,
    messagingSenderId: process.env.REACT_FIREBASE_MESSAGING_SENDER_ID,
    appId: process.env.REACT_FIREBASE_APP_ID,
};

// Initialize Firebase
const app = initializeApp(firebaseConfig);
const auth = getAuth(app);
const provider = new GoogleAuthProvider();

// Login Component
const Login = () => {
    const handleLogin = async () => {
        try {
            const result = await signInWithPopup(auth, provider);
            const user = result.user;
            const token = await user.getIdToken();
            // Store necessary data in localStorage
            if (emails.includes(user.email)) {
                localStorage.setItem('user', JSON.stringify({uid: user.uid, email: user.email, displayName: user.displayName}));
                localStorage.setItem('token', token);
                window.location.href = '/dashboard';
            } else {
                alert('User not allowed to login');
            }
        } catch (error) {
            console.error('Error during login:', error);
        }
    };

    return (
        <div className='login'>
            <button onClick={handleLogin}>Login with Google</button>
        </div>
    );
};

// // Protected Route Component
// @ts-ignore
const ProtectedRoute = ({component: Component, ...rest}) => {
    const [user, setUser] = React.useState(() => {
        const storedUser = localStorage.getItem('user');
        return storedUser ? JSON.parse(storedUser) : null;
    });

    React.useEffect(() => {
        const unsubscribe = onAuthStateChanged(auth, (currentUser) => {
            if (currentUser) {
                const {uid, email, displayName} = currentUser;
                const userData = {uid, email, displayName};
                localStorage.setItem('user', JSON.stringify(userData));
                setUser(userData);
            } else {
                localStorage.removeItem('user');
                setUser(null);
            }
        });
        return () => unsubscribe();
    }, []);

    return <Route {...rest} render={(props) => (user ? <Component {...props} /> : <Redirect to='/login' />)} />;
};

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
                                component={<RolloutsHome />}
                                shortcuts={[
                                    {key: '/', description: 'Search'},
                                    {key: 'TAB', description: 'Search, navigate search items'},
                                    {key: ['fa-arrow-left', 'fa-arrow-right', 'fa-arrow-up', 'fa-arrow-down'], description: 'Navigate rollouts list', icon: true},
                                    {key: ['SHIFT', 'H'], description: 'Show help menu', combo: true},
                                ]}
                                changeNamespace={changeNamespace}
                            />
                            <ProtectedRoute component={<Page path='/rollout/:namespace?/:name' component={<Rollout />} changeNamespace={changeNamespace} />} />
                            <Route path='/login' component={Login} />
                        </Switch>
                    </Router>
                </KeybindingProvider>
            </NamespaceContext.Provider>
        )
    );
};

export default App;
