import * as moment from 'moment';
import * as React from 'react';
import {interval, Subscription} from 'rxjs';

export class Ticker extends React.Component<{intervalMs?: number; disabled?: boolean; children?: (time: moment.Moment) => React.ReactNode}, {time: moment.Moment}> {
    private subscription: Subscription | null = null;

    constructor(props: {intervalMs?: number; children?: (time: moment.Moment) => React.ReactNode}) {
        super(props);
        this.state = {time: moment()};
        this.ensureSubscribed();
    }

    public render() {
        return this.props.children && this.props.children(this.state.time);
    }

    public componentDidUpdate() {
        this.ensureSubscribed();
    }

    public componentWillUnmount() {
        this.ensureUnsubscribed();
    }

    private ensureSubscribed() {
        if (this.props.disabled) {
            this.ensureUnsubscribed();
        } else if (!this.subscription) {
            this.subscription = interval(this.props.intervalMs || 1000).subscribe(() => this.setState({time: moment()}));
        }
    }

    private ensureUnsubscribed() {
        if (this.subscription != null) {
            this.subscription.unsubscribe();
            this.subscription = null;
        }
    }
}
