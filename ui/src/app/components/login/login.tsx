import * as React from 'react';
import {Button, Divider, Input, notification} from 'antd';
import {AuthContext} from '../../shared/context/auth';
import {getApiBasePath} from '../../shared/context/api';
import './login.scss';

const Logo = () => <img src='assets/images/argo-icon-color-square.png' style={{width: '80px', height: '80px'}} alt='Argo Logo' />;

export const LoginPage = () => {
    const [tokenInput, setTokenInput] = React.useState('');
    const [loading, setLoading] = React.useState(false);
    const [ssoAvailable, setSsoAvailable] = React.useState(false);
    const {setToken, setAuthRequired} = React.useContext(AuthContext);

    // Check if SSO/OIDC login is available by probing the auth/login endpoint
    React.useEffect(() => {
        const basePath = getApiBasePath();
        fetch(`${basePath}/auth/login`, {method: 'HEAD', redirect: 'manual'})
            .then((resp) => {
                // A redirect (302) or success means OIDC is configured
                if (resp.status === 302 || resp.type === 'opaqueredirect' || resp.ok) {
                    setSsoAvailable(true);
                }
            })
            .catch(() => {
                // OIDC not available, ignore
            });
    }, []);

    const handleLogin = async () => {
        const trimmedToken = tokenInput.trim();
        if (!trimmedToken) {
            notification.error({
                message: 'Token required',
                description: 'Please enter a valid Kubernetes bearer token.',
                duration: 5,
                placement: 'bottomRight',
            });
            return;
        }

        setLoading(true);

        try {
            const basePath = getApiBasePath();
            const response = await fetch(`${basePath}/api/v1/version`, {
                headers: {Authorization: `Bearer ${trimmedToken}`},
            });

            if (response.ok) {
                setToken(trimmedToken);
                setAuthRequired(false);
            } else if (response.status === 401) {
                notification.error({
                    message: 'Authentication failed',
                    description: 'The provided token is invalid or expired.',
                    duration: 5,
                    placement: 'bottomRight',
                });
            } else {
                notification.error({
                    message: 'Error',
                    description: `Unexpected response: ${response.status}`,
                    duration: 5,
                    placement: 'bottomRight',
                });
            }
        } catch (e: any) {
            console.error('Login error:', e);
            notification.error({
                message: 'Connection error',
                description: e?.message || 'Could not connect to the server.',
                duration: 5,
                placement: 'bottomRight',
            });
        } finally {
            setLoading(false);
        }
    };

    const handleSSOLogin = () => {
        const basePath = getApiBasePath();
        window.location.href = `${basePath}/auth/login`;
    };

    const handleKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === 'Enter') {
            handleLogin();
        }
    };

    return (
        <div className='login'>
            <div className='login__box'>
                <div className='login__logo'>
                    <Logo />
                </div>
                <div className='login__title'>
                    <img src='assets/images/argologo.svg' alt='Argo Text Logo' style={{height: '1.5em'}} />
                </div>
                <div className='login__subtitle'>Rollouts Dashboard</div>
                <div className='login__form'>
                    {ssoAvailable && (
                        <React.Fragment>
                            <Button type='primary' className='login__button login__button--sso' onClick={handleSSOLogin} block>
                                Login with SSO
                            </Button>
                            <Divider plain>or use a bearer token</Divider>
                        </React.Fragment>
                    )}
                    <label className='login__label' htmlFor='bearer-token-input'>Bearer Token</label>
                    <Input.TextArea
                        id='bearer-token-input'
                        className='login__input'
                        placeholder='Paste your Kubernetes bearer token here'
                        value={tokenInput}
                        onChange={(e) => setTokenInput(e.target.value)}
                        onKeyDown={handleKeyDown}
                        rows={4}
                        autoFocus={!ssoAvailable}
                    />
                    <Button type='primary' className='login__button' onClick={handleLogin} loading={loading} block>
                        Sign In with Token
                    </Button>
                </div>
            </div>
        </div>
    );
};
