import * as React from 'react';

import {Typography} from 'antd';

import classNames from 'classnames';
import './query-box.scss';

const {Paragraph} = Typography;

interface QueryBoxProps {
    className?: string[] | string;
    query: string;
}

const QueryBox = ({className, query}: QueryBoxProps) => {
    const queryTextRef = React.useRef<HTMLDivElement>(null);
    const [canExpand, setCanExpand] = React.useState<boolean>(false);
    const [expanded, toggleExpanded] = React.useState<boolean>(false);

    React.useEffect(() => {
        setCanExpand(queryTextRef.current?.offsetHeight !== queryTextRef.current?.scrollHeight);
    }, [queryTextRef]);

    const expandQuery = () => {
        toggleExpanded(true);
        setCanExpand(false);
    };

    return (
        <div
            ref={queryTextRef}
            className={classNames('query-box', canExpand && 'can-expand', expanded && 'is-expanded', className)}
            title={canExpand ? 'Click to expand query' : undefined}>
            <pre className={classNames('query')} onClick={expandQuery} onKeyDown={expandQuery}>
                {query}
            </pre>
            <Paragraph className={classNames('query-copy-button')} copyable={{text: query}} />
        </div>
    );
};

export default QueryBox;
