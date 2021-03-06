import * as React from 'react';

import {InfoLabelProps} from '../info-item/info-item';
import './action-button.scss';

export interface ButtonAction extends InfoLabelProps {
    action: () => any;
}

export const ActionButton = (props: {action: () => any} & InfoLabelProps) => {
    const {label, action, icon} = props;
    return (
        <button
            className='action-button'
            onClick={(e) => {
                action();
                e.preventDefault();
            }}>
            {icon && <span style={{marginRight: '5px'}}>{icon}</span>}
            {label}
        </button>
    );
};
