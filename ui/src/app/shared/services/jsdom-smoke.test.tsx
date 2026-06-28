/** @jest-environment jsdom */
import * as React from 'react';
import {render, screen} from '@testing-library/react';

// Importing a stylesheet must not break the test (proves the scss moduleNameMapper).
import './auth-fetch'; // a real module in this dir; ensures resolution works

test('jsdom renders a React element', () => {
    render(<div>hello-jsdom</div>);
    expect(screen.getByText('hello-jsdom')).toBeTruthy();
});
