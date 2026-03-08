import React, { useMemo, useState } from 'react';
import { Layout, Input, List, Avatar, Badge, Tabs, Button, Modal, message, Select, Tag, Tooltip, Dropdown, MenuProps } from 'antd';
import { UserOutlined, TeamOutlined, SearchOutlined, UserAddOutlined, PlusOutlined } from '@ant-design/icons';
import { useChatStore } from '../../stores/chatStore';
import { friendService } from '../../api/FriendService';

const { Sider } = Layout;
const { Search } = Input;
const { TextArea } = Input;

interface SidebarListItem {
    id: string;
    name: string;
    avatar: string | null | undefined;
    type: 'friend' | 'group';
    status?: string;
    extra?: string;
    friendUserId?: string;
    friendUserCode?: number;
}

const Sidebar: React.FC = () => {
    const {
        friends,
        groups,
        setActiveConversation,
        activeConversationId,
        friendRequests,
        fetchFriendRequests,
        fetchFriends,
        createGroup,
        inviteGroupMembers
    } = useChatStore();
    const [activeTab, setActiveTab] = useState('friends');
    const [isAddFriendModalVisible, setIsAddFriendModalVisible] = useState(false);
    const [isRequestsModalVisible, setIsRequestsModalVisible] = useState(false);
    const [isCreateGroupModalVisible, setIsCreateGroupModalVisible] = useState(false);
    const [userCode, setUserCode] = useState('');
    const [friendMessage, setFriendMessage] = useState('');
    const [isAddingFriend, setIsAddingFriend] = useState(false);
    const [isCreatingGroup, setIsCreatingGroup] = useState(false);
    const [groupName, setGroupName] = useState('');
    const [groupMemberIds, setGroupMemberIds] = useState<string[]>([]);
    const [isLoadingRequests, setIsLoadingRequests] = useState(false);
    const [respondingRequestId, setRespondingRequestId] = useState<string | null>(null);
    const [searchKeyword, setSearchKeyword] = useState('');

    const friendItems = useMemo<SidebarListItem[]>(
        () =>
            friends.map((f) => ({
                id: f.room_id,
                name: f.nickname || f.username,
                avatar: f.avatarURL,
                type: 'friend' as const,
                status: f.status,
                extra: '',
                friendUserId: f.id,
                friendUserCode: f.user_code
            })),
        [friends]
    );

    const groupItems = useMemo<SidebarListItem[]>(
        () =>
            groups.map((g) => ({
                id: g.id,
                name: g.name,
                avatar: null,
                type: 'group' as const,
                status: g.status || 'active',
                extra: `${g.member_count || 0} 人`
            })),
        [groups]
    );

    const items = (activeTab === 'friends' ? friendItems : groupItems).filter((item) =>
        item.name.toLowerCase().includes(searchKeyword.trim().toLowerCase())
    );

    const createGroupOptions = friends.map((friend) => ({
        label: friend.nickname || friend.username,
        value: friend.id
    }));
    const inviteGroupOptions = groups.filter((group) => (group.status || 'active') === 'active');

    const handleAddFriend = async () => {
        if (!userCode || isNaN(Number(userCode))) {
            message.error('请输入有效的数字用户编号');
            return;
        }

        setIsAddingFriend(true);
        try {
            await friendService.addFriend({
                target_user_code: parseInt(userCode, 10),
                message: friendMessage.trim() || undefined
            });
            message.success('好友请求已发送');
            setIsAddFriendModalVisible(false);
            setUserCode('');
            setFriendMessage('');
        } catch (error: any) {
            console.error('Add friend error:', error);
            const errorMsg = error?.error || error?.message || '发送好友请求失败';
            message.error(errorMsg);
        } finally {
            setIsAddingFriend(false);
        }
    };

    const handleRespondRequest = async (requestId: string, accept: boolean) => {
        if (!requestId) return;
        if (respondingRequestId) return;
        setRespondingRequestId(requestId);
        try {
            await friendService.respondToRequest({ request_id: requestId, action: accept ? 'accept' : 'reject' });
            message.success(accept ? '已同意好友请求' : '已拒绝好友请求');
            await fetchFriendRequests();
            if (accept) {
                await fetchFriends();
            }
        } catch (error: any) {
            const errorMsg = error?.error || error?.message || '处理好友请求失败';
            message.error(errorMsg);
        } finally {
            setRespondingRequestId(null);
        }
    };

    const openRequestsModal = async () => {
        setIsRequestsModalVisible(true);
        setIsLoadingRequests(true);
        try {
            await fetchFriendRequests();
        } finally {
            setIsLoadingRequests(false);
        }
    };

    const handleCreateGroup = async () => {
        if (!groupName.trim()) {
            message.error('请输入群名称');
            return;
        }
        setIsCreatingGroup(true);
        try {
            const created = await createGroup(groupName.trim(), groupMemberIds);
            message.success('群组创建成功');
            setIsCreateGroupModalVisible(false);
            setGroupName('');
            setGroupMemberIds([]);
            setActiveConversation(created.id);
            setActiveTab('groups');
        } catch (error: any) {
            message.error(error?.error || error?.message || '创建群组失败');
        } finally {
            setIsCreatingGroup(false);
        }
    };

    const handleInviteFriendToGroup = async (friend: SidebarListItem, groupId: string) => {
        if (!friend.friendUserId) {
            message.error('好友信息不完整，无法邀请入群');
            return;
        }
        try {
            const addedCount = await inviteGroupMembers(groupId, [friend.friendUserId]);
            if (addedCount > 0) {
                message.success(`已邀请 ${friend.name} 入群`);
                return;
            }
            message.info('未新增成员，可能已在群内');
        } catch (error: any) {
            message.error(error?.error || error?.message || '邀请入群失败');
        }
    };

    const handleRemoveFriend = (friend: SidebarListItem) => {
        if (typeof friend.friendUserCode !== 'number') {
            message.error('好友缺少用户编号，无法删除');
            return;
        }
        Modal.confirm({
            title: '删除好友',
            content: `确认删除好友「${friend.name}」吗？`,
            okText: '删除',
            okButtonProps: { danger: true },
            cancelText: '取消',
            onOk: async () => {
                try {
                    await friendService.removeFriend({ target_user_code: friend.friendUserCode! });
                    await fetchFriends();
                    message.success('已删除好友');
                } catch (error: any) {
                    message.error(error?.error || error?.message || '删除好友失败');
                }
            }
        });
    };

    const getFriendContextMenu = (item: SidebarListItem): MenuProps => ({
        items: [
            {
                key: 'invite-group',
                label: '邀请入群',
                children: inviteGroupOptions.length > 0
                    ? inviteGroupOptions.map((group) => ({
                        key: `invite:${group.id}`,
                        label: group.name
                    }))
                    : [{ key: 'invite-empty', label: '暂无可邀请群组', disabled: true }]
            },
            { type: 'divider' },
            {
                key: 'remove-friend',
                label: '删除好友',
                danger: true
            }
        ],
        onClick: ({ key }) => {
            const keyText = String(key);
            if (keyText.startsWith('invite:')) {
                const groupId = keyText.replace('invite:', '');
                handleInviteFriendToGroup(item, groupId);
                return;
            }
            if (keyText === 'remove-friend') {
                handleRemoveFriend(item);
            }
        }
    });

    const renderGroupStatus = (status: string) => {
        if (status === 'left') return <Tag color="orange">已退群</Tag>;
        if (status === 'dismissed') return <Tag color="red">已解散</Tag>;
        return <Tag color="green">正常</Tag>;
    };

    const onClickItem = (item: SidebarListItem) => {
        if (item.type === 'group' && item.status !== 'active') {
            message.warning('该群不可聊天，请选择正常状态群组');
            return;
        }
        setActiveConversation(item.id);
    };

    return (
        <Sider width={300} theme="light" style={{ borderRight: '1px solid #f0f0f0', display: 'flex', flexDirection: 'column' }}>
            <div style={{ padding: '16px', display: 'flex', gap: '8px' }}>
                <Search
                    placeholder={activeTab === 'friends' ? '搜索好友' : '搜索群组'}
                    prefix={<SearchOutlined />}
                    style={{ flex: 1 }}
                    value={searchKeyword}
                    onChange={(e) => setSearchKeyword(e.target.value)}
                />
                <Button icon={<UserAddOutlined />} onClick={() => setIsAddFriendModalVisible(true)} />
                <Tooltip title="创建群组">
                    <Button icon={<PlusOutlined />} onClick={() => setIsCreateGroupModalVisible(true)} />
                </Tooltip>
                <Badge count={friendRequests.length} size="small">
                    <Button icon={<UserOutlined />} onClick={openRequestsModal} />
                </Badge>
            </div>
            
            <Tabs 
                defaultActiveKey="friends" 
                centered 
                onChange={(key) => {
                    setActiveTab(key);
                    setSearchKeyword('');
                }}
                items={[
                    { key: 'friends', label: '好友', icon: <UserOutlined /> },
                    { key: 'groups', label: '群组', icon: <TeamOutlined /> },
                ]}
            />

            <div style={{ flex: 1, overflowY: 'auto' }}>
                <List
                    itemLayout="horizontal"
                    dataSource={items}
                    renderItem={(item) => {
                        const listItem = (
                            <List.Item 
                                style={{ 
                                    padding: '12px 16px', 
                                    cursor: 'pointer',
                                    background: activeConversationId === item.id ? '#e6f7ff' : 'transparent'
                                }} 
                                onClick={() => onClickItem(item)}
                            >
                                <List.Item.Meta
                                    avatar={
                                        <Badge count={0} dot> {/* Placeholder for unread */}
                                            <Avatar icon={item.type === 'friend' ? <UserOutlined /> : <TeamOutlined />} src={item.avatar} />
                                        </Badge>
                                    }
                                    title={item.name}
                                    description={
                                        item.type === 'group'
                                            ? (
                                                <span>
                                                    {renderGroupStatus(item.status || 'active')}
                                                    <span style={{ marginLeft: 8 }}>{item.extra}</span>
                                                </span>
                                            )
                                            : (item.status === 'online' ? <span style={{ color: '#52c41a' }}>在线</span> : '离线')
                                    }
                                />
                            </List.Item>
                        );

                        if (item.type !== 'friend') {
                            return listItem;
                        }

                        return (
                            <Dropdown trigger={['contextMenu']} menu={getFriendContextMenu(item)}>
                                <div>{listItem}</div>
                            </Dropdown>
                        );
                    }}
                />
            </div>

            <Modal
                title="添加好友"
                open={isAddFriendModalVisible}
                onOk={handleAddFriend}
                onCancel={() => setIsAddFriendModalVisible(false)}
                confirmLoading={isAddingFriend}
            >
                <Input 
                    placeholder="输入用户编号（user_code）" 
                    value={userCode} 
                    onChange={(e) => setUserCode(e.target.value)} 
                    onPressEnter={handleAddFriend}
                />
                <TextArea
                    style={{ marginTop: 12 }}
                    rows={3}
                    value={friendMessage}
                    onChange={(e) => setFriendMessage(e.target.value)}
                    placeholder="可选：验证消息"
                />
            </Modal>

            <Modal
                title="好友请求"
                open={isRequestsModalVisible}
                footer={null}
                onCancel={() => setIsRequestsModalVisible(false)}
            >
                <List
                    loading={isLoadingRequests}
                    dataSource={friendRequests}
                    renderItem={(item) => (
                        (() => {
                            const requestId = (item as any).id ?? (item as any).request_id ?? '';
                            const fromUser: any = (item as any).from_user;
                            const displayName = fromUser?.nickname || fromUser?.username || item.from_user_id;
                            const codeText = typeof fromUser?.user_code === 'number' ? `${fromUser.user_code}` : '-';
                            const isPending = item.status === 'pending';
                            const isResponding = respondingRequestId === requestId;
                            return (
                        <List.Item
                            actions={[
                                <Button key="accept" type="link" disabled={!isPending || isResponding} loading={isResponding} onClick={() => handleRespondRequest(requestId, true)}>同意</Button>,
                                <Button key="reject" type="link" danger disabled={!isPending || isResponding} loading={isResponding} onClick={() => handleRespondRequest(requestId, false)}>拒绝</Button>
                            ]}
                        >
                            <List.Item.Meta
                                title={`${displayName} (用户编号: ${codeText})`}
                                description={item.message || item.status}
                            />
                        </List.Item>
                            );
                        })()
                    )}
                />
            </Modal>

            <Modal
                title="创建群组"
                open={isCreateGroupModalVisible}
                confirmLoading={isCreatingGroup}
                onOk={handleCreateGroup}
                onCancel={() => setIsCreateGroupModalVisible(false)}
            >
                <Input
                    placeholder="请输入群名称"
                    value={groupName}
                    onChange={(e) => setGroupName(e.target.value)}
                    maxLength={30}
                />
                <div style={{ marginTop: 12 }}>
                    <div style={{ marginBottom: 8, color: '#666' }}>选择初始成员（可选）</div>
                    <Select
                        mode="multiple"
                        style={{ width: '100%' }}
                        placeholder="从好友列表选择成员"
                        options={createGroupOptions}
                        value={groupMemberIds}
                        onChange={setGroupMemberIds}
                        showSearch
                        optionFilterProp="label"
                    />
                </div>
            </Modal>
        </Sider>
    );
};

export default Sidebar;
