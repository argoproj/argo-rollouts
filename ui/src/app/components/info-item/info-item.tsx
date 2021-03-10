import {IconDefinition} from '@fortawesome/fontawesome-svg-core';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';
import {ThemeDiv} from '../theme-div/theme-div';
import './info-item.scss';

export interface InfoItemProps {
    content?: string;
    icon?: IconDefinition;
    style?: React.CSSProperties;
}

export const InfoItem = (props: InfoItemProps) => (
    <ThemeDiv className='info-item' style={props.style}>
        {props.icon && (
            <span style={{marginRight: '5px'}}>
                <FontAwesomeIcon icon={props.icon} />
            </span>
        )}
        {props.content && props.content}
    </ThemeDiv>
);

export const InfoItemRow = (props: {label: string; content: InfoItemProps | InfoItemProps[]}) => {
    let {label, content} = props;
    if (!Array.isArray(content)) {
        content = [content];
    }
    return (
        <div className='info-item--row'>
            {props.label && <label>{label}</label>}
            <div style={{marginLeft: 'auto', display: 'flex'}}>
                {content.map((c, i) => (
                    <InfoItem key={`${c} ${i}`} {...c} />
                ))}
            </div>
        </div>
    );
};
