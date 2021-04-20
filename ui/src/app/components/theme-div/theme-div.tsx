import * as React from 'react';
import {Theme, useTheme} from '../../shared/context/theme';

export const ThemeDiv = (
    props: {children?: React.ReactNode; disabled?: boolean; innerref?: React.MutableRefObject<any>} & React.DetailedHTMLProps<React.HTMLAttributes<HTMLDivElement>, HTMLDivElement>
) => {
    const theme = useTheme();
    let clString = props.className;

    if (theme === Theme.Dark && !props.disabled) {
        const cl = (clString || '').split(' ') || [];
        const darkCl = [];
        for (const c of cl) {
            if (!c.endsWith('--dark')) {
                darkCl.push(c + '--dark');
            }
        }
        clString = `${cl.join(' ')} ${darkCl.join(' ')}`;
    }

    return (
        <div {...props} className={clString} ref={props.innerref}>
            {props.children}
        </div>
    );
};

export default ThemeDiv;
