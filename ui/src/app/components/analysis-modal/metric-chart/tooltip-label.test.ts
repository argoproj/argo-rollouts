import {getTooltipLabel} from './tooltip-label';
import {AnalysisStatus} from '../types';

const valueFormatter = (value: number | string | null) => (value === null ? '' : value.toString());

describe('getTooltipLabel()', () => {
    test.each([
        ['single condition key with a value', {phase: AnalysisStatus.Successful, chartValue: {0: 5}}, ['0'], '5'],
        ['multiple condition keys with values', {phase: AnalysisStatus.Successful, chartValue: {0: 5, 1: 10}}, ['0', '1'], '5 (0) , 10 (1)'],
        ['no condition keys with a scalar value', {phase: AnalysisStatus.Successful, chartValue: 5}, [], '5'],
        ['error phase with a message', {phase: AnalysisStatus.Error, message: 'boom'}, ['0'], 'boom'],
        ['error phase without a message', {phase: AnalysisStatus.Error}, ['0'], 'Measurement error'],
        ['null chartValue with a single condition key', {phase: AnalysisStatus.Successful, chartValue: null}, ['0'], ''],
        ['null chartValue with multiple condition keys', {phase: AnalysisStatus.Successful, chartValue: null}, ['0', '1'], ' (0) ,  (1)'],
        ['null chartValue with no condition keys', {phase: AnalysisStatus.Successful, chartValue: null}, [], ''],
        ['object chartValue missing the requested key', {phase: AnalysisStatus.Successful, chartValue: {0: 5}}, ['1'], ''],
    ] as [string, {phase: AnalysisStatus; message?: string; chartValue?: any}, string[], string][])('for %s', (_label, data, conditionKeys, expected) => {
        expect(getTooltipLabel(data, conditionKeys, valueFormatter)).toBe(expected);
    });
});
