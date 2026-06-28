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
            <div className='rollouts-login__card'>
                {error && <Alert type='error' message={error} style={{marginBottom: 12}} />}
                <Form layout='vertical' onFinish={handleFinish}>
                    <Form.Item label='Username' name='username' rules={[{required: true, message: 'Username is required'}]}>
                        <Input id='username' autoFocus />
                    </Form.Item>
                    <Form.Item label='Password' name='password' rules={[{required: true, message: 'Password is required'}]}>
                        <Input.Password id='password' />
                    </Form.Item>
                    <Form.Item>
                        <Button type='primary' htmlType='submit' block loading={submitting}>
                            Log in
                        </Button>
                    </Form.Item>
                </Form>
                <a className='rollouts-login__sso' href={`${base}/auth/login`}>
                    Login with SSO
                </a>
            </div>
        </div>
    );
};
