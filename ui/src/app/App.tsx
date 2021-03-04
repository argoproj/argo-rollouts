import * as React from 'react';
import './App.scss';
import {Header} from './components/header/header';
import {RolloutsList} from './components/rollouts-list/rollouts-list';

const App = () => (
    <div className='rollouts'>
        <Header />
        <RolloutsList />
    </div>
);

export default App;
