import * as React from 'react';
import {ThemeDiv} from '../theme-div/theme-div';

import './tooltip.scss';

export const useHover = (): [React.MutableRefObject<any>, boolean] => {
    const [show, setShow] = React.useState(false);
    const ref = React.useRef(null);

    const handleMouseOver = () => setShow(true);
    const handleMouseOut = () => setShow(false);

    React.useEffect(() => {
        const cur = ref.current;

        if (cur) {
            cur.addEventListener('mouseover', handleMouseOver);
            cur.addEventListener('mouseout', handleMouseOut);

            return () => {
                cur.removeEventListener('mouseover', handleMouseOver);
                cur.removeEventListener('mouseout', handleMouseOut);
            };
        }
    }, []);

    return [ref, show];
};

export const Tooltip = (props: {content: React.ReactNode | string} & React.PropsWithRef<any>) => {
    const [tooltip, showTooltip] = useHover();
    return (
        <div style={{position: 'relative'}}>
            <ThemeDiv hidden={!showTooltip} className='tooltip'>
                {props.content}
            </ThemeDiv>
            <div ref={tooltip}>{props.children}</div>
        </div>
    );
};
