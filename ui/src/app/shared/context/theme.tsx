import * as React from 'react';

export const THEME_KEY = 'theme';

export enum Theme {
    Dark = 'dark',
    Light = 'light',
}

const init = (JSON.parse(window.localStorage.getItem(THEME_KEY)) as Theme) || Theme.Light;

interface ThemeContextProps {
    theme: Theme;
    set: (th: Theme) => void;
}

export const ThemeContext = React.createContext({theme: init} as ThemeContextProps);

export const ThemeProvider = (props: {children: React.ReactNode}) => {
    const [theme, setTheme] = React.useState(init);
    React.useEffect(() => {
        window.localStorage.setItem(THEME_KEY, JSON.stringify(theme));
    }, [theme]);

    return <ThemeContext.Provider value={{theme: theme, set: (th) => setTheme(th)}}>{props.children}</ThemeContext.Provider>;
};

export const useTheme = () => {
    try {
        const dmCtx = React.useContext(ThemeContext);
        return dmCtx.theme;
    } catch {
        return Theme.Light;
    }
};
