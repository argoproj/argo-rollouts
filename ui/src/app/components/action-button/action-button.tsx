import {faCheck, faCircleNotch, IconDefinition} from '@fortawesome/free-solid-svg-icons';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {Key, KeybindingContext} from 'react-keyhooks';
import * as React from 'react';
import {useClickOutside} from '../../shared/utils/utils';
import {EffectDiv} from '../effect-div/effect-div';
import {Tooltip} from '../tooltip/tooltip';

import './action-button.scss';

export interface ActionButtonProps {
    action?: Function; // What do you want this button to do when clicked?
    label?: string; // The text shown in the button
    icon?: IconDefinition; // Icon shown on left side of text, or centered if no text. Should be faSomething
    indicateLoading?: boolean; // If set, button's icon (if exists) is briefly replaced with spinner after clicking
    dark?: boolean; // If set, button is always dark
    disabled?: boolean; // If set, button is, and appears, unclickable
    short?: boolean; // If set, button only displays icon (no label)
    style?: React.CSSProperties; // CSS styles
    tooltip?: React.ReactNode; // If set, a tooltip is shown on hover with this content
    shouldConfirm?: boolean; // If set, user must confirm action by clicking again, after clicking the first time
}

export const ActionButton = (props: ActionButtonProps) => {
    const {label, action, icon, indicateLoading, short, shouldConfirm} = props;
    const [loading, setLoading] = React.useState(false);
    const [confirmed, confirm] = React.useState(false);
    const [displayLabel, setDisplayLabel] = React.useState(label);
    const [displayIcon, setDisplayIcon] = React.useState(icon);
    const ref = React.useRef(null);

    React.useEffect(() => {
        setDisplayIcon(props.icon);
        setDisplayLabel(props.label);
    }, [props.icon, props.label]);

    const unconfirm = React.useCallback(() => {
        if (props.shouldConfirm) {
            setDisplayIcon(icon);
            setDisplayLabel(label);
            confirm(false);
        }
    }, [icon, label, props.shouldConfirm]);
    useClickOutside(ref, unconfirm);
    React.useEffect(() => {
        const to = setTimeout(() => setLoading(false), 1000);
        return () => clearInterval(to);
    }, [loading]);

    const {useKeybinding} = React.useContext(KeybindingContext);
    useKeybinding(Key.ESCAPE, () => {
        unconfirm();
        return confirmed;
    });
    const button = (
        <EffectDiv
            className={`action-button ${props.dark ? 'action-button--dark' : ''} ${props.disabled ? 'action-button--disabled' : ''} ${confirmed ? 'action-button--selected' : ''}`}
            style={props.style}
            innerref={ref}
            onClick={(e) => {
                if (props.disabled) {
                    e.preventDefault();
                    return;
                }
                if (shouldConfirm) {
                    if (!confirmed) {
                        setDisplayLabel('SURE?');
                        setDisplayIcon(faCheck);
                        confirm(true);
                        e.preventDefault();
                        return;
                    } else {
                        confirm(false);
                        setDisplayLabel(props.label);
                        setDisplayIcon(props.icon);
                    }
                }
                if (action && (shouldConfirm ? confirmed : true)) {
                    action();
                    setLoading(true);
                    e.preventDefault();
                }
            }}>
            {icon && <FontAwesomeIcon icon={loading && indicateLoading ? faCircleNotch : displayIcon} spin={loading && indicateLoading} />}
            {label && !short && <span style={icon && {marginLeft: '5px'}}>{displayLabel}</span>}
        </EffectDiv>
    );
    return props.tooltip ? <Tooltip content={props.tooltip}>{button}</Tooltip> : button;
};
