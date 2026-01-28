import React, { useState } from 'react';
import { Layout, Menu, Input, List, Avatar, Badge, Tabs } from 'antd';
import { UserOutlined, TeamOutlined, SearchOutlined } from '@ant-design/icons';
import { useChatStore } from '../../stores/chatStore';

const { Sider } = Layout;
const { Search } = Input;

const Sidebar: React.FC = () => {
    const { friends, groups, setActiveConversation, activeConversationId } = useChatStore();
    const [activeTab, setActiveTab] = useState('friends');

    const items = activeTab === 'friends' 
        ? friends.map(f => ({ id: f.id, name: f.username, avatar: f.avatarURL, type: 'friend' }))
        : groups.map(g => ({ id: g.id, name: g.name, avatar: null, type: 'group' }));

    return (
        <Sider width={300} theme="light" style={{ borderRight: '1px solid #f0f0f0', display: 'flex', flexDirection: 'column' }}>
            <div style={{ padding: '16px' }}>
                <Search placeholder="Search" prefix={<SearchOutlined />} />
            </div>
            
            <Tabs 
                defaultActiveKey="friends" 
                centered 
                onChange={setActiveTab}
                items={[
                    { key: 'friends', label: 'Friends', icon: <UserOutlined /> },
                    { key: 'groups', label: 'Groups', icon: <TeamOutlined /> },
                ]}
            />

            <div style={{ flex: 1, overflowY: 'auto' }}>
                <List
                    itemLayout="horizontal"
                    dataSource={items}
                    renderItem={(item) => (
                        <List.Item 
                            style={{ 
                                padding: '12px 16px', 
                                cursor: 'pointer',
                                background: activeConversationId === item.id ? '#e6f7ff' : 'transparent'
                            }} 
                            onClick={() => setActiveConversation(item.id)}
                        >
                            <List.Item.Meta
                                avatar={
                                    <Badge count={0} dot> {/* Placeholder for unread */}
                                        <Avatar icon={item.type === 'friend' ? <UserOutlined /> : <TeamOutlined />} src={item.avatar} />
                                    </Badge>
                                }
                                title={item.name}
                                description={item.type === 'group' ? 'Group Chat' : 'Online'}
                            />
                        </List.Item>
                    )}
                />
            </div>
        </Sider>
    );
};

export default Sidebar;
