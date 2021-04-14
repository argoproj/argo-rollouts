import {Key, KeybindingContext, useNav} from 'react-keyhooks';
import * as React from 'react';
import {Input, InputProps, SetInputFxn, useDebounce, useInput} from '../input/input';
import ThemeDiv from '../theme-div/theme-div';

import './autocomplete.scss';
import {IconDefinition} from '@fortawesome/fontawesome-common-types';

interface AutocompleteProps extends InputProps {
    inputref?: React.MutableRefObject<HTMLInputElement>;
}

export const useAutocomplete = (init: string, callback?: (val: string) => void): [string, SetInputFxn, AutocompleteProps] => {
    const [state, setState, Input] = useInput(init);
    const Autocomplete = Input as AutocompleteProps;
    if (Autocomplete.ref) {
        Autocomplete.inputref = Input.ref;
        delete Autocomplete.ref;
    }
    return [state, setState, Autocomplete];
};

export const Autocomplete = (
    props: React.InputHTMLAttributes<HTMLInputElement> & {
        items: string[];
        inputStyle?: React.CSSProperties;
        onItemClick?: (item: string) => void;
        icon?: IconDefinition;
        inputref?: React.MutableRefObject<HTMLInputElement>;
    }
) => {
    const [curItems, setCurItems] = React.useState(props.items || []);
    const nullInputRef = React.useRef<HTMLInputElement>(null);
    const inputRef = props.inputref || nullInputRef;
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

    const debouncedVal = useDebounce(props.value as string, 350);

    React.useEffect(() => {
        const filtered = (props.items || []).filter((i) => {
            return i.includes(debouncedVal);
        });
        setCurItems(filtered.length > 0 ? filtered : props.items);
    }, [debouncedVal, props.items]);

    const {useKeybinding} = React.useContext(KeybindingContext);
    useKeybinding(Key.TAB, (e) => {
        if (showSuggestions) {
            if (pos === curItems.length - 1) {
                reset();
            }
            nav(1);
            return true;
        }
        return false;
    });

    useKeybinding(Key.ESCAPE, (e) => {
        if (showSuggestions) {
            reset();
            setShowSuggestions(false);
            if (inputRef && inputRef.current) {
                inputRef.current.blur();
            }
            return true;
        }
        return false;
    });

    useKeybinding(Key.ENTER, () => {
        if (showSuggestions && props.onItemClick) {
            props.onItemClick(curItems[pos]);
            return true;
        }
        return false;
    });

    useKeybinding(Key.UP, () => {
        if (showSuggestions) {
            nav(-1);
            return false;
        }
        return true;
    });

    useKeybinding(Key.DOWN, () => {
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
    delete trimmedProps.onItemClick;

    return (
        <div className='autocomplete' ref={autocompleteRef} style={style}>
            <Input
                {...trimmedProps}
                style={props.inputStyle}
                innerref={inputRef}
                className={(props.className || '') + ' autocomplete__input'}
                onChange={(e) => {
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
