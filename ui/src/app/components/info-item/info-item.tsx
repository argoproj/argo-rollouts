import {IconDefinition} from '@fortawesome/fontawesome-svg-core';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';
import {ThemeDiv} from '../theme-div/theme-div';
import './info-item.scss';

export enum InfoItemKind {
    Default = 'default',
    Colored = 'colored',
    Canary = 'canary',
    BlueGreen = 'bluegreen',
}

export interface InfoItemProps {
    content?: string;
    icon?: IconDefinition;
    style?: React.CSSProperties;
    kind?: InfoItemKind;
}

export const InfoItem = (props: InfoItemProps) => (
    <ThemeDiv className={`info-item${props.kind ? ` info-item--${props.kind}` : ''}`} style={props.style}>
        {props.icon && (
            <span style={props.content && {marginRight: '5px'}}>
                <FontAwesomeIcon icon={props.icon} />
            </span>
        )}
        {props.content}
    </ThemeDiv>
);

export const InfoItemRow = (props: {label: string; items: InfoItemProps | InfoItemProps[]}) => {
    let {label, items} = props;
    if (!Array.isArray(items)) {
        items = [items];
    }
    return (
        <div className='info-item--row'>
            {props.label && <label>{label}</label>}
            <div style={{marginLeft: 'auto', display: 'flex'}}>
                {items.map((c, i) => (
                    <InfoItem key={`${c} ${i}`} {...c} />
                ))}
            </div>
        </div>
    );
};
