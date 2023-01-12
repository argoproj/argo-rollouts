import * as React from 'react';

import {ActionButton, Brand, InfoItemRow, ThemeToggle, Tooltip, Header as GenericHeader, Autocomplete, ThemeDiv} from 'argo-ui/v2';
import {useParams} from 'react-router';
import {NamespaceContext, RolloutAPIContext} from '../../shared/context/api';

import './header.scss';
import {Link, useHistory} from 'react-router-dom';

const Logo = () => <img src='assets/images/argo-icon-color-square.png' style={{width: '35px', height: '35px', margin: '0 8px'}} alt='Argo Logo' />;

export const Header = (props: {pageHasShortcuts: boolean; changeNamespace: (val: string) => void; showHelp: () => void}) => {
    const history = useHistory();
    const namespaceInfo = React.useContext(NamespaceContext);
    const {name} = useParams<{name: string}>();
    const {namespace} = useParams<{namespace: string}>();
    const api = React.useContext(RolloutAPIContext);
    const [version, setVersion] = React.useState('v?');
    const [nsInput, setNsInput] = React.useState(namespaceInfo.namespace);
    React.useEffect(() => {
        const getVersion = async () => {
            const v = await api.rolloutServiceVersion();
            setVersion(v.rolloutsVersion);
        };
        getVersion();
    }, []);
    React.useEffect(() => {
      if (namespace && namespace != namespaceInfo.namespace) {
        props.changeNamespace(namespace);
        setNsInput(namespace);
      }
    }, []);
    return (
        <GenericHeader>
            <Link to='/'>
                <Brand path={name} brandName='Argo Rollouts' logo={<Logo />} />
            </Link>
            <div className='rollouts-header__info'>
                {props.pageHasShortcuts && (
                    <Tooltip content='Keyboard Shortcuts' inverted={true}>
                        <ActionButton icon={'fa-keyboard'} action={props.showHelp} dark={true} />
                    </Tooltip>
                )}
                <Tooltip content='Documentation' inverted={true}>
                    <a href='https://argo-rollouts.readthedocs.io/' target='_blank' rel='noreferrer'>
                        <ActionButton icon='fa-book' dark={true} />
                    </a>
                </Tooltip>
                <span style={{marginRight: '7px'}}>
                    <Tooltip content='Toggle Dark Mode' inverted={true}>
                        <ThemeToggle />
                    </Tooltip>
                </span>
                {(namespaceInfo.availableNamespaces || []).length == 0 ? (
                    <InfoItemRow label={'NS:'} items={{content: namespaceInfo.namespace}} />
                ) : (
                    <ThemeDiv className='rollouts-header__namespace'>
                        <div className='rollouts-header__label'>NS:</div>
                        <Autocomplete
                            items={namespaceInfo.availableNamespaces || []}
                            placeholder='Namespace'
                            onChange={(el) => setNsInput(el.target.value)}
                            onItemClick={(val) => {
                                const selectedNamespace = val ? val : nsInput;
                                props.changeNamespace(selectedNamespace);
                                history.push(`/${selectedNamespace}`);
                            }}
                            value={nsInput}
                        />
                    </ThemeDiv>
                )}
                <div className='rollouts-header__label'>{version}</div>
            </div>
        </GenericHeader>
    );
};
