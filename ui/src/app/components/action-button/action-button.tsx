import {faCircleNotch, IconDefinition} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';
import {EffectDiv} from '../effect-div/effect-div';
import {Tooltip} from '../tooltip/tooltip';

import './action-button.scss';

export interface ActionButtonProps {
    action?: Function;
    label?: string;
    icon?: IconDefinition;
    indicateLoading?: boolean;
    dark?: boolean;
    disabled?: boolean;
    short?: boolean;
    style?: React.CSSProperties;
    tooltip?: React.ReactNode;
}

export const ActionButton = (props: ActionButtonProps) => {
    const {label, action, icon, indicateLoading, short} = props;
    const [loading, setLoading] = React.useState(false);
    React.useEffect(() => {
        const to = setTimeout(() => setLoading(false), 1000);
        return () => clearInterval(to);
    }, [loading]);
    const button = (
        <EffectDiv
            className={`action-button ${props.dark ? 'action-button--dark' : ''} ${props.disabled ? 'action-button--disabled' : ''}`}
            style={props.style}
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
    return props.tooltip ? <Tooltip content={props.tooltip}>{button}</Tooltip> : button;
};
