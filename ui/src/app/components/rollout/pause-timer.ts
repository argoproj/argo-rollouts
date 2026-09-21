export interface PauseProgress {
    fillPercent: number;
    remainingMs: number;
    done: boolean;
}

// computePauseProgress positions the pause bar for a rollout paused on a timed
// canary step.
//
// Both inputs come from the controller as whole seconds
// (RolloutInfo.pauseDurationSeconds / pauseRemainingSeconds), so the UI neither
// parses Go's duration grammar nor compares a server timestamp against the local
// clock — a wrong clock on the viewer's machine cannot skew the bar.
//
// durationSeconds is 0 when the pause is indefinite or not in progress, and -1
// when the controller could not parse the configured duration. Both yield null
// so the caller renders no bar rather than an instantly-complete one.
export const computePauseProgress = (params: {durationSeconds: number; remainingSeconds: number}): PauseProgress | null => {
    const {durationSeconds, remainingSeconds} = params;
    if (durationSeconds <= 0) {
        return null;
    }
    const remaining = Math.min(Math.max(remainingSeconds, 0), durationSeconds);
    return {
        fillPercent: ((durationSeconds - remaining) / durationSeconds) * 100,
        remainingMs: remaining * 1000,
        done: remaining <= 0,
    };
};
