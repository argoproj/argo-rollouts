import {faCircleNotch} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';
import {ThemeDiv} from '../theme-div/theme-div';
import './spinner.scss';

export const Spinner = () => (
    <ThemeDiv className='spinner'>
        <FontAwesomeIcon icon={faCircleNotch} spin />
    </ThemeDiv>
);

export const WaitFor = (props: {loading: boolean; loader?: React.ReactNode} & React.ComponentProps<React.FunctionComponent>): JSX.Element => (
    <React.Fragment>{props.loading ? props.loader || <Spinner /> : props.children}</React.Fragment>
);
