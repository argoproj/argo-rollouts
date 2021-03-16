import * as React from 'react';
import {Input, useDebounce} from '../input/input';
import ThemeDiv from '../theme-div/theme-div';

import './autocomplete.scss';

export const Autocomplete = (props: React.InputHTMLAttributes<HTMLInputElement> & {items: string[]}) => {
    const [value, setValue] = React.useState((props.value as string) || '');
    const [curItems, setCurItems] = React.useState(props.items || []);
    const inputRef = React.useRef(null);
    const autocompleteRef = React.useRef(null);
    const [showSuggestions, setShowSuggestions] = React.useState(false);
    React.useEffect(() => {
        function unfocus(e: any) {
            if (autocompleteRef.current && !autocompleteRef.current.contains(e.target)) {
                setShowSuggestions(false);
            }
        }

        document.addEventListener('mousedown', unfocus);
        return () => document.removeEventListener('mousedown', unfocus);
    }, [autocompleteRef]);

    const debouncedVal = useDebounce(value, 350);

    React.useEffect(() => {
        const filtered = (props.items || []).filter((i) => {
            return i.includes(debouncedVal);
        });
        setCurItems(filtered.length > 0 ? filtered : props.items);
    }, [debouncedVal, props.items]);

    return (
        <div className='autocomplete' ref={autocompleteRef}>
            <Input
                {...props}
                innerref={inputRef}
                className={(props.className || '') + ' autocomplete__input'}
                value={value}
                onChange={(e) => {
                    setValue(e.target.value);
                    props.onChange(e);
                }}
                onFocus={() => setShowSuggestions(true)}
            />
            <ThemeDiv className='autocomplete__items' hidden={!showSuggestions}>
                {curItems.map((i) => (
                    <div
                        key={i}
                        onClick={() => {
                            setValue(i);
                            props.onChange({target: {value: i}} as React.ChangeEvent<HTMLInputElement>);
                            setShowSuggestions(false);
                        }}
                        className='autocomplete__items__item'>
                        {i}
                    </div>
                ))}
            </ThemeDiv>
        </div>
    );
};
