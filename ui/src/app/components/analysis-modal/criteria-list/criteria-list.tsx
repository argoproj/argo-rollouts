import * as React from 'react';
import {Space, Typography} from 'antd';

import {AnalysisStatus} from '../types';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';

import {faCheck, faRotateRight, faXmark} from '@fortawesome/free-solid-svg-icons';

import classNames from 'classnames';
import './criteria-list.scss';

const {Text} = Typography;

enum CriterionStatus {
    Fail = 'FAIL',
    Pass = 'PASS',
    InProgress = 'IN_PROGRESS',
    Pending = 'PENDING',
}

const defaultCriterionStatus = (analysisStatus: AnalysisStatus) => (analysisStatus === AnalysisStatus.Pending ? CriterionStatus.Pending : CriterionStatus.InProgress);

const criterionLabel = (measurementLabel: string, maxAllowed: number) => (maxAllowed === 0 ? `No ${measurementLabel}.` : `Fewer than ${maxAllowed + 1} ${measurementLabel}.`);

interface CriteriaListItemProps {
    children: React.ReactNode;
    showIcon: boolean;
    status: CriterionStatus;
}

const CriteriaListItem = ({children, showIcon, status}: CriteriaListItemProps) => {
    let StatusIcon: React.ReactNode | null = null;
    switch (status) {
        case CriterionStatus.Fail: {
            StatusIcon = <FontAwesomeIcon icon={faXmark} className={classNames('icon-fail')} />;
            break;
        }
        case CriterionStatus.Pass: {
            StatusIcon = <FontAwesomeIcon icon={faCheck} className={classNames('icon-pass')} />;
            break;
        }
        case CriterionStatus.InProgress: {
            StatusIcon = <FontAwesomeIcon icon={faRotateRight} className={classNames('icon-pending')} />;
            break;
        }
        case CriterionStatus.Pending:
        default: {
            break;
        }
    }

    return (
        <li>
            <Space size='small'>
                {showIcon && <>{StatusIcon}</>}
                {children}
            </Space>
        </li>
    );
};

interface CriteriaListProps {
    analysisStatus: AnalysisStatus;
    className?: string[] | string;
    consecutiveErrors: number;
    consecutiveSuccesses: number;
    failures: number;
    inconclusives: number;
    maxConsecutiveErrors: number;
    maxFailures: number;
    maxInconclusives: number;
    consecutiveSuccessLimit: number;
    showIcons: boolean;
}

const CriteriaList = ({
    analysisStatus,
    className,
    consecutiveErrors,
    consecutiveSuccesses,
    failures,
    inconclusives,
    maxConsecutiveErrors,
    maxFailures,
    maxInconclusives,
    consecutiveSuccessLimit,
    showIcons,
}: CriteriaListProps) => {
    let failureStatus = defaultCriterionStatus(analysisStatus);
    let errorStatus = defaultCriterionStatus(analysisStatus);
    let inconclusiveStatus = defaultCriterionStatus(analysisStatus);
    let successStatus = defaultCriterionStatus(analysisStatus);

    if (analysisStatus !== AnalysisStatus.Pending && analysisStatus !== AnalysisStatus.Running) {
        failureStatus = failures <= maxFailures ? CriterionStatus.Pass : CriterionStatus.Fail;
        errorStatus = consecutiveErrors <= maxConsecutiveErrors ? CriterionStatus.Pass : CriterionStatus.Fail;
        inconclusiveStatus = inconclusives <= maxInconclusives ? CriterionStatus.Pass : CriterionStatus.Fail;
        successStatus = consecutiveSuccesses >= consecutiveSuccessLimit ? CriterionStatus.Pass : CriterionStatus.Fail;
    }

    return (
        <ul className={classNames('criteria-list', className)}>
            {consecutiveSuccessLimit > 0 && (
                <CriteriaListItem status={successStatus} showIcon={showIcons}>
                    <Text>{`Exactly ${consecutiveSuccessLimit} consecutive successes.`}</Text>
                </CriteriaListItem>
            )}
            {maxFailures > -1 && (
                <CriteriaListItem status={failureStatus} showIcon={showIcons}>
                    <Text>{criterionLabel('measurement failures', maxFailures)}</Text>
                </CriteriaListItem>
            )}
            {maxConsecutiveErrors > -1 && (
                <CriteriaListItem status={errorStatus} showIcon={showIcons}>
                    <Text>{criterionLabel('consecutive measurement errors', maxConsecutiveErrors)}</Text>
                </CriteriaListItem>
            )}
            {maxInconclusives > -1 && (
                <CriteriaListItem status={inconclusiveStatus} showIcon={showIcons}>
                    <Text>{criterionLabel('inconclusive measurements', maxInconclusives)}</Text>
                </CriteriaListItem>
            )}
        </ul>
    );
};

export default CriteriaList;
