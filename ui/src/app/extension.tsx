import * as React from 'react';
import {RolloutExtension} from './components/rollout/rollout';

export const Extension = (props: {name: string; namespace: string}) => {
    return <RolloutExtension name={props.name} namespace={props.namespace} />;
};

export default Extension;
