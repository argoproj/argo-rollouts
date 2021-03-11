import * as React from 'react';
import {ThemeDiv} from '../theme-div/theme-div';

import './input.scss';

interface InputProps {
    value: string;
    onChange: (e: React.ChangeEvent<HTMLInputElement>) => void;
}

type SetInputFxn = (val: string) => void;
export const FormResetFactory = (setFxns: SetInputFxn[]) => {
    return () => {
        setFxns.forEach((reset) => reset(''));
    };
};

export const useInput = (init: string, callback?: (val: string) => void): [string, SetInputFxn, InputProps] => {
    const [state, setState] = React.useState(init);

    const Input: InputProps = {
        value: state,
        onChange: (e: React.ChangeEvent<HTMLInputElement>) => {
            setState(e.target.value);
            if (callback) {
                callback(e.target.value);
            }
        },
    };

    return [state, setState, Input];
};

export const Input = (props: React.InputHTMLAttributes<HTMLInputElement>) => (
    <ThemeDiv className='input-container'>
        <input {...props} className='input' />
    </ThemeDiv>
);
