import {Key, useKeyListener, useNav} from 'react-keyhooks';
import * as React from 'react';
import {Input, useDebounce} from '../input/input';
import ThemeDiv from '../theme-div/theme-div';

import './autocomplete.scss';

export const Autocomplete = (props: React.InputHTMLAttributes<HTMLInputElement> & {items: string[]; inputStyle?: React.CSSProperties; onItemClick?: (item: string) => void}) => {
    const [value, setValue] = React.useState((props.value as string) || '');
    const [curItems, setCurItems] = React.useState(props.items || []);
    const inputRef = React.useRef(null);
    const autocompleteRef = React.useRef(null);
    const [showSuggestions, setShowSuggestions] = React.useState(false);
    const [pos, nav, reset] = useNav(props.items.length);

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

    const listen = useKeyListener();
    listen(Key.TAB, (e) => {
        if (showSuggestions) {
            if (pos === curItems.length - 1) {
                reset();
            }
            nav(1);
            return true;
        }
        return false;
    });

    listen(Key.ESCAPE, (e) => {
        if (showSuggestions) {
            reset();
            setShowSuggestions(false);
            return true;
        }
        return false;
    });

    listen(Key.ENTER, () => {
        if (showSuggestions && props.onItemClick) {
            props.onItemClick(curItems[pos]);
            return true;
        }
        return false;
    });

    listen(Key.UP, () => {
        if (showSuggestions) {
            nav(-1);
            return false;
        }
        return true;
    });

    listen(Key.DOWN, () => {
        if (showSuggestions) {
            nav(1);
            return false;
        }
        return true;
    });

    const style = props.style;
    const trimmedProps = {...props};
    delete trimmedProps.style;
    delete trimmedProps.inputStyle;

    return (
        <div className='autocomplete' ref={autocompleteRef} style={style}>
            <Input
                {...trimmedProps}
                style={props.inputStyle}
                innerref={inputRef}
                className={(props.className || '') + ' autocomplete__input'}
                value={value}
                onChange={(e) => {
                    setValue(e.target.value);
                    if (props.onChange) {
                        props.onChange(e);
                    }
                }}
                onFocus={() => setShowSuggestions(true)}
            />
            <ThemeDiv className='autocomplete__items' hidden={!showSuggestions}>
                {curItems.map((i, n) => (
                    <div
                        key={i}
                        onClick={() => {
                            if (props.onItemClick) {
                                props.onItemClick(i);
                            }
                            setValue(i);
                            props.onChange({target: {value: i}} as React.ChangeEvent<HTMLInputElement>);
                            setShowSuggestions(false);
                        }}
                        className={`autocomplete__items__item ${pos === n ? 'autocomplete__items__item--selected' : ''}`}>
                        {i}
                    </div>
                ))}
            </ThemeDiv>
        </div>
    );
};
