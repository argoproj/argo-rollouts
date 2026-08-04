import * as React from 'react';

import {Space, Typography} from 'antd';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {faMagnifyingGlassChart} from '@fortawesome/free-solid-svg-icons';

import StatusIndicator from '../status-indicator/status-indicator';
import {AnalysisStatus, FunctionalStatus} from '../types';

import classNames from 'classnames/bind';
import './header.scss';

const {Text, Title} = Typography;
const cx = classNames;

interface HeaderProps {
    className?: string[] | string;
    status: AnalysisStatus;
    substatus?: FunctionalStatus.ERROR | FunctionalStatus.WARNING;
    subtitle?: string;
    title: string;
}

const Header = ({className, status, substatus, subtitle, title}: HeaderProps) => (
    <Space className={cx(className)} size='small' align='start'>
        <StatusIndicator size='large' status={status} substatus={substatus}>
            <FontAwesomeIcon icon={faMagnifyingGlassChart} className={cx('icon', 'fa')} />
        </StatusIndicator>
        <div>
            <Title level={4} className={cx('title')}>
                {title}
            </Title>
            {subtitle && <Text type='secondary'>{subtitle}</Text>}
        </div>
    </Space>
);

export default Header;
