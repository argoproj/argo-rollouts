import * as React from 'react';

import './shortcuts.scss';

export interface Shortcut {
    key?: string | string[];
    description: string;
    combo?: boolean;
    icon?: boolean;
}

export const Shortcuts = (props: {shortcuts: Shortcut[]}) => {
    if (!props.shortcuts) {
        return <React.Fragment />;
    }
    return (
        <div>
            {props.shortcuts.map((sc, i) => {
                if (!Array.isArray(sc.key)) {
                    sc.key = [sc.key];
                }
                return (
                    <div className='shortcuts__shortcut' key={i}>
                        {(sc.key || []).map((k, i) => {
                            let contents: any = k;
                            if (sc.icon) {
                                contents = <i className={`fa ${k}`} />;
                            }
                            return (
                                <React.Fragment key={i}>
                                    <div className='shortcuts__key'>{contents}</div>
                                    {sc.combo && i !== sc.key?.length - 1 && <div style={{marginRight: '5px'}}>+</div>}
                                </React.Fragment>
                            );
                        })}
                        <div className='shortcuts__description'>{sc.description}</div>
                    </div>
                );
            })}
        </div>
    );
};
