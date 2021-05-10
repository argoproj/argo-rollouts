import * as React from 'react';

import {ActionButton, Brand, InfoItemRow, ThemeToggle, Tooltip, useData, Header as GenericHeader} from 'argo-ux';
import {useParams} from 'react-router';
import {RolloutNamespaceInfo, RolloutServiceApi} from '../../../models/rollout/generated';
import {RolloutAPIContext} from '../../shared/context/api';
import {faBook, faKeyboard} from '@fortawesome/free-solid-svg-icons';

import './header.scss';
import {Link} from 'react-router-dom';

const Logo = () => <img src='assets/images/argo-icon-color-square.png' style={{width: '35px', height: '35px', margin: '0 8px'}} alt='Argo Logo' />;

export const Header = (props: {pageHasShortcuts: boolean; showHelp: () => void}) => {
    const getNs = React.useCallback(() => new RolloutServiceApi().rolloutServiceGetNamespace(), []);
    const [nsData, loading] = useData<RolloutNamespaceInfo>(getNs);
    const [namespace, setNamespace] = React.useState('Unknown');
    React.useEffect(() => {
        if (!loading && nsData && nsData.namespace) {
            setNamespace(nsData.namespace);
        }
    }, [nsData, loading]);
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
        <GenericHeader>
            <Link to='/'>
                <Brand path={name} brandName='Argo Rollouts' logo={<Logo />} />
            </Link>
            <div className='rollouts-header__info'>
                {props.pageHasShortcuts && (
                    <Tooltip content='Keyboard Shortcuts' inverted={true}>
                        <ActionButton icon={faKeyboard} action={props.showHelp} dark={true} />
                    </Tooltip>
                )}
                <Tooltip content='Documentation' inverted={true}>
                    <a href='https://argoproj.github.io/argo-rollouts/' target='_blank' rel='noreferrer'>
                        <ActionButton icon={faBook} dark={true} />
                    </a>
                </Tooltip>
                <span style={{marginRight: '7px'}}>
                    <Tooltip content='Toggle Dark Mode' inverted={true}>
                        <ThemeToggle />
                    </Tooltip>
                </span>
                <InfoItemRow label={'NS:'} items={{content: namespace}} />
                <div className='rollouts-header__version'>{version}</div>
            </div>
        </GenericHeader>
    );
};
