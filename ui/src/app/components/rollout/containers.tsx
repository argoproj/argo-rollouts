import * as React from 'react';
import {RolloutContainerInfo} from '../../../models/rollout/generated';
import {ImageInfo, ReactStatePair} from './rollout';
import {AutoComplete, Button, Input} from 'antd';
import {ConfirmButton} from '../confirm-button/confirm-button';
import {FontAwesomeIcon} from '@fortawesome/react-fontawesome';
import {faExclamationCircle, faPencilAlt, faSave, faTimes} from '@fortawesome/free-solid-svg-icons';

interface ContainersWidgetProps {
    containers: RolloutContainerInfo[];
    images: ImageInfo[];
    interactive?: {
        editState: ReactStatePair;
        setImage: (container: string, image: string, tag: string) => void;
    };
}

export const ContainersWidget = (props: ContainersWidgetProps) => {
    const {containers, images, interactive} = props;
    const [editing, setEditing] = interactive?.editState || [null, null];
    const inputMap: {[key: string]: string} = {};
    for (const container of containers) {
        inputMap[container.name] = '';
    }
    const [inputs, setInputs] = React.useState(inputMap);
    const [error, setError] = React.useState(false);

    return (
        <React.Fragment>
            <div style={{display: 'flex', alignItems: 'center', height: '2em'}}>
                <div className='info__title' style={{marginBottom: '0'}}>
                    Containers
                </div>

                {interactive &&
                    (interactive?.editState[0] ? (
                        <div style={{marginLeft: 'auto', display: 'flex', alignItems: 'center'}}>
                            <Button
                                style={{marginRight: '10px'}}
                                danger
                                icon={<FontAwesomeIcon icon={faTimes} />}
                                onClick={() => {
                                    setEditing(false);
                                    setError(false);
                                }}
                            />
                            <ConfirmButton
                                style={{marginRight: 0}}
                                type='primary'
                                icon={<FontAwesomeIcon icon={error ? faExclamationCircle : faSave} style={{marginRight: '5px'}} />}
                                danger={error}
                                onClick={() => {
                                    for (const container of Object.keys(inputs)) {
                                        const split = inputs[container].split(':');
                                        if (split.length > 1) {
                                            const image = split[0];
                                            const tag = split[1];
                                            interactive.setImage(container, image, tag);
                                            setTimeout(() => {
                                                setEditing(false);
                                            }, 350);
                                        } else {
                                            setError(true);
                                        }
                                    }
                                }}>
                                {error ? 'ERROR' : 'SAVE'}
                            </ConfirmButton>
                        </div>
                    ) : (
                        <Button onClick={() => setEditing(true)} style={{marginLeft: 'auto'}} icon={<FontAwesomeIcon icon={faPencilAlt} style={{marginRight: '5px'}} />}>
                            Edit
                        </Button>
                    ))}
            </div>
            {containers.map((c, i) => (
                <ContainerWidget
                    key={`${c}-${i}`}
                    container={c}
                    images={images}
                    editing={editing}
                    setInput={(img) => {
                        const update = {...inputs};
                        update[c.name] = img;
                        setInputs(update);
                    }}
                />
            ))}
            {containers.length < 2 && (
                <div className='containers__few'>
                    <span style={{marginRight: '5px'}}>
                        <i className='fa fa-boxes' />
                    </span>
                </div>
            )}
        </React.Fragment>
    );
};

const ContainerWidget = (props: {container: RolloutContainerInfo; images: ImageInfo[]; setInput: (image: string) => void; editing: boolean}) => {
    const {container, editing} = props;
    const [input, setInput] = React.useState(container.image);

    const update = (val: string) => {
        setInput(val);
        props.setInput(val);
    };

    return (
        <div style={{margin: '1em 0', whiteSpace: 'nowrap'}}>
            <div style={{marginBottom: '0.5em', fontWeight: 600, fontSize: '14px'}}>{container.name}</div>
            <div style={{width: '100%', height: '2em', minWidth: 0}}>
                {!editing ? (
                    <Input value={container.image} style={{width: '100%', cursor: 'default', color: 'black'}} disabled={true} />
                ) : (
                    <AutoComplete
                        allowClear={true}
                        style={{width: '100%'}}
                        options={props.images.map((img) => {
                            return {label: img.image, value: img.image};
                        })}
                        placeholder='New Image'
                        value={input}
                        onSelect={update}
                        onChange={update}
                    />
                )}
            </div>
        </div>
    );
};
