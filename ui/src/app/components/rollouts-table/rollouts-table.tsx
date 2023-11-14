import * as React from 'react';
import {useHistory} from 'react-router-dom';
import {Tooltip, Table, TablePaginationConfig} from 'antd';
import {Key, KeybindingContext} from 'react-keyhooks';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {IconDefinition} from '@fortawesome/fontawesome-svg-core';
import {faStar as faStarSolid} from '@fortawesome/free-solid-svg-icons';
import {faStar as faStarOutline} from '@fortawesome/free-regular-svg-icons/faStar';

import {RolloutAction, RolloutActionButton} from '../rollout-actions/rollout-actions';
import {RolloutStatus, StatusIcon} from '../status-icon/status-icon';
import {ReplicaSetStatus, ReplicaSetStatusIcon} from '../status-icon/status-icon';
import {RolloutInfo} from '../../../models/rollout/rollout';
import {InfoItemKind, InfoItemRow} from '../info-item/info-item';
import { AlignType } from 'rc-table/lib/interface';
import './rollouts-table.scss';

export const RolloutsTable = ({
    rollouts,
    onFavoriteChange,
    favorites,
}: {
    rollouts: RolloutInfo[];
    onFavoriteChange: (rolloutName: string, isFavorite: boolean) => void;
    favorites: {[key: string]: boolean};
}) => {
    const tableRef = React.useRef(null);

    const handleFavoriteChange = (rolloutName: string, isFavorite: boolean) => {
        onFavoriteChange(rolloutName, isFavorite);
    };
    const data = rollouts
        .map((rollout) => {
            return {
                ...rollout,
                key: rollout.objectMeta?.uid,
                favorite: favorites[rollout.objectMeta?.name] || false,
            };
        })
        .sort((a, b) => {
            if (a.favorite && !b.favorite) {
                return -1;
            } else if (!a.favorite && b.favorite) {
                return 1;
            } else {
                return 0;
            }
        });

    const columns = [
        {
            dataIndex: 'favorite',
            key: 'favorite',
            render: (favorite: boolean, rollout: RolloutInfo) => {
                return favorite ? (
                    <button
                        onClick={(e) => {
                            e.stopPropagation();
                            handleFavoriteChange(rollout.objectMeta?.name, false);
                        }}
                        style={{cursor: 'pointer'}}
                    >
                        <FontAwesomeIcon icon={faStarSolid} size='lg' />
                    </button>
                ) : (
                    <button
                        onClick={(e) => {
                            e.stopPropagation();
                            handleFavoriteChange(rollout.objectMeta?.name, true);
                        }}
                        style={{cursor: 'pointer'}}
                    >
                        <FontAwesomeIcon icon={faStarOutline as IconDefinition} size='lg' />
                    </button>
                );
            },
            width: 50,
        },
        {
            title: 'Name',
            dataIndex: 'objectMeta',
            key: 'name',
            width: 300,
            render: (objectMeta: {name?: string}) => objectMeta.name,
            sorter: (a: any, b: any) => a.objectMeta.name.localeCompare(b.objectMeta.name),
        },
        {
            title: 'Strategy',
            dataIndex: 'strategy',
            key: 'strategy',
            align: 'left' as AlignType,
            sorter: (a: any, b: any) => a.strategy.localeCompare(b.strategy),
            render: (strategy: string) => {
                return (
                    <InfoItemRow 
                        label={false} 
                        items={{
                            content: strategy, 
                            icon: strategy === 'BlueGreen' ? 'fa-palette' : 'fa-dove', 
                            kind: strategy.toLowerCase() as InfoItemKind
                        }}
                        style={{marginLeft: '0px', paddingLeft: '0px'}}
                    />
                );
            },
        },
        {
            title: 'Step',
            dataIndex: 'step',
            key: 'step',
            render: (text: any, record: {step?: string}) => record.step || '-',
            sorter: (a: any, b: any) => {
                if (a.step === undefined) {
                    return -1;
                }
                if (b.step === undefined) {
                    return 1;
                } else return a.step.localeCompare(b.step);
            },
        },
        {
            title: 'Weight',
            dataIndex: 'setWeight',
            key: 'weight',
            render: (text: any, record: {setWeight?: number}) => record.setWeight || '-',
            sorter: (a: any, b: any) => a.setWeight - b.setWeight,
        },
        {
            title: 'ReplicaSets',
            key: 'replicasets',
            width: 200,
            sorter: (a: RolloutInfo, b: RolloutInfo) => a.desired - b.desired,
            render: (rollout: RolloutInfo) => {
                const stableReplicaSets = rollout.replicaSets?.filter((rs) => rs.stable);
                const canaryReplicaSets = rollout.replicaSets?.filter((rs) => rs.canary);
                const previewReplicaSets = rollout.replicaSets?.filter((rs) => rs.preview);
                return (
                    <div>
                        {stableReplicaSets?.length > 0 && (
                            <div>
                                Stable:{' '}
                                {stableReplicaSets.map((rs) => (
                                    <React.Fragment key={rs.objectMeta?.name}>
                                        <Tooltip title={rs.objectMeta?.name}>
                                            Rev {rs.revision} ({rs.available}/{rs.replicas}) <ReplicaSetStatusIcon status={rs.status as ReplicaSetStatus} />
                                        </Tooltip>
                                    </React.Fragment>
                                ))}
                            </div>
                        )}
                        {canaryReplicaSets?.length > 0 && (
                            <div>
                                Canary:{' '}
                                {canaryReplicaSets.map((rs) => (
                                    <React.Fragment key={rs.objectMeta?.name}>
                                        <Tooltip title={rs.objectMeta?.name}>
                                            Rev {rs.revision} ({rs.available}/{rs.replicas}) <ReplicaSetStatusIcon status={rs.status as ReplicaSetStatus} />
                                        </Tooltip>
                                    </React.Fragment>
                                ))}
                            </div>
                        )}
                        {previewReplicaSets?.length > 0 && (
                            <div>
                                Preview:{' '}
                                {previewReplicaSets.map((rs) => (
                                    <React.Fragment key={rs.objectMeta?.name}>
                                        <Tooltip title={rs.objectMeta?.name}>
                                            Rev {rs.revision} ({rs.available}/{rs.replicas}) <ReplicaSetStatusIcon status={rs.status as ReplicaSetStatus} />
                                        </Tooltip>
                                    </React.Fragment>
                                ))}
                            </div>
                        )}
                    </div>
                );
            },
        },
        {
            title: 'Status',
            sorter: (a: any, b: any) => a.status.localeCompare(b.status),
            render: (record: {message?: string; status?: string}) => {
                return (
                    <div>
                        <Tooltip title={record.message}>
                            {record.status} <StatusIcon status={record.status as RolloutStatus} />
                        </Tooltip>
                    </div>
                );
            },
        },
        {
            title: 'Actions',
            dataIndex: 'actions',
            key: 'actions',
            render: (text: any, rollout: {objectMeta?: {name?: string}}) => {
                return (
                    <div className='rollouts-table_widget_actions'>
                        <div className='rollouts-table_widget_actions_button'>
                            <RolloutActionButton action={RolloutAction.Restart} rollout={rollout} callback={() => {}} indicateLoading />
                        </div>
                        <div className='rollouts-table_widget_actions_button'>
                            <RolloutActionButton action={RolloutAction.Promote} rollout={rollout} callback={() => {}} indicateLoading />
                        </div>
                        <div className='rollouts-table_widget_actions_button'>
                            <RolloutActionButton action={RolloutAction.Abort} rollout={rollout} callback={() => {}} indicateLoading />
                        </div>
                        <div className='rollouts-table_widget_actions_button'>
                            <RolloutActionButton action={RolloutAction.Retry} rollout={rollout} callback={() => {}} indicateLoading />
                        </div>
                    </div>
                );
            },
        },
    ];

    const history = useHistory();
    const [selectedRow, setSelectedRow] = React.useState<number>(undefined);
    const {useKeybinding} = React.useContext(KeybindingContext);
    useKeybinding(Key.UP, () => {
        if (selectedRow === undefined) {
            setSelectedRow(itemsPerPage - 1);
            return true;
        } else if (selectedRow > 0) {
            setSelectedRow(selectedRow - 1);
            return true;
        }
        return false;
    });
    useKeybinding(Key.DOWN, () => {
        if (selectedRow === undefined) {
            setSelectedRow(0);
            return true;
        } else if (selectedRow < itemsPerPage - 1) {
            setSelectedRow(selectedRow + 1);
            return true;
        }
        return false;
    });
    useKeybinding(Key.ENTER, () => {
        if (selectedRow !== undefined) {
            history.push(`/rollout/${data[selectedRow].objectMeta?.name}`);
            return true;
        }
        return false;
    });
    useKeybinding(Key.ESCAPE, () => {
        setSelectedRow(undefined);
        return false; // let the toolbar handle clearing the search bar
    });

    const [itemsPerPage, setItemsPerPage] = React.useState(10);
    const handlePaginationChange = (pagination: TablePaginationConfig) => {
        setItemsPerPage(pagination.pageSize);
    };

    return (
        <Table
            className='rollouts-table'
            columns={columns}
            dataSource={data}
            onRow={(record: RolloutInfo, index: number) => ({
                className: selectedRow === index ? 'rollouts-table__row__selected' : '',
                onClick: () => {
                    history.push(`/rollout/${record.objectMeta?.name}`);
                },
                style: {cursor: 'pointer'},
            })}
            pagination={
                {
                    pageSize: itemsPerPage,
                    onChange: handlePaginationChange,
                } as TablePaginationConfig
            }
            ref={tableRef}
            rowClassName='rollouts-table__row'
            rowKey={(_, index) => index}
            style={{width: '100%', padding: '20px 20px'}}
        />
    );
};
