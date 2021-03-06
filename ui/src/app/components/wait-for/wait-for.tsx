import {faCircleNotch} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';

export const Spinner = () => (
    <div style={{color: 'white', fontSize: '25px'}}>
        <FontAwesomeIcon icon={faCircleNotch} spin />
    </div>
);

export const WaitFor = (props: {loading: boolean; loader?: React.ReactNode} & React.ComponentProps<React.FunctionComponent>): JSX.Element => (
    <React.Fragment>{props.loading ? props.loader || <Spinner /> : props.children}</React.Fragment>
);
