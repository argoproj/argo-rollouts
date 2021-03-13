import {faCircleNotch} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';
import {ThemeDiv} from '../theme-div/theme-div';
import './spinner.scss';
import './loading-bar.scss';

export const Spinner = () => (
    <ThemeDiv className='spinner'>
        <FontAwesomeIcon icon={faCircleNotch} spin />
    </ThemeDiv>
);

export const LoadingBar = (props: {loadms?: string | number}) => {
    const [loading, setLoading] = React.useState(true);

    const loadms = props.loadms || 400;

    React.useEffect(() => {
        setLoading(false);
    }, []);
    return (
        <ThemeDiv className={`loading-bar ${!loading ? 'loading-bar--loaded' : ''}`} style={{transition: `opacity 200ms ease ${loadms}ms`}} onClick={() => setLoading(false)}>
            <div className={`loading-bar__fill ${!loading ? 'loading-bar__fill--loaded' : ''}`} style={{transition: `transform ${loadms}ms ease`}} />
        </ThemeDiv>
    );
};

export const WaitFor = (props: {loading: boolean; loader?: React.ReactNode; loadms?: string | number} & React.ComponentProps<React.FunctionComponent>): JSX.Element => (
    <React.Fragment>{props.loading ? props.loader || <LoadingBar loadms={props.loadms} /> : props.children}</React.Fragment>
);
