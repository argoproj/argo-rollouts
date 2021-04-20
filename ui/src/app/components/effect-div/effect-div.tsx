import * as React from 'react';
import {appendSuffixToClasses} from '../../shared/utils/utils';
import ThemeDiv from '../theme-div/theme-div';

import './effect-div.scss';

/* EffectDiv is a component that attaches a background a div, that can be animated with CSS transitions or otherwise.
 * It was designed to avoid text artifacts when scaling a div; an EffectDiv allows you to easily scale JUST its background, and not its contents.
 *
 * You can drop in replace a div with an EffectDiv, but to add a background effect, you need to:
 * - Remove background styles from the main div (including border and border-radius)
 * - Add the styles you removed to the &__background element
 * - Add transitions to the &__background element
 */

export const EffectDiv = (
    props: {children?: React.ReactNode; innerref?: React.MutableRefObject<any>} & React.DetailedHTMLProps<React.HTMLAttributes<HTMLDivElement>, HTMLDivElement>
) => {
    const backgroundCl = appendSuffixToClasses(props.className, '__background');
    return (
        <ThemeDiv className={`${props.className} effect-div`} style={props.style} onClick={props.onClick} innerref={props.innerref}>
            <ThemeDiv className={`effect-div__background ${backgroundCl}`} />
            <div style={{zIndex: 2, position: 'relative', display: 'inherit', flex: 'inherit', alignItems: 'inherit'}}>{props.children}</div>
        </ThemeDiv>
    );
};
