import * as React from 'react';

import {ActionButton, Brand, InfoItemRow, ThemeToggle, Tooltip, Header as GenericHeader, Autocomplete, useAutocomplete, ThemeDiv} from 'argo-ux';
import {useParams} from 'react-router';
import {RolloutAPIContext} from '../../shared/context/api';
import {faBook, faKeyboard} from '@fortawesome/free-solid-svg-icons';
import {NamespaceContext} from '../../shared/context/api';

import './header.scss';
import {Link} from 'react-router-dom';

const Logo = () => <img src='assets/images/argo-icon-color-square.png' style={{width: '35px', height: '35px', margin: '0 8px'}} alt='Argo Logo' />;

export const Header = (props: {pageHasShortcuts: boolean; showHelp: () => void}) => {
    const namespaceCtx = React.useContext(NamespaceContext);
    const [, setNamespaceInputStr, namespaceInput] = useAutocomplete('');
    React.useEffect(() => {
        if (namespaceCtx.namespace) {
            console.log("namespace context updated: " + namespaceCtx.namespace)
            setNamespaceInputStr(namespaceCtx.namespace)
        }
    }, [namespaceCtx.namespace]);

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
                <InfoItemRow label={'NS:'} items={{content: namespaceCtx.namespace}} />
                <ThemeDiv className='rollouts-header__namespace'>
                    <Autocomplete items={namespaceCtx.availableNamespaces}
                                  placeholder='Namespace'
                                  onItemClick={(item) => { namespaceCtx.set(item) } }
                                  {...namespaceInput}
                    />
                </ThemeDiv>
                <div className='rollouts-header__version'>{version}</div>
            </div>
        </GenericHeader>
    );
};
