import React, { useEffect, useRef } from 'react';
import { List, Avatar, Typography } from 'antd';
import { UserOutlined } from '@ant-design/icons';
import { Message } from '../../types/models';
import { useAuthStore } from '../../stores/authStore';

interface MessageListProps {
    messages: Message[];
}

const MessageList: React.FC<MessageListProps> = ({ messages }) => {
    const user = useAuthStore((state) => state.user);
    const bottomRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
    }, [messages]);

    return (
        <div style={{ flex: 1, overflowY: 'auto', padding: '20px', background: '#f5f5f5' }}>
            <List
                dataSource={messages}
                split={false}
                renderItem={(msg) => {
                    const isMe = msg.from_user_id === user?.id;
                    return (
                        <List.Item style={{ 
                            display: 'flex', 
                            justifyContent: isMe ? 'flex-end' : 'flex-start',
                            border: 'none',
                            padding: '4px 0'
                        }}>
                            <div style={{ 
                                display: 'flex', 
                                flexDirection: isMe ? 'row-reverse' : 'row',
                                maxWidth: '70%',
                                alignItems: 'flex-start'
                            }}>
                                <Avatar icon={<UserOutlined />} style={{ margin: isMe ? '0 0 0 8px' : '0 8px 0 0' }} />
                                <div style={{
                                    background: isMe ? '#1890ff' : '#fff',
                                    color: isMe ? '#fff' : '#000',
                                    padding: '8px 12px',
                                    borderRadius: '8px',
                                    boxShadow: '0 1px 2px rgba(0,0,0,0.1)',
                                    wordBreak: 'break-word'
                                }}>
                                    {msg.content}
                                </div>
                            </div>
                        </List.Item>
                    );
                }}
            />
            <div ref={bottomRef} />
        </div>
    );
};

export default MessageList;
