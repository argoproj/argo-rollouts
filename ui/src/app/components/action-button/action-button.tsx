import {faCircleNotch, IconDefinition} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';
import {EffectDiv} from '../effect-div/effect-div';

import './action-button.scss';

export interface ActionButtonProps {
    action?: Function;
    label?: string;
    icon?: IconDefinition;
    indicateLoading?: boolean;
    dark?: boolean;
    disabled?: boolean;
    short?: boolean;
}

export const ActionButton = (props: ActionButtonProps) => {
    const {label, action, icon, indicateLoading, short} = props;
    const [loading, setLoading] = React.useState(false);
    React.useEffect(() => {
        setTimeout(() => setLoading(false), 1000);
    }, [loading]);
    return (
        <EffectDiv
            className={`action-button ${props.dark ? 'action-button--dark' : ''} ${props.disabled ? 'action-button--disabled' : ''}`}
            onClick={(e) => {
                if (action && !props.disabled) {
                    action();
                    setLoading(true);
                }
                e.preventDefault();
            }}>
            {icon && <FontAwesomeIcon icon={loading && indicateLoading ? faCircleNotch : icon} spin={loading && indicateLoading} />}
            {label && !short && <span style={icon && {marginLeft: '5px'}}>{label}</span>}
        </EffectDiv>
    );
};
