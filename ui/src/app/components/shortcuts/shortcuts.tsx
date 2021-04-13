import {IconDefinition} from '@fortawesome/fontawesome-common-types';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';

import './shortcuts.scss';

type StringOrIcon = string | IconDefinition;
type StringsOrIcons = (string | IconDefinition)[];

export interface Shortcut {
    key?: StringOrIcon | StringsOrIcons;
    description: string;
}

export const Shortcuts = (props: {shortcuts: Shortcut[]}) => {
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
                                <div key={i} className='shortcuts__key'>
                                    {contents}
                                </div>
                            );
                        })}
                        <div className='shortcuts__description'>{sc.description}</div>
                    </div>
                );
            })}
        </div>
    );
};
