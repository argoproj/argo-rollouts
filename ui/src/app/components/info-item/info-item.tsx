import * as React from 'react';
import './info-item.scss';
import { Tooltip } from 'antd';

export enum InfoItemKind {
    Default = 'default',
    Colored = 'colored',
    Monospace = 'monospace',
    Canary = 'canary',
    BlueGreen = 'bluegreen',
}

export interface InfoItemProps {
    content?: string;
    icon?: string;
    style?: React.CSSProperties;
    kind?: InfoItemKind;
    truncate?: boolean;
    lightweight?: boolean;
}

/**
 * Displays a small piece encapsulated piece of data
 */
export const InfoItem = (props: InfoItemProps) => {
    const truncateStyle = props.truncate ? {overflow: 'hidden', whiteSpace: 'nowrap', textOverflow: 'ellipsis'} : {};
    const item = (
        <div className={`info-item${props.kind ? ` info-item--${props.kind}` : ''} ${props.lightweight ? 'info-item--lightweight' : ''}`} style={props.style}>
            {props.icon && (
                <span style={props.content && {marginRight: '5px'}}>
                    <i className={`fa ${props.icon}`} />
                </span>
            )}
            <div style={truncateStyle as React.CSSProperties}>{props.content}</div>
        </div>
    );
    return props.truncate ? <Tooltip title={props.content}>{item}</Tooltip> : item;
};

/**
 * Displays a right justified InfoItem (or multiple InfoItems) and a left justfied label
 */
export const InfoItemRow = (props: {label: string | React.ReactNode; items?: InfoItemProps | InfoItemProps[]; lightweight?: boolean}) => {
    let {label, items} = props;
    let itemComponents = null;
    if (!Array.isArray(items)) {
        items = [items];
    }
    itemComponents = items.map((c, i) => <InfoItem key={`${c} ${i}`} {...c} lightweight={c.lightweight === undefined ? props.lightweight : c.lightweight} />);

    return (
        <div className='info-item--row'>
            {props.label && (
                <div>
                    <label>{label}</label>
                </div>
            )}
            {props.items && <div className='info-item--row__container'>{itemComponents}</div>}
        </div>
    );
};
