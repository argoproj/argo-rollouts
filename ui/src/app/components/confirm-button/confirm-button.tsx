import * as React from 'react';

import {Button, Popconfirm, Tooltip} from 'antd';
import {ButtonProps} from 'antd/es/button/button';
import {useState} from 'react';

interface ConfirmButtonProps extends ButtonProps {
    skipconfirm?: boolean;
    tooltip?: string;
}

export const ConfirmButton = (props: ConfirmButtonProps) => {
    const [open, setOpen] = useState(false);
    const [buttonProps, setButtonProps] = useState(props);

    React.useEffect(() => {
        const tmp = {...props};
        delete tmp.skipconfirm;
        delete tmp.children;
        delete tmp.onClick;
        setButtonProps(tmp);
    }, [props]);

    const confirm = () => {
        setOpen(false);
        if (props.onClick) {
            props.onClick(null);
        }
    };

    const cancel = () => {
        setOpen(false);
    };

    const handleOpenChange = (newOpen: boolean) => {
        if (!newOpen) {
            setOpen(newOpen);
            return;
        }
        if (props.skipconfirm) {
            confirm(); // next step
        } else {
            setOpen(newOpen);
        }
    };

    return (
        <Popconfirm title='Are you sure?' open={open && !props.disabled} onConfirm={confirm} onCancel={cancel} okText='Yes' cancelText='No' onOpenChange={handleOpenChange}>
            <Tooltip title={props.tooltip}>
                <Button {...buttonProps}>{props.children}</Button>
            </Tooltip>
        </Popconfirm>
    );
};
