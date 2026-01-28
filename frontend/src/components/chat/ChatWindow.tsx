import React from 'react';
import { Layout, Typography, Button } from 'antd';
import { InfoCircleOutlined, MoreOutlined } from '@ant-design/icons';
import MessageList from './MessageList';
import InputArea from './InputArea';
import { useChatStore } from '../../stores/chatStore';

const { Header, Content } = Layout;
const { Title, Text } = Typography;

const ChatWindow: React.FC = () => {
    const { activeConversationId, friends, groups, messages, sendMessage } = useChatStore();

    if (!activeConversationId) {
        return (
            <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#ccc' }}>
                Select a chat to start messaging
            </div>
        );
    }

    // Resolve active chat details
    const activeFriend = friends.find(f => f.id === activeConversationId);
    const activeGroup = groups.find(g => g.id === activeConversationId);
    const title = activeFriend?.username || activeGroup?.name || 'Unknown';
    const currentMessages = messages[activeConversationId] || [];

    return (
        <Layout style={{ height: '100%' }}>
            <Header style={{ 
                background: '#fff', 
                borderBottom: '1px solid #f0f0f0', 
                padding: '0 24px', 
                display: 'flex', 
                alignItems: 'center',
                justifyContent: 'space-between'
            }}>
                <Title level={4} style={{ margin: 0 }}>{title}</Title>
                <div>
                    <Button type="text" icon={<InfoCircleOutlined />} />
                    <Button type="text" icon={<MoreOutlined />} />
                </div>
            </Header>
            <Content style={{ display: 'flex', flexDirection: 'column', background: '#fff' }}>
                <MessageList messages={currentMessages} />
                <InputArea onSend={sendMessage} />
            </Content>
        </Layout>
    );
};

export default ChatWindow;
