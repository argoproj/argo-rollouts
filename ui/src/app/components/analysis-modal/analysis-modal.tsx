import * as React from 'react';
import {Modal} from 'antd';
import {RolloutAnalysisRunInfo} from '../../../models/rollout/generated';
import {AnalysisWidget} from './analysis-widget';

import './styles.scss';

interface AnalysisModalProps {
    analysis: RolloutAnalysisRunInfo;
    analysisName: string;
    images: string[];
    onClose: () => void;
    open: boolean;
    revision: string;
}

export const AnalysisModal = ({analysis, analysisName, images, onClose, open, revision}: AnalysisModalProps) => {
    return (
        <Modal centered open={open} title={analysisName} onCancel={onClose} width={866} footer={null}>
            <AnalysisWidget analysis={analysis} images={images} revision={revision} />
        </Modal>
    );
};
