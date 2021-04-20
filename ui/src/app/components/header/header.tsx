import {faBook, faKeyboard} from '@fortawesome/free-solid-svg-icons';
import * as React from 'react';
import {Link, useParams} from 'react-router-dom';
import {RolloutNamespaceInfo, RolloutServiceApi} from '../../../models/rollout/generated';
import {RolloutAPIContext} from '../../shared/context/api';
import {useServerData} from '../../shared/utils/utils';
import {ActionButton} from '../action-button/action-button';
import {InfoItemRow} from '../info-item/info-item';
import {ThemeToggle} from '../theme-toggle/theme-toggle';
import {Tooltip} from '../tooltip/tooltip';

import './header.scss';

export const Logo = () => <img src='assets/images/argo-icon-color-square.png' style={{width: '35px', height: '35px', margin: '0 8px'}} alt='Argo Logo' />;

const Brand = (props: {path?: string}) => {
    const [loading, setLoading] = React.useState(true);
    React.useEffect(() => {
        setTimeout(() => setLoading(false), 500);
    }, []);

    const showWelcome = loading && !props.path;
    return (
        <Link to='/' className='rollouts-header__brand'>
            <Logo />
            <h1>
                <div className='rollouts-header__welcome' style={{opacity: showWelcome ? 1 : 0, transform: showWelcome ? 'none' : 'scaleX(0.01)'}}>
                    Welcome to Argo
                </div>
                <div className='rollouts-header__title' style={showWelcome ? {} : {transform: 'translateX(0)'}}>
                    Rollouts {props.path && <h2> / {props.path} </h2>}
                </div>
            </h1>
        </Link>
    );
};

export const Header = (props: {pageHasShortcuts: boolean; showHelp: () => void}) => {
    const getNs = React.useCallback(() => new RolloutServiceApi().rolloutServiceGetNamespace(), []);
    const namespace = useServerData<RolloutNamespaceInfo>(getNs).namespace || 'Unknown';
    const {name} = useParams<{name: string}>();
    const api = React.useContext(RolloutAPIContext);
    const [version, setVersion] = React.useState('v?');
    React.useEffect(() => {
        const getVersion = async () => {
            const v = await api.rolloutServiceVersion();
            setVersion(v.rolloutsVersion);
        };
        getVersion();
    });
    return (
        <header className='rollouts-header'>
            <Brand path={name} />
            <div className='rollouts-header__info'>
                {props.pageHasShortcuts && (
                    <Tooltip content='Keyboard Shortcuts' inverted>
                        <ActionButton icon={faKeyboard} action={props.showHelp} dark />
                    </Tooltip>
                )}
                <Tooltip content='Documentation' inverted>
                    <a href='https://argoproj.github.io/argo-rollouts/' target='_blank' rel='noreferrer'>
                        <ActionButton icon={faBook} dark />
                    </a>
                </Tooltip>
                <span style={{marginRight: '7px'}}>
                    <Tooltip content='Toggle Dark Mode' inverted>
                        <ThemeToggle />
                    </Tooltip>
                </span>
                <InfoItemRow label={'NS:'} items={{content: namespace}} />
                <div className='rollouts-header__version'>{version}</div>
            </div>
        </header>
    );
};
