import * as React from 'react';
import {AuthContext, getAuthToken} from '../../shared/context/auth';
import {Alert, Button, Input} from 'antd';

import './login.scss';

export const Login = (props: {error?: string}) => {
    const {login} = React.useContext(AuthContext);
    // seed from the cookie so a rejected token stays on screen and can be corrected
    const [tokenInput, setTokenInput] = React.useState(getAuthToken() || '');

    const handleLogin = () => {
        const trimmed = tokenInput.trim();
        if (trimmed) {
            login(trimmed);
        }
    };

    return (
        <div className='login'>
            <div className='login__box'>
                <img src='assets/images/argo-icon-color-square.png' alt='Argo Logo' className='login__logo' />
                <h2 className='login__title'>Argo Rollouts</h2>
                <p className='login__subtitle'>Enter a Kubernetes bearer token to continue</p>
                {props.error && <Alert className='login__error' type='error' message={props.error} showIcon />}
                <Input.TextArea
                    className='login__input'
                    placeholder='Bearer token'
                    aria-label='Bearer token'
                    value={tokenInput}
                    onChange={(e) => setTokenInput(e.target.value)}
                    onPressEnter={handleLogin}
                    rows={4}
                    autoFocus
                />
                <Button type='primary' onClick={handleLogin} disabled={!tokenInput.trim()} block>
                    Login
                </Button>
                <p className='login__hint'>
                    Run <code>kubectl create token &lt;service-account&gt;</code> to get a token. See the{' '}
                    <a href='https://argo-rollouts.readthedocs.io/en/stable/dashboard/' target='_blank' rel='noreferrer'>
                        dashboard documentation
                    </a>
                    .
                </p>
            </div>
        </div>
    );
};
