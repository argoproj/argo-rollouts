import * as React from 'react';
import {Link, useParams} from 'react-router-dom';
import {RolloutNamespaceInfo, RolloutServiceApi} from '../../../models/rollout/generated';
import {useServerData} from '../../shared/utils/utils';
import {InfoItemRow} from '../info-item/info-item';
import {ThemeToggle} from '../theme-toggle/theme-toggle';

import './header.scss';

export const Logo = () => <img src='assets/images/argo-icon-color-square.png' style={{width: '35px', height: '35px', margin: '0 8px'}} alt='Argo Logo' />;

const Brand = (props: {path?: string}) => (
    <Link to='/' className='rollouts-header__brand'>
        <Logo />
        <h1> Rollouts </h1>
        {props.path && <h2> / {props.path} </h2>}
    </Link>
);

export const Header = () => {
    const getNs = React.useCallback(() => new RolloutServiceApi().getNamespace(), []);
    const nsData = useServerData<RolloutNamespaceInfo>(getNs);
    const {name} = useParams<{name: string}>();
    return (
        <header className='rollouts-header'>
            <Brand path={name} />
            <div className='rollouts-header__info'>
                <span style={{marginRight: '7px'}}>
                    <ThemeToggle />
                </span>
                <InfoItemRow label={'NS:'} content={{content: nsData.namespace}} />
                <div className='rollouts-header__version'>v0.1.0</div>
            </div>
        </header>
    );
};
