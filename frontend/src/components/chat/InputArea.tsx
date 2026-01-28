import React, { useState } from 'react';
import { Input, Button, Upload } from 'antd';
import { SendOutlined, PaperClipOutlined, PictureOutlined } from '@ant-design/icons';

interface InputAreaProps {
    onSend: (content: string, type: 'text' | 'image') => void;
}

const { TextArea } = Input;

const InputArea: React.FC<InputAreaProps> = ({ onSend }) => {
    const [value, setValue] = useState('');

    const handleSend = () => {
        if (!value.trim()) return;
        onSend(value, 'text');
        setValue('');
    };

    const handleKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            handleSend();
        }
    };

    return (
        <div style={{ borderTop: '1px solid #f0f0f0', background: '#fff', padding: '10px' }}>
            <div style={{ marginBottom: '8px', display: 'flex', gap: '8px' }}>
                <Button type="text" icon={<PictureOutlined />} />
                <Button type="text" icon={<PaperClipOutlined />} />
            </div>
            <TextArea
                value={value}
                onChange={(e) => setValue(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder="Type a message..."
                autoSize={{ minRows: 2, maxRows: 6 }}
                style={{ border: 'none', resize: 'none', boxShadow: 'none' }}
            />
            <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '8px' }}>
                <Button type="primary" onClick={handleSend} icon={<SendOutlined />}>
                    Send
                </Button>
            </div>
        </div>
    );
};

export default InputArea;
