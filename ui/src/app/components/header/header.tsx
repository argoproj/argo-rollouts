import * as React from 'react';

import './header.scss';

export const Logo = () => <img src='assets/images/argo-icon-white-square.svg' style={{width: '35px', margin: '0 8px'}} alt='Argo Logo' />;

export const Header = () => (
    <header className='rollouts-header'>
        <Logo />
        <h1> Rollouts </h1>
        <div className='rollouts-header__version'>v0.1.0</div>
    </header>
);
