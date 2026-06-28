import * as React from 'react';
import {Button, Form, Input, Alert} from 'antd';
import {getApiBasePath} from '../../shared/context/api';
import './login.scss';

export interface LoginPageProps {
    onSuccess?: () => void;
}

export const LoginPage: React.FC<LoginPageProps> = ({onSuccess}) => {
    const base = getApiBasePath();
    const [error, setError] = React.useState<string | null>(null);
    const [submitting, setSubmitting] = React.useState(false);

    const handleFinish = async (values: {username: string; password: string}) => {
        setSubmitting(true);
        setError(null);
        try {
            const res = await fetch(`${base}/api/login`, {
                method: 'POST',
                credentials: 'include',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({username: values.username, password: values.password}),
            });
            if (!res.ok) {
                setError('Invalid username or password');
                return;
            }
            if (onSuccess) {
                onSuccess();
            } else {
                window.location.assign(`${base}/`);
            }
        } catch (e) {
            setError('Invalid username or password');
        } finally {
            setSubmitting(false);
        }
    };

    return (
        <div className='rollouts-login'>
            <div className='rollouts-login__content'>
                <div className='rollouts-login__text'>Let&apos;s get stuff rolled out!</div>
                <div className='rollouts-login__logo-hero' />
            </div>
            <div className='rollouts-login__box'>
                <div className='rollouts-login__brand'>
                    <img className='rollouts-login__brand-icon' src='assets/images/argo-icon-color-square.png' alt='Argo' />
                    <div className='rollouts-login__brand-title'>Argo Rollouts</div>
                </div>
                <div className='rollouts-login__form'>
                    {error && <Alert type='error' message={error} showIcon style={{marginBottom: 16}} />}
                    <Form layout='vertical' requiredMark={false} onFinish={handleFinish}>
                        <Form.Item label='Username' name='username' rules={[{required: true, message: 'Username is required'}]}>
                            <Input id='username' size='large' autoFocus autoCapitalize='none' autoComplete='username' />
                        </Form.Item>
                        <Form.Item label='Password' name='password' rules={[{required: true, message: 'Password is required'}]}>
                            <Input.Password id='password' size='large' autoComplete='current-password' />
                        </Form.Item>
                        <Form.Item style={{marginBottom: 0}}>
                            <Button type='primary' htmlType='submit' size='large' block loading={submitting}>
                                Log in
                            </Button>
                        </Form.Item>
                    </Form>
                    <div className='rollouts-login__separator'>
                        <span>or</span>
                    </div>
                    <a className='rollouts-login__sso' href={`${base}/auth/login`}>
                        Log in via SSO
                    </a>
                </div>
                <div className='rollouts-login__footer'>
                    <a href='https://argoproj.io' target='_blank' rel='noreferrer'>
                        <img src='assets/images/argologo.svg' alt='Argo' />
                    </a>
                </div>
            </div>
        </div>
    );
};
