import {IconDefinition} from '@fortawesome/fontawesome-common-types';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';

import './shortcuts.scss';

type StringOrIcon = string | IconDefinition;
type StringsOrIcons = (string | IconDefinition)[];

export interface Shortcut {
    key?: StringOrIcon | StringsOrIcons;
    description: string;
    combo?: boolean;
}

export const Shortcuts = (props: {shortcuts: Shortcut[]}) => {
    if (!props.shortcuts) {
        return <React.Fragment />;
    }
    return (
        <div>
            {props.shortcuts.map((sc, i) => {
                if (!Array.isArray(sc.key)) {
                    sc.key = [sc.key as StringOrIcon];
                }
                return (
                    <div className='shortcuts__shortcut' key={i}>
                        {(sc.key as StringsOrIcons).map((k, i) => {
                            let contents: any = k;
                            if (typeof k !== 'string') {
                                contents = <FontAwesomeIcon icon={k} />;
                            }
                            return (
                                <React.Fragment key={i}>
                                    <div className='shortcuts__key'>{contents}</div>
                                    {sc.combo && i !== (sc.key as StringsOrIcons).length - 1 && <div style={{marginRight: '5px'}}>+</div>}
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
