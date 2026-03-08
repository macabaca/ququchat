import React from 'react';
import { Layout, Typography, Button, Drawer, Descriptions, Tag, List, Select, Space, Popconfirm, message } from 'antd';
import { InfoCircleOutlined, MoreOutlined } from '@ant-design/icons';
import MessageList from './MessageList';
import InputArea from './InputArea';
import { useChatStore } from '../../stores/chatStore';
import { useAuthStore } from '../../stores/authStore';
import { GroupMember } from '../../types/models';

const { Header, Content } = Layout;
const { Title, Text } = Typography;

const ChatWindow: React.FC = () => {
    const {
        activeConversationId,
        friends,
        groups,
        messages,
        sendMessage,
        fetchGroupDetails,
        fetchGroupMembers,
        activeGroupDetails,
        groupMembersByGroupId,
        inviteGroupMembers,
        removeGroupMember,
        addGroupAdmins,
        dismissGroup,
        leaveGroup
    } = useChatStore();
    const user = useAuthStore((state) => state.user);
    const [isDrawerOpen, setIsDrawerOpen] = React.useState(false);
    const [isLoadingGroupInfo, setIsLoadingGroupInfo] = React.useState(false);
    const [inviteIds, setInviteIds] = React.useState<string[]>([]);
    const [adminIds, setAdminIds] = React.useState<string[]>([]);
    const [isSubmitting, setIsSubmitting] = React.useState(false);

    if (!activeConversationId) {
        return (
            <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#ccc' }}>
                Select a chat to start messaging
            </div>
        );
    }

    // Resolve active chat details
    const activeFriend = friends.find(f => f.room_id === activeConversationId);
    const activeGroup = groups.find(g => g.id === activeConversationId);
    const title = (activeFriend?.nickname || activeFriend?.username) || activeGroup?.name || 'Unknown';
    const currentMessages = messages[activeConversationId] || [];
    const isGroupConversation = !!activeGroup;
    const members = activeGroup ? (groupMembersByGroupId[activeGroup.id] || []) : [];
    const myRole = activeGroupDetails?.my_role || members.find((m) => m.user_id === user?.id)?.role || activeGroup?.my_role;
    const memberIdSet = new Set(members.map((m) => m.user_id));
    const inviteOptions = friends
        .filter((friend) => !memberIdSet.has(friend.id))
        .map((friend) => ({ label: friend.nickname || friend.username, value: friend.id }));

    const openGroupPanel = async () => {
        if (!activeGroup) return;
        setIsDrawerOpen(true);
        setIsLoadingGroupInfo(true);
        try {
            await Promise.all([fetchGroupDetails(activeGroup.id), fetchGroupMembers(activeGroup.id)]);
        } catch (error: any) {
            message.error(error?.error || error?.message || '获取群信息失败');
        } finally {
            setIsLoadingGroupInfo(false);
        }
    };

    const canRemoveMember = (target: GroupMember) => {
        if (!user?.id) return false;
        if (target.user_id === user.id) return false;
        if (myRole === 'owner') return true;
        if (myRole === 'admin') return target.role === 'member';
        return false;
    };

    const handleInviteMembers = async () => {
        if (!activeGroup || inviteIds.length === 0) return;
        setIsSubmitting(true);
        try {
            const count = await inviteGroupMembers(activeGroup.id, inviteIds);
            message.success(`邀请完成，新增 ${count} 人`);
            setInviteIds([]);
        } catch (error: any) {
            message.error(error?.error || error?.message || '邀请成员失败');
        } finally {
            setIsSubmitting(false);
        }
    };

    const handleAddAdmins = async () => {
        if (!activeGroup || adminIds.length === 0) return;
        setIsSubmitting(true);
        try {
            const count = await addGroupAdmins(activeGroup.id, adminIds);
            message.success(`管理员设置完成，更新 ${count} 人`);
            setAdminIds([]);
        } catch (error: any) {
            message.error(error?.error || error?.message || '设置管理员失败');
        } finally {
            setIsSubmitting(false);
        }
    };

    const handleRemoveMember = async (targetUserId: string) => {
        if (!activeGroup) return;
        setIsSubmitting(true);
        try {
            await removeGroupMember(activeGroup.id, targetUserId);
            message.success('成员已移除');
        } catch (error: any) {
            message.error(error?.error || error?.message || '移除成员失败');
        } finally {
            setIsSubmitting(false);
        }
    };

    const handleDismissGroup = async () => {
        if (!activeGroup) return;
        setIsSubmitting(true);
        try {
            await dismissGroup(activeGroup.id);
            message.success('群已解散');
            setIsDrawerOpen(false);
        } catch (error: any) {
            message.error(error?.error || error?.message || '解散群失败');
        } finally {
            setIsSubmitting(false);
        }
    };

    const handleLeaveGroup = async () => {
        if (!activeGroup) return;
        setIsSubmitting(true);
        try {
            await leaveGroup(activeGroup.id);
            message.success('已退出群组');
            setIsDrawerOpen(false);
        } catch (error: any) {
            message.error(error?.error || error?.message || '退出群组失败');
        } finally {
            setIsSubmitting(false);
        }
    };

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
                    <Button type="text" icon={<InfoCircleOutlined />} onClick={openGroupPanel} disabled={!isGroupConversation} />
                    <Button type="text" icon={<MoreOutlined />} />
                </div>
            </Header>
            <Content style={{ display: 'flex', flexDirection: 'column', background: '#fff' }}>
                <MessageList messages={currentMessages} />
                <InputArea onSend={sendMessage} />
            </Content>
            <Drawer
                title="群管理"
                open={isDrawerOpen}
                width={460}
                onClose={() => setIsDrawerOpen(false)}
                destroyOnClose
            >
                {activeGroup ? (
                    <Space direction="vertical" size={16} style={{ width: '100%' }}>
                        <Descriptions bordered size="small" column={1} loading={isLoadingGroupInfo}>
                            <Descriptions.Item label="群名称">{activeGroupDetails?.name || activeGroup.name}</Descriptions.Item>
                            <Descriptions.Item label="群ID">{activeGroup.id}</Descriptions.Item>
                            <Descriptions.Item label="成员数">{activeGroupDetails?.member_count ?? activeGroup.member_count}</Descriptions.Item>
                            <Descriptions.Item label="我的角色">
                                <Tag color={myRole === 'owner' ? 'gold' : myRole === 'admin' ? 'blue' : 'default'}>
                                    {myRole || 'member'}
                                </Tag>
                            </Descriptions.Item>
                        </Descriptions>

                        {(myRole === 'owner' || myRole === 'admin') && (
                            <div>
                                <Text strong>邀请成员入群</Text>
                                <Space direction="vertical" style={{ width: '100%', marginTop: 8 }}>
                                    <Select
                                        mode="multiple"
                                        style={{ width: '100%' }}
                                        placeholder="从好友中选择要邀请的成员"
                                        options={inviteOptions}
                                        value={inviteIds}
                                        onChange={setInviteIds}
                                        optionFilterProp="label"
                                    />
                                    <Button type="primary" onClick={handleInviteMembers} disabled={inviteIds.length === 0} loading={isSubmitting}>
                                        邀请入群
                                    </Button>
                                </Space>
                            </div>
                        )}

                        {myRole === 'owner' && (
                            <div>
                                <Text strong>批量设置管理员</Text>
                                <Space direction="vertical" style={{ width: '100%', marginTop: 8 }}>
                                    <Select
                                        mode="multiple"
                                        style={{ width: '100%' }}
                                        placeholder="选择成员设为管理员"
                                        options={members.filter((m) => m.role === 'member').map((m) => ({
                                            label: m.nickname || m.username || m.user_id,
                                            value: m.user_id
                                        }))}
                                        value={adminIds}
                                        onChange={setAdminIds}
                                        optionFilterProp="label"
                                    />
                                    <Button onClick={handleAddAdmins} disabled={adminIds.length === 0} loading={isSubmitting}>
                                        设置管理员
                                    </Button>
                                </Space>
                            </div>
                        )}

                        <div>
                            <Text strong>群成员</Text>
                            <List
                                loading={isLoadingGroupInfo}
                                dataSource={members}
                                renderItem={(member) => (
                                    <List.Item
                                        actions={[
                                            canRemoveMember(member) ? (
                                                <Popconfirm
                                                    key="remove"
                                                    title="确认移除该成员？"
                                                    onConfirm={() => handleRemoveMember(member.user_id)}
                                                    okText="确认"
                                                    cancelText="取消"
                                                >
                                                    <Button type="link" danger loading={isSubmitting}>
                                                        移除
                                                    </Button>
                                                </Popconfirm>
                                            ) : null
                                        ].filter(Boolean)}
                                    >
                                        <List.Item.Meta
                                            title={member.nickname || member.username || member.user_id}
                                            description={
                                                <Space>
                                                    <span>{member.user_id}</span>
                                                    <Tag color={member.role === 'owner' ? 'gold' : member.role === 'admin' ? 'blue' : 'default'}>
                                                        {member.role}
                                                    </Tag>
                                                </Space>
                                            }
                                        />
                                    </List.Item>
                                )}
                            />
                        </div>

                        <Space>
                            {myRole === 'owner' && (
                                <Popconfirm
                                    title="确认解散该群？该操作不可恢复"
                                    onConfirm={handleDismissGroup}
                                    okText="确认"
                                    cancelText="取消"
                                >
                                    <Button danger loading={isSubmitting}>解散群组</Button>
                                </Popconfirm>
                            )}
                            {myRole !== 'owner' && (
                                <Popconfirm
                                    title="确认退出该群？"
                                    onConfirm={handleLeaveGroup}
                                    okText="确认"
                                    cancelText="取消"
                                >
                                    <Button danger loading={isSubmitting}>退出群组</Button>
                                </Popconfirm>
                            )}
                        </Space>
                    </Space>
                ) : null}
            </Drawer>
        </Layout>
    );
};

export default ChatWindow;
