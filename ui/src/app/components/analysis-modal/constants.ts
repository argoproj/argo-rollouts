import {AnalysisStatus, FunctionalStatus} from './types';

export const METRIC_FAILURE_LIMIT_DEFAULT = 0;
export const METRIC_INCONCLUSIVE_LIMIT_DEFAULT = 0;
export const METRIC_CONSECUTIVE_ERROR_LIMIT_DEFAULT = 4;

export const ANALYSIS_STATUS_THEME_MAP: {[key in AnalysisStatus]: string} = {
    Successful: FunctionalStatus.SUCCESS,
    Error: FunctionalStatus.WARNING,
    Failed: FunctionalStatus.ERROR,
    Running: FunctionalStatus.IN_PROGRESS,
    Pending: FunctionalStatus.INACTIVE,
    Inconclusive: FunctionalStatus.WARNING,
    Unknown: FunctionalStatus.INACTIVE, // added by frontend
};
