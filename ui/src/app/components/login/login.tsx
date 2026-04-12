import * as React from 'react';
import {AuthContext} from '../../shared/context/auth';
import {getApiBasePath} from '../../shared/context/api';
import {Button, Input, notification} from 'antd';

import './login.scss';

export const Login = () => {
    const {setToken, setAuthRequired} = React.useContext(AuthContext);
    const [tokenInput, setTokenInput] = React.useState('');
    const [loading, setLoading] = React.useState(false);

    const handleLogin = async () => {
        const trimmed = tokenInput.trim();
        if (!trimmed) {
            notification.error({
                message: 'Token required',
                description: 'Please enter a Kubernetes bearer token.',
                duration: 5,
                placement: 'bottomRight',
            });
            return;
        }

        setLoading(true);
        try {
            const basePath = getApiBasePath();
            const res = await fetch(`${basePath}/api/v1/version`, {
                headers: {Authorization: `Bearer ${trimmed}`},
            });
            if (res.status === 401) {
                notification.error({
                    message: 'Authentication failed',
                    description: 'The provided token is invalid or expired.',
                    duration: 5,
                    placement: 'bottomRight',
                });
                return;
            }
            if (!res.ok) {
                throw new Error(`Unexpected response: ${res.status}`);
            }
            setToken(trimmed);
            setAuthRequired(false);
        } catch (e) {
            notification.error({
                message: 'Connection error',
                description: e.message || 'Failed to connect to the server.',
                duration: 5,
                placement: 'bottomRight',
            });
        } finally {
            setLoading(false);
        }
    };

    return (
        <div className='login'>
            <div className='login__box'>
                <img src='assets/images/argo-icon-color-square.png' alt='Argo Logo' className='login__logo' />
                <h2 className='login__title'>Argo Rollouts</h2>
                <p className='login__subtitle'>Enter a Kubernetes bearer token to continue</p>
                <Input.TextArea
                    className='login__input'
                    placeholder='Bearer token'
                    value={tokenInput}
                    onChange={(e) => setTokenInput(e.target.value)}
                    onPressEnter={handleLogin}
                    rows={4}
                    autoFocus
                />
                <Button type='primary' onClick={handleLogin} loading={loading} block>
                    Login
                </Button>
            </div>
        </div>
    );
};
