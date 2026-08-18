export const durationToSeconds = (duration: string): number => {
    if (!duration) {
        return 0;
    }
    const match = duration.match(/^(\d+)(s|m|h)?$/);
    if (!match) {
        return 0;
    }
    const value = parseInt(match[1], 10);
    switch (match[2]) {
        case 'h':
            return value * 3600;
        case 'm':
            return value * 60;
        default:
            return value;
    }
};

export interface PauseProgress {
    fillPercent: number;
    remainingMs: number;
    done: boolean;
}

export const computePauseProgress = (params: {startTimeMs: number; durationSeconds: number; nowMs: number}): PauseProgress => {
    const {startTimeMs, durationSeconds, nowMs} = params;
    const totalMs = durationSeconds * 1000;
    if (totalMs <= 0) {
        return {fillPercent: 100, remainingMs: 0, done: true};
    }
    const elapsedMs = Math.max(0, nowMs - startTimeMs);
    const fillPercent = Math.min(100, (elapsedMs / totalMs) * 100);
    const remainingMs = Math.max(0, totalMs - elapsedMs);
    return {fillPercent, remainingMs, done: elapsedMs >= totalMs};
};
