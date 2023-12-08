import {
    GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1Measurement,
    GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1Metric,
    GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1MetricResult,
} from '../../../models/rollout/generated';

export enum AnalysisStatus {
    Successful = 'Successful',
    Error = 'Error',
    Failed = 'Failed',
    Running = 'Running',
    Pending = 'Pending',
    Inconclusive = 'Inconclusive',
    Unknown = 'Unknown', // added by frontend
}

export enum FunctionalStatus {
    ERROR = 'ERROR',
    INACTIVE = 'INACTIVE',
    IN_PROGRESS = 'IN_PROGRESS',
    SUCCESS = 'SUCCESS',
    WARNING = 'WARNING',
}

export type TransformedMetricStatus = GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1MetricResult & {
    adjustedPhase: AnalysisStatus;
    chartable: boolean;
    chartMax: number | null;
    chartMin: number;
    statusLabel: string;
    substatus?: FunctionalStatus.ERROR | FunctionalStatus.WARNING;
    transformedMeasurements: TransformedMeasurement[];
};

export type TransformedMetricSpec = GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1Metric & {
    failConditionLabel: string | null;
    failThresholds: number[] | null;
    queries?: string[];
    successConditionLabel: string | null;
    successThresholds: number[] | null;
    conditionKeys: string[];
};

export type TransformedMetric = {
    name: string;
    spec?: TransformedMetricSpec;
    status: TransformedMetricStatus;
};

export type TransformedValueObject = {
    [key: string]: number | string | null;
};

export type TransformedMeasurement = GithubComArgoprojArgoRolloutsPkgApisRolloutsV1alpha1Measurement & {
    chartValue?: TransformedValueObject | number | string | null;
    tableValue: TransformedValueObject | number | string | null;
};

export type MeasurementSetInfo = {
    chartable: boolean;
    max: number | null;
    measurements: TransformedMeasurement[];
    min: number;
};

export type MeasurementValueInfo = {
    canChart: boolean;
    chartValue?: TransformedValueObject | number | string | null;
    tableValue: TransformedValueObject | number | string | null;
};
