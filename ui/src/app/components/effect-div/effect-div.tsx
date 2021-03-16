import * as React from 'react';
import {appendSuffixToClasses} from '../../shared/utils/utils';
import ThemeDiv from '../theme-div/theme-div';

import './effect-div.scss';

export const EffectDiv = (props: {children?: React.ReactNode} & React.DetailedHTMLProps<React.HTMLAttributes<HTMLDivElement>, HTMLDivElement>) => {
    const backgroundCl = appendSuffixToClasses(props.className, '__background');
    return (
        <ThemeDiv className={`${props.className} effect-div`} onClick={props.onClick}>
            <ThemeDiv className={`effect-div__background ${backgroundCl}`} />
            <div style={{zIndex: 2}} onClick={props.onClick}>
                {props.children}
            </div>
        </ThemeDiv>
    );
};
