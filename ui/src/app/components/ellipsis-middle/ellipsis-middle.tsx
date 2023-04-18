import * as React from 'react';
import {Typography} from 'antd';

const {Text} = Typography;
export const EllipsisMiddle: React.FC<{suffixCount: number; children: string; style: React.CSSProperties}> = ({suffixCount, children, style}) => {
    const start = children.slice(0, children.length - suffixCount).trim();
    const suffix = children.slice(-suffixCount).trim();
    return (
        <Text style={{...style, maxWidth: '100%'}} ellipsis={{suffix}}>
            {start}
        </Text>
    );
};
