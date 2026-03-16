import React from 'react';
import { Layout, Typography, Button, Drawer, Descriptions, Tag, List, Select, Space, Popconfirm, message, Spin, Dropdown, Input, Modal } from 'antd';
import { InfoCircleOutlined, MoreOutlined, CopyOutlined, FileOutlined, FileImageOutlined, DownloadOutlined, AimOutlined, CloseOutlined } from '@ant-design/icons';
import MessageList from './MessageList';
import InputArea from './InputArea';
import { useChatStore } from '../../stores/chatStore';
import { useAuthStore } from '../../stores/authStore';
import { GroupMember } from '../../types/models';
import { messageDao, MessageRow } from '../../api/db_sqlite';
import { fileService } from '../../api/FileService';
import { localFileService } from '../../api/LocalFileService';

const { Header, Content } = Layout;
const { Title, Text } = Typography;

const ChatWindow: React.FC = () => {
    const {
        activeConversationId,
        friends,
        groups,
        messages,
        sendMessage,
        loadMessages,
        fetchGroupDetails,
        fetchGroupMembers,
        activeGroupDetails,
        groupMembersByGroupId,
        inviteGroupMembers,
        removeGroupMember,
        addGroupAdmins,
        dismissGroup,
        leaveGroup,
        isReconnecting,
        unreadCountByConversation,
        earliestUnreadMessageIdByConversation,
        markConversationRead
    } = useChatStore();
    const user = useAuthStore((state) => state.user);
    const [isDrawerOpen, setIsDrawerOpen] = React.useState(false);
    const [isLoadingGroupInfo, setIsLoadingGroupInfo] = React.useState(false);
    const [inviteIds, setInviteIds] = React.useState<string[]>([]);
    const [adminIds, setAdminIds] = React.useState<string[]>([]);
    const [isSubmitting, setIsSubmitting] = React.useState(false);
    const [isHistoryDrawerOpen, setIsHistoryDrawerOpen] = React.useState(false);
    const [historyKeyword, setHistoryKeyword] = React.useState('');
    const [historyRows, setHistoryRows] = React.useState<MessageRow[]>([]);
    const [isHistoryLoading, setIsHistoryLoading] = React.useState(false);
    const [historyOffset, setHistoryOffset] = React.useState(0);
    const [historyHasMore, setHistoryHasMore] = React.useState(true);
    const [selectedHistoryRow, setSelectedHistoryRow] = React.useState<MessageRow | null>(null);
    const [historyPreviewUrl, setHistoryPreviewUrl] = React.useState('');
    const [isHistoryPreviewLoading, setIsHistoryPreviewLoading] = React.useState(false);
    const [historyFocusMessageId, setHistoryFocusMessageId] = React.useState<string | null>(null);
    const [isHistoryJumping, setIsHistoryJumping] = React.useState(false);
    const [mainLoadedOffset, setMainLoadedOffset] = React.useState(0);
    const [mainHasMorePrevious, setMainHasMorePrevious] = React.useState(true);
    const [isMainLoadingPrevious, setIsMainLoadingPrevious] = React.useState(false);
    const [unreadHint, setUnreadHint] = React.useState<{ roomId: string; count: number; earliestMessageId: string | null } | null>(null);

    // Resolve active chat details
    const roomId = activeConversationId || '';
    const activeFriend = friends.find(f => f.room_id === roomId);
    const activeGroup = groups.find(g => g.id === roomId);
    const title = (activeFriend?.nickname || activeFriend?.username) || activeGroup?.name || 'Unknown';
    const currentMessages = messages[roomId] || [];
    const isGroupConversation = !!activeGroup;
    const members = activeGroup ? (groupMembersByGroupId[activeGroup.id] || []) : [];
    const myRole = activeGroupDetails?.my_role || members.find((m) => m.user_id === user?.id)?.role || activeGroup?.my_role;
    const memberIdSet = new Set(members.map((m) => m.user_id));
    const inviteOptions = friends
        .filter((friend) => !memberIdSet.has(friend.id))
        .map((friend) => ({ label: friend.nickname || friend.username, value: friend.id }));

    const HISTORY_PAGE_SIZE = 50;
    const MAIN_PAGE_SIZE = 50;

    React.useEffect(() => {
        setMainLoadedOffset(0);
        setMainHasMorePrevious(true);
        setIsMainLoadingPrevious(false);
    }, [activeConversationId]);

    React.useEffect(() => {
        if (!activeConversationId || mainLoadedOffset !== 0) return;
        if (currentMessages.length === 0) {
            setMainHasMorePrevious(false);
            return;
        }
        setMainLoadedOffset(currentMessages.length);
        setMainHasMorePrevious(currentMessages.length === MAIN_PAGE_SIZE);
    }, [activeConversationId, currentMessages.length, mainLoadedOffset]);

    React.useEffect(() => {
        if (!activeConversationId) {
            setUnreadHint(null);
            return;
        }
        const count = unreadCountByConversation[activeConversationId] || 0;
        if (count <= 0) {
            setUnreadHint(null);
            return;
        }
        const earliestMessageId = earliestUnreadMessageIdByConversation[activeConversationId] || null;
        setUnreadHint({ roomId: activeConversationId, count, earliestMessageId });
        markConversationRead(activeConversationId);
    }, [activeConversationId]);

    const loadPreviousMainMessages = async () => {
        if (!activeConversationId || isMainLoadingPrevious || !mainHasMorePrevious) return;
        setIsMainLoadingPrevious(true);
        try {
            const loaded = await loadMessages(activeConversationId, MAIN_PAGE_SIZE, mainLoadedOffset, 'prepend');
            setMainLoadedOffset((prev) => prev + loaded);
            setMainHasMorePrevious(loaded === MAIN_PAGE_SIZE);
        } finally {
            setIsMainLoadingPrevious(false);
        }
    };

    const jumpToEarliestUnread = () => {
        if (unreadHint?.earliestMessageId) {
            setHistoryFocusMessageId(unreadHint.earliestMessageId);
        }
        setUnreadHint(null);
    };

    const closeUnreadHint = () => {
        setUnreadHint(null);
    };

    const loadHistoryRows = async (keyword: string, append: boolean = false) => {
        if (!activeConversationId) return;
        const nextOffset = append ? historyOffset : 0;
        setIsHistoryLoading(true);
        try {
            const rows = await messageDao.searchTextByRoomId(activeConversationId, keyword, HISTORY_PAGE_SIZE, nextOffset);
            if (append) {
                setHistoryRows((prev) => {
                    const exists = new Set(prev.map((r) => r.id));
                    const appended = rows.filter((r) => !exists.has(r.id));
                    return [...prev, ...appended];
                });
            } else {
                setHistoryRows(rows);
            }
            setHistoryOffset(nextOffset + rows.length);
            setHistoryHasMore(rows.length === HISTORY_PAGE_SIZE);
        } catch (error: any) {
            message.error(error?.message || error?.error || '查询聊天记录失败');
        } finally {
            setIsHistoryLoading(false);
        }
    };

    const openHistoryDrawer = async () => {
        setIsHistoryDrawerOpen(true);
        setHistoryKeyword('');
        setHistoryOffset(0);
        setHistoryHasMore(true);
        await loadHistoryRows('', false);
    };

    const resolveSenderName = (senderId: string) => {
        if (!senderId) return '未知用户';
        if (senderId === user?.id) return '我';
        const friend = friends.find((f) => f.id === senderId);
        if (friend) return friend.nickname || friend.username || senderId;
        const member = members.find((m) => m.user_id === senderId);
        if (member) return member.nickname || member.username || senderId;
        return senderId;
    };

    const formatHistoryTime = (createdAt: number) => {
        const time = new Date(createdAt || Date.now());
        return time.toLocaleString('zh-CN', { hour12: false });
    };

    const historyEmptyText = historyKeyword.trim()
        ? `未找到包含“${historyKeyword.trim()}”的聊天记录`
        : '当前房间暂无聊天记录';

    const parsePayload = (row: MessageRow): Record<string, any> => {
        if (!row.payload_json) return {};
        try {
            return JSON.parse(row.payload_json);
        } catch {
            return {};
        }
    };

    const getAttachmentId = (row: MessageRow): string => {
        const payload = parsePayload(row);
        return row.attachment_id || payload?.attachment_id || payload?.attachment?.attachment_id || '';
    };

    const getHistoryDisplayName = (row: MessageRow): string => {
        if (row.content_type === 'text') return row.content_text || '(空文本)';
        const payload = parsePayload(row);
        const name = payload?.attachment?.file_name || payload?.file_name || row.content_text || getAttachmentId(row);
        return name || (row.content_type === 'image' ? '图片' : '文件');
    };

    const buildHistoryDownloadFileName = (row: MessageRow): string => {
        const payload = parsePayload(row);
        const candidates = [payload?.attachment?.file_name, payload?.file_name, row.content_text, getHistoryDisplayName(row)].filter(Boolean) as string[];

        const toBaseName = (value: string): string => {
            const trimmed = (value || '').trim();
            if (!trimmed) return '';
            let name = trimmed;
            if (/^https?:\/\//i.test(trimmed)) {
                try {
                    const u = new URL(trimmed);
                    name = decodeURIComponent(u.pathname.split('/').pop() || '');
                } catch {
                    name = trimmed;
                }
            } else {
                const noQuery = trimmed.split('?')[0].split('#')[0];
                name = noQuery.split(/[\\/]/).pop() || noQuery;
                try {
                    name = decodeURIComponent(name);
                } catch {}
            }
            return name.replace(/[\\/:*?"<>|]/g, '_').trim();
        };

        let fileName = '';
        for (const value of candidates) {
            const candidateName = toBaseName(value);
            if (candidateName) {
                fileName = candidateName;
                break;
            }
        }

        if (!fileName) {
            fileName = getAttachmentId(row) || 'download';
        }

        const hasExt = /\.[a-zA-Z0-9]{1,8}$/.test(fileName);
        if (!hasExt && row.content_type === 'image') {
            fileName = `${fileName}.png`;
        }
        return fileName;
    };

    const onClickHistorySearch = async (value?: string) => {
        const next = (value ?? historyKeyword).trim();
        setHistoryKeyword(next);
        setHistoryOffset(0);
        setHistoryHasMore(true);
        await loadHistoryRows(next, false);
    };

    const onClickLoadMoreHistory = async () => {
        if (isHistoryLoading || !historyHasMore) return;
        await loadHistoryRows(historyKeyword, true);
    };

    const resolveHistoryPreviewUrl = async (row: MessageRow): Promise<string> => {
        if (row.cache_path) {
            const localUrl = await localFileService.getLocalFileUrl(row.cache_path);
            if (localUrl) return localUrl;
        }
        const attachmentId = getAttachmentId(row);
        if (!attachmentId) return '';
        const result = await fileService.getFileUrl(attachmentId);
        return result.url || '';
    };

    const openHistoryDetail = async (row: MessageRow) => {
        setSelectedHistoryRow(row);
        setHistoryPreviewUrl('');
        if (row.content_type !== 'image' && row.content_type !== 'file') return;
        setIsHistoryPreviewLoading(true);
        try {
            const url = await resolveHistoryPreviewUrl(row);
            setHistoryPreviewUrl(url);
        } catch {
            message.error('加载附件失败');
        } finally {
            setIsHistoryPreviewLoading(false);
        }
    };

    const onDownloadHistoryAttachment = async () => {
        if (!selectedHistoryRow) return;
        const attachmentId = getAttachmentId(selectedHistoryRow);
        if (!attachmentId) {
            message.warning('缺少附件标识，无法下载');
            return;
        }

        const fileName = buildHistoryDownloadFileName(selectedHistoryRow);

        setIsHistoryPreviewLoading(true);
        try {
            const localPath = await localFileService.downloadAndSaveAs(attachmentId, fileName);
            if (!localPath) {
                message.info('已取消下载');
                return;
            }

            setSelectedHistoryRow((prev) => prev ? { ...prev, cache_path: localPath } : prev);
            setHistoryRows((prev) => prev.map((row) => row.id === selectedHistoryRow.id ? { ...row, cache_path: localPath } : row));

            if (selectedHistoryRow.content_type === 'image') {
                const localUrl = await localFileService.getLocalFileUrl(localPath);
                if (localUrl) {
                    setHistoryPreviewUrl(localUrl);
                }
            }

            message.success(`已保存到：${localPath}`);
        } catch {
            message.error('下载失败');
        } finally {
            setIsHistoryPreviewLoading(false);
        }
    };

    const copyHistoryContent = async () => {
        if (!selectedHistoryRow) return;
        const text = selectedHistoryRow.content_type === 'text' ? (selectedHistoryRow.content_text || '') : getHistoryDisplayName(selectedHistoryRow);
        if (!text) return;
        try {
            await navigator.clipboard.writeText(text);
            message.success('已复制到剪贴板');
        } catch {
            message.error('复制失败');
        }
    };

    const jumpToMainMessage = async () => {
        if (!selectedHistoryRow || !activeConversationId) return;
        setIsHistoryJumping(true);
        try {
            const loaded = await loadMessages(activeConversationId, 2000, 0, 'replace');
            setMainLoadedOffset(loaded);
            setMainHasMorePrevious(loaded === 2000);
            setHistoryFocusMessageId(selectedHistoryRow.id);
            setSelectedHistoryRow(null);
            setIsHistoryDrawerOpen(false);
        } catch {
            message.error('定位失败');
        } finally {
            setIsHistoryJumping(false);
        }
    };

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

    if (!activeConversationId) {
        return (
            <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#ccc' }}>
                Select a chat to start messaging
            </div>
        );
    }

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
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    {isReconnecting && <Tag color="orange">重连中...</Tag>}
                    <Button type="text" icon={<InfoCircleOutlined />} onClick={openGroupPanel} disabled={!isGroupConversation} />
                    <Dropdown
                        trigger={['click']}
                        menu={{
                            items: [{ key: 'chat-history', label: '聊天记录' }],
                            onClick: ({ key }) => {
                                if (key === 'chat-history') {
                                    openHistoryDrawer();
                                }
                            }
                        }}
                    >
                        <Button type="text" icon={<MoreOutlined />} />
                    </Dropdown>
                </div>
            </Header>
            <Content style={{ display: 'flex', flexDirection: 'column', background: '#fff' }}>
                {unreadHint && unreadHint.roomId === roomId && (
                    <div style={{ padding: '8px 16px', borderBottom: '1px solid #f0f0f0', background: '#fffbe6', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                        <Space size={8} wrap>
                            <Tag color="red">{unreadHint.count} 条未读</Tag>
                            <Button size="small" type="link" onClick={jumpToEarliestUnread}>跳到最早未读</Button>
                        </Space>
                        <Button type="text" size="small" icon={<CloseOutlined />} onClick={closeUnreadHint} />
                    </div>
                )}
                <MessageList
                    key={roomId}
                    messages={currentMessages}
                    focusMessageId={historyFocusMessageId}
                    onFocusDone={() => setHistoryFocusMessageId(null)}
                    canLoadPrevious={mainHasMorePrevious}
                    isLoadingPrevious={isMainLoadingPrevious}
                    onLoadPrevious={loadPreviousMainMessages}
                />
                <InputArea onSend={sendMessage} roomId={activeConversationId} roomName={title} />
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
                        <Spin spinning={isLoadingGroupInfo}>
                            <Descriptions bordered size="small" column={1}>
                                <Descriptions.Item label="群名称">{activeGroupDetails?.name || activeGroup.name}</Descriptions.Item>
                                <Descriptions.Item label="群ID">{activeGroup.id}</Descriptions.Item>
                                <Descriptions.Item label="成员数">{activeGroupDetails?.member_count ?? activeGroup.member_count}</Descriptions.Item>
                                <Descriptions.Item label="我的角色">
                                    <Tag color={myRole === 'owner' ? 'gold' : myRole === 'admin' ? 'blue' : 'default'}>
                                        {myRole || 'member'}
                                    </Tag>
                                </Descriptions.Item>
                            </Descriptions>
                        </Spin>

                        {(myRole === 'owner' || myRole === 'admin') && (
                            <div>
                                <Text strong>邀请成员入群</Text>
                                <Space direction="vertical" style={{ width: '100%', marginTop: 8 }}>
                                    <Select mode="multiple" style={{ width: '100%' }} placeholder="从好友中选择要邀请的成员" options={inviteOptions} value={inviteIds} onChange={setInviteIds} optionFilterProp="label" />
                                    <Button type="primary" onClick={handleInviteMembers} disabled={inviteIds.length === 0} loading={isSubmitting}>邀请入群</Button>
                                </Space>
                            </div>
                        )}

                        {myRole === 'owner' && (
                            <div>
                                <Text strong>批量设置管理员</Text>
                                <Space direction="vertical" style={{ width: '100%', marginTop: 8 }}>
                                    <Select mode="multiple" style={{ width: '100%' }} placeholder="选择成员设为管理员" options={members.filter((m) => m.role === 'member').map((m) => ({ label: m.nickname || m.username || m.user_id, value: m.user_id }))} value={adminIds} onChange={setAdminIds} optionFilterProp="label" />
                                    <Button onClick={handleAddAdmins} disabled={adminIds.length === 0} loading={isSubmitting}>设置管理员</Button>
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
                                                <Popconfirm key="remove" title="确认移除该成员？" onConfirm={() => handleRemoveMember(member.user_id)} okText="确认" cancelText="取消">
                                                    <Button type="link" danger loading={isSubmitting}>移除</Button>
                                                </Popconfirm>
                                            ) : null
                                        ].filter(Boolean)}
                                    >
                                        <List.Item.Meta
                                            title={member.nickname || member.username || member.user_id}
                                            description={<Space><span>{member.user_id}</span><Tag color={member.role === 'owner' ? 'gold' : member.role === 'admin' ? 'blue' : 'default'}>{member.role}</Tag></Space>}
                                        />
                                    </List.Item>
                                )}
                            />
                        </div>

                        <Space>
                            {myRole === 'owner' && (
                                <Popconfirm title="确认解散该群？该操作不可恢复" onConfirm={handleDismissGroup} okText="确认" cancelText="取消">
                                    <Button danger loading={isSubmitting}>解散群组</Button>
                                </Popconfirm>
                            )}
                            {myRole !== 'owner' && (
                                <Popconfirm title="确认退出该群？" onConfirm={handleLeaveGroup} okText="确认" cancelText="取消">
                                    <Button danger loading={isSubmitting}>退出群组</Button>
                                </Popconfirm>
                            )}
                        </Space>
                    </Space>
                ) : null}
            </Drawer>

            <Drawer
                title="聊天记录"
                open={isHistoryDrawerOpen}
                width={460}
                onClose={() => setIsHistoryDrawerOpen(false)}
                destroyOnClose
            >
                <Space direction="vertical" size={12} style={{ width: '100%' }}>
                    <Input.Search
                        allowClear
                        enterButton="搜索"
                        placeholder="搜索当前房间聊天消息"
                        value={historyKeyword}
                        onChange={(e) => setHistoryKeyword(e.target.value)}
                        onSearch={onClickHistorySearch}
                    />
                    <List
                        loading={isHistoryLoading && historyRows.length === 0}
                        dataSource={historyRows}
                        locale={{ emptyText: historyEmptyText }}
                        rowKey={(row) => row.id}
                        renderItem={(row) => {
                            const isImage = row.content_type === 'image';
                            const isFile = row.content_type === 'file';
                            const icon = isImage ? <FileImageOutlined style={{ color: '#1677ff' }} /> : isFile ? <FileOutlined style={{ color: '#722ed1' }} /> : null;
                            const typeText = isImage ? '图片' : isFile ? '文件' : '文本';
                            return (
                                <List.Item style={{ padding: 0, border: 'none', marginBottom: 8 }} onClick={() => openHistoryDetail(row)}>
                                    <div style={{ width: '100%', cursor: 'pointer', border: '1px solid #f0f0f0', borderRadius: 10, padding: '10px 12px', background: '#fafafa' }}>
                                        <Space size={8} style={{ marginBottom: 6 }}>
                                            {icon}
                                            <Text strong>{resolveSenderName(row.sender_id)}</Text>
                                            <Text type="secondary">{typeText} · {formatHistoryTime(row.created_at)}</Text>
                                        </Space>
                                        <div style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{getHistoryDisplayName(row)}</div>
                                    </div>
                                </List.Item>
                            );
                        }}
                    />
                    {historyRows.length > 0 && (
                        <Button block onClick={onClickLoadMoreHistory} loading={isHistoryLoading && historyRows.length > 0} disabled={!historyHasMore}>
                            {historyHasMore ? '加载下一页' : '没有更多了'}
                        </Button>
                    )}
                </Space>
            </Drawer>

            <Modal
                open={!!selectedHistoryRow}
                title="聊天记录详情"
                zIndex={2200}
                getContainer={document.body}
                onCancel={() => setSelectedHistoryRow(null)}
                footer={[
                    <Button key="copy" icon={<CopyOutlined />} onClick={copyHistoryContent}>复制</Button>,
                    <Button key="jump" icon={<AimOutlined />} loading={isHistoryJumping} onClick={jumpToMainMessage}>定位到主界面</Button>,
                    (selectedHistoryRow?.content_type === 'image' || selectedHistoryRow?.content_type === 'file') ? (
                        <Button key="download" icon={<DownloadOutlined />} loading={isHistoryPreviewLoading} onClick={onDownloadHistoryAttachment}>下载</Button>
                    ) : null,
                    <Button key="close" type="primary" onClick={() => setSelectedHistoryRow(null)}>关闭</Button>
                ].filter(Boolean)}
            >
                <Space direction="vertical" size={8} style={{ width: '100%' }}>
                    <Text type="secondary">{selectedHistoryRow ? `${resolveSenderName(selectedHistoryRow.sender_id)} · ${formatHistoryTime(selectedHistoryRow.created_at)}` : ''}</Text>
                    {selectedHistoryRow?.content_type === 'image' ? (
                        <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: 180, background: '#fafafa', borderRadius: 8, padding: 12 }}>
                            {isHistoryPreviewLoading ? <Spin /> : historyPreviewUrl ? <img src={historyPreviewUrl} alt={getHistoryDisplayName(selectedHistoryRow)} style={{ maxWidth: '100%', maxHeight: '60vh', borderRadius: 6 }} /> : <Text type="secondary">图片加载失败</Text>}
                        </div>
                    ) : selectedHistoryRow?.content_type === 'file' ? (
                        <Space>
                            <FileOutlined />
                            <Text>{selectedHistoryRow ? getHistoryDisplayName(selectedHistoryRow) : ''}</Text>
                        </Space>
                    ) : (
                        <div style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{selectedHistoryRow?.content_text || ''}</div>
                    )}
                </Space>
            </Modal>
        </Layout>
    );
};

export default ChatWindow;
