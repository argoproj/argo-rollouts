import {IconDefinition} from '@fortawesome/fontawesome-common-types';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import * as React from 'react';
import {ThemeDiv} from '../theme-div/theme-div';

import './input.scss';

export interface InputProps {
    value: string;
    ref?: React.MutableRefObject<HTMLInputElement>;
    onChange: (e: React.ChangeEvent<HTMLInputElement>) => void;
}

export type SetInputFxn = (val: string) => void;
export const FormResetFactory = (setFxns: SetInputFxn[]) => {
    return () => {
        setFxns.forEach((reset) => reset(''));
    };
};

export const useInput = (init: string, callback?: (val: string) => void): [string, SetInputFxn, InputProps] => {
    const [state, setState] = React.useState(init);
    const inputRef = React.useRef(null);

    const changeHandler = (value: string) => {
        setState(value);
        if (callback) {
            callback(value);
        }
    };

    return [state, changeHandler, {ref: inputRef, onChange: (e) => changeHandler(e.target.value), value: state}];
};

export const useDebounce = (value: string, debouncems: number): string => {
    const [val, setVal] = React.useState(value);

    React.useEffect(() => {
        const to = setTimeout(() => {
            setVal(value);
        }, debouncems);
        return () => clearInterval(to);
    }, [value, debouncems]);

    return val;
};

export const Input = (props: React.InputHTMLAttributes<HTMLInputElement> & {innerref?: React.MutableRefObject<any>; icon?: IconDefinition}) => {
    return (
        <ThemeDiv className='input-container'>
            {props.icon && <FontAwesomeIcon icon={props.icon} className='input-container__icon' />}
            <input {...props} className={props.className ? `${props.className} input` : 'input'} ref={props.innerref} />
        </ThemeDiv>
    );
};
