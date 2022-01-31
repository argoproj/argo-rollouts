import {ActionButton, Autocomplete, InfoItem, ThemeDiv, useInput} from 'argo-ui/v2';
import * as React from 'react';
import {RolloutContainerInfo} from '../../../models/rollout/generated';
import {ImageInfo, ReactStatePair} from './rollout';

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
                <ThemeDiv className='info__title' style={{marginBottom: '0'}}>
                    Containers
                </ThemeDiv>

                {interactive &&
                    (interactive?.editState[0] ? (
                        <div style={{marginLeft: 'auto', display: 'flex', alignItems: 'center'}}>
                            <ActionButton
                                icon='fa-times'
                                action={() => {
                                    setEditing(false);
                                    setError(false);
                                }}
                            />
                            <ActionButton
                                label={error ? 'ERROR' : 'SAVE'}
                                style={{marginRight: 0}}
                                icon={error ? 'fa-exclamation-circle' : 'fa-save'}
                                action={() => {
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
                                }}
                                shouldConfirm
                                indicateLoading={!error}
                            />
                        </div>
                    ) : (
                        <i className='fa fa-pencil-alt' onClick={() => setEditing(true)} style={{cursor: 'pointer', marginLeft: 'auto'}} />
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
                <ThemeDiv className='containers__few'>
                    <span style={{marginRight: '5px'}}>
                        <i className='fa fa-boxes' />
                    </span>
                    Add more containers to fill this space!
                </ThemeDiv>
            )}
        </React.Fragment>
    );
};

const ContainerWidget = (props: {container: RolloutContainerInfo; images: ImageInfo[]; setInput: (image: string) => void; editing: boolean}) => {
    const {container, editing} = props;
    const [, , newImageInput] = useInput(container.image, (val) => props.setInput(val));

    return (
        <div style={{margin: '1em 0', display: 'flex', alignItems: 'center', whiteSpace: 'nowrap'}}>
            <div style={{paddingRight: '20px'}}>{container.name}</div>
            <div style={{width: '100%', display: 'flex', alignItems: 'center', height: '2em', minWidth: 0}}>
                {!editing ? (
                    <InfoItem content={container.image} truncate={true} />
                ) : (
                    <Autocomplete items={props.images.map((img) => img.image)} placeholder='New Image' {...newImageInput} />
                )}
            </div>
        </div>
    );
};
