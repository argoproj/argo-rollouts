import * as React from 'react';
import './info-item.scss';

export interface InfoLabelProps {
    label: string;
    icon?: JSX.Element;
}

export const InfoItem = (props: InfoLabelProps) => (
    <div className='info-item'>
        {props.icon && <span style={{marginRight: '5px'}}>{props.icon}</span>}
        {props.label}
    </div>
);

export const InfoItemRow = (props: {content: string} & InfoLabelProps) => {
    const {label, content, icon} = props;
    return (
        <div className='info-item--row'>
            <label>{label}</label>
            <InfoItem label={content} icon={icon} />
        </div>
    );
};
