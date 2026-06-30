/** @jest-environment jsdom */
import * as React from 'react';
import {render, screen} from '@testing-library/react';

// Proves jsdom + @testing-library/react render works; the scss mapper is exercised by login.test.tsx.
import './auth-fetch'; // a real module in this dir; ensures resolution works

test('jsdom renders a React element', () => {
    render(<div>hello-jsdom</div>);
    expect(screen.getByText('hello-jsdom')).toBeTruthy();
});
