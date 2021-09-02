import * as React from 'react';
import {RolloutWidget} from '../app/components/rollout/rollout';
import {ObjectMeta, TypeMeta} from '../models/kubernetes';
import {RolloutRolloutInfo} from '../models/rollout/generated';

export type State = TypeMeta & {metadata: ObjectMeta} & {status: any; spec: any};

const parseInfoFromResourceNode = (tree: any, resource: State): RolloutRolloutInfo => {
    const ro: RolloutRolloutInfo = {};
    const {spec, status, metadata} = resource;
    ro.objectMeta = metadata as any;
    ro.replicaSets = parseReplicaSets(tree, resource);

    if (spec.strategy.canary) {
        ro.strategy = 'Canary';
        const steps = spec.strategy?.canary?.steps || [];

        if (status.currentStepIndex && steps.length > 0) {
            ro.step = `${status.currentStepIndex}/${steps.length}`;
            ro.steps = steps;
        }

        const {currentStep, currentStepIndex} = parseCurrentCanaryStep(resource);
        ro.setWeight = parseCurrentSetWeight(resource, currentStepIndex);

        ro.actualWeight = '0';

        if (!currentStep) {
            ro.actualWeight = '100';
        } else if (status.availableReplicas > 0) {
            if (!spec.strategy.canary.trafficRouting) {
                for (const rs of ro.replicaSets) {
                    if (rs.canary) {
                        ro.actualWeight = `${rs.available / status.availableReplicas}`;
                    }
                }
            } else {
                ro.actualWeight = ro.setWeight;
            }
        }
    } else {
        ro.strategy = 'BlueGreen';
    }

    ro.containers = [];
    for (const c of spec.template?.spec?.containers) {
        ro.containers.push({name: c.name, image: c.image});
    }

    ro.current = status.replicas;
    ro.updated = status.updatedReplicas;
    ro.available = status.availableReplicas;
    console.log(ro);
    return ro;
};

const parseCurrentCanaryStep = (resource: State): {currentStep: any; currentStepIndex: number} => {
    const {status, spec} = resource;
    const canary = spec.strategy?.canary;
    if (!canary || canary.steps.length === 0) {
        return null;
    }
    let currentStepIndex = 0;
    if (status.currentStepIndex) {
        currentStepIndex = status.currentStepIndex;
    }
    if (canary?.steps?.length <= currentStepIndex) {
        return {currentStep: null, currentStepIndex};
    }
    const currentStep = canary?.steps[currentStepIndex];
    return {currentStep, currentStepIndex};
};

const parseCurrentSetWeight = (resource: State, currentStepIndex: number) => {
    const {status, spec} = resource;
    if (status.abort) {
        return 0;
    }

    for (let i = currentStepIndex; i >= 0; i--) {
        const step = spec.strategy?.canary?.steps[i];
        if (step?.setWeight) {
            return step.setWeight;
        }
    }
    return 0;
};

const parseRevision = (rs: any) => {
    for (const item of rs.info || []) {
        if (item.name === 'Revision') {
            const parts = item.value.split(':') || [];
            return parts.length == 2 ? parts[1] : '0';
        }
    }
};

const parsePodStatus = (pod: any) => {
    for (const item of pod.info || []) {
        if (item.name === 'Status Reason') {
            return item.value;
        }
    }
};

const parseReplicaSets = (tree: any, rollout: any) => {
    const allReplicaSets = [];
    const allPods = [];
    for (const node of tree.nodes) {
        if (node.kind === 'ReplicaSet') {
            allReplicaSets.push(node);
        } else if (node.kind === 'Pod') {
            allPods.push(node);
        }
    }
    const ownedReplicaSets: {[key: string]: any} = {};

    for (const rs of allReplicaSets) {
        for (const parentRef of rs.parentRefs) {
            if (parentRef?.kind === 'Rollout' && parentRef?.name === rollout?.metadata?.name) {
                rs.pods = [];
                rs.objectMeta = {
                    name: rs.name,
                    uid: rs.uid,
                };
                rs.revision = parseRevision(rs);
                ownedReplicaSets[rs?.name] = rs;
            }
        }
    }

    const podMap: {[key: string]: any[]} = {};

    for (const pod of allPods) {
        pod.objectMeta = {
            name: pod.name,
            uid: pod.uid,
        };
        pod.status = parsePodStatus(pod);
        for (const parentRef of pod.parentRefs) {
            const pods = podMap[parentRef?.name] || [];
            if (parentRef.kind === 'ReplicaSet' && pods?.length > -1) {
                pods.push(pod);
                podMap[parentRef?.name] = [...pods];
            }
        }
    }

    return (Object.values(ownedReplicaSets) || []).map((rs) => {
        rs.pods = podMap[rs.name] || [];
        return rs;
    });
};

interface ApplicationResourceTree {}
export const Extension = (props: {tree: ApplicationResourceTree; resource: State}) => {
    const ro = parseInfoFromResourceNode(props.tree, props.resource);
    return <RolloutWidget rollout={ro} />;
};

export const component = Extension;
