// jsdom implements neither of these, and antd reads both while rendering.
Object.defineProperty(window, 'matchMedia', {
    writable: true,
    value: (query: string) => ({
        matches: false,
        media: query,
        onchange: null as any,
        addListener: () => {},
        removeListener: () => {},
        addEventListener: () => {},
        removeEventListener: () => {},
        dispatchEvent: () => false,
    }),
});

(global as any).ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
};
