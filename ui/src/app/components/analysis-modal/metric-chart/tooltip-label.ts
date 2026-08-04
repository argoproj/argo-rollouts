import {AnalysisStatus, TransformedValueObject} from '../types';

/**
 * Builds the label shown in the metric chart tooltip for a hovered measurement.
 *
 * @param data transformed measurement backing the hovered data point (chartValue is null when unchartable)
 * @param conditionKeys keys used to pull values from the measurement result
 * @param valueFormatter formats a single value for display
 * @returns label shown in the metric chart tooltip
 */
export const getTooltipLabel = (
    data: {phase?: string; message?: string; chartValue?: TransformedValueObject | number | string | null},
    conditionKeys: string[],
    valueFormatter: (value: number | string | null) => string,
): string => {
    if (data.phase === AnalysisStatus.Error) {
        return data.message ?? 'Measurement error';
    }

    if (conditionKeys.length > 0) {
        return conditionKeys
            .map((cKey) => {
                const value = (data.chartValue as TransformedValueObject | null)?.[cKey] ?? null;
                return conditionKeys.length > 1 ? `${valueFormatter(value)} (${cKey})` : valueFormatter(value);
            })
            .join(' , ');
    }

    return valueFormatter((data.chartValue ?? null) as number | string | null);
};
