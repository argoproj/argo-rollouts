export type Time = string;

export interface ObjectMeta {
    name?: string;
    generateName?: string;
    namespace?: string;
    selfLink?: string;
    uid?: string;
    resourceVersion?: string;
    generation?: number;
    creationTimestamp?: Time;
    deletionTimestamp?: Time;
    deletionGracePeriodSeconds?: number;
    labels?: {[name: string]: string};
    annotations?: {[name: string]: string};
    ownerReferences?: any[];
    initializers?: any;
    finalizers?: string[];
    clusterName?: string;
}

export interface TypeMeta {
    kind?: string;
    apiVersion?: string;
}