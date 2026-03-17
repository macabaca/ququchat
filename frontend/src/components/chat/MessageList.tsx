import React, { useEffect, useMemo, useRef, useState } from 'react';
import { List, Avatar, Button, Modal, message } from 'antd';
import { UserOutlined, FileOutlined, DownloadOutlined, EyeOutlined, FileImageOutlined, DownOutlined } from '@ant-design/icons';
import { Message, ROBOT_DISPLAY_NAME, ROBOT_USER_ID } from '../../types/models';
import { useAuthStore } from '../../stores/authStore';
import { fileService } from '../../api/FileService';
import { useChatStore } from '../../stores/chatStore';
import { localFileService } from '../../api/LocalFileService';

interface MessageListProps {
    messages: Message[];
    focusMessageId?: string | null;
    onFocusDone?: () => void;
    canLoadPrevious?: boolean;
    isLoadingPrevious?: boolean;
    onLoadPrevious?: () => Promise<void> | void;
}

const MessageItem: React.FC<{ msg: Message; isMe: boolean; avatarUrl?: string; senderName: string; isHighlighted?: boolean }> = ({ msg, isMe, avatarUrl, senderName, isHighlighted }) => {
    const [thumbUrl, setThumbUrl] = useState<string>('');
    const [isModalVisible, setIsModalVisible] = useState(false);
    const [originalUrl, setOriginalUrl] = useState<string>('');
    const [loadingOriginal, setLoadingOriginal] = useState(false);
    const [isDownloading, setIsDownloading] = useState(false);

    const isImage = msg.is_image || (typeof msg.content === 'string' && (msg.content.startsWith('http') || msg.content.startsWith('blob:')) && (msg.content.match(/\.(jpeg|jpg|gif|png|webp)(\?.*)?$/i) != null));
    // For temp messages, we might just have the content as the URL (blob or uploaded URL) even if is_image is not set correctly yet?
    // But usually InputArea sets type='image' which sets is_image=true in sendMessage.
    // However, let's trust isImage derived above.
    
    // Check if we already have a valid URL in content (e.g. from temp message or direct URL message)
    const contentIsUrl = typeof msg.content === 'string' && (msg.content.startsWith('http') || msg.content.startsWith('blob:'));
    const hasLocalImageCache = !!msg.cache_path;

    const isFile = !isImage && (!!msg.attachment_id || contentIsUrl);
    const filePlaceholder = !isFile
        ? ''
        : (typeof msg.content === 'string' && msg.content.trim() && !contentIsUrl ? msg.content : `File (${msg.attachment_id || 'attachment'})`);
    const isRobotMessage = msg.from_user_id === ROBOT_USER_ID;
    const payloadText = useMemo(() => {
        const rawPayload = msg.payload_json;
        if (!rawPayload) {
            return '';
        }
        if (typeof rawPayload === 'string') {
            const trimmed = rawPayload.trim();
            if (!trimmed) {
                return '';
            }
            try {
                return JSON.stringify(JSON.parse(trimmed), null, 2);
            } catch {
                return trimmed;
            }
        }
        try {
            return JSON.stringify(rawPayload, null, 2);
        } catch {
            return '';
        }
    }, [msg]);

    useEffect(() => {
        const isBlob = typeof msg.content === 'string' && msg.content.startsWith('blob:');
        setThumbUrl('');

        if (isImage) {
            if (isBlob) {
                // 1. 本地 Blob 预览，最高优先级，直接使用
                setThumbUrl(msg.content);
            } else if (msg.cache_path && window.electronAPI) {
                // 1.5 Local Cache
                window.electronAPI.fs.readFile(msg.cache_path)
                    .then((buffer) => {
                        const arrayBuffer = buffer.buffer.slice(buffer.byteOffset, buffer.byteOffset + buffer.byteLength) as ArrayBuffer;
                        const blob = new Blob([arrayBuffer]);
                        const url = URL.createObjectURL(blob);
                        setThumbUrl(url);
                    })
                    .catch((err) => {
                        console.error("Failed to load local image", err);
                        // Local cache missing or failed -> keep placeholder, no auto remote fetch
                    });
            } else if (contentIsUrl) {
                setThumbUrl(msg.content);
            } 
            // Else: No local cache, no temp URL -> Show placeholder
        }

    }, [msg.id, msg.thumb_attachment_id, isImage, isFile, msg.content, contentIsUrl, msg.cache_path]);

    const handleImageClick = () => {
        setIsModalVisible(true);
        if (isImage && !hasLocalImageCache) {
            if (!originalUrl && msg.content && (msg.content.startsWith('http') || msg.content.startsWith('blob:'))) {
                setOriginalUrl(msg.content);
            } else {
                console.info('[MessageList] skip original image url request: cache_path missing', {
                    id: msg.id,
                    attachment_id: msg.attachment_id
                });
            }
            return;
        }
        if (!originalUrl && msg.attachment_id) {
            // Only fetch original on user explicit action
            setLoadingOriginal(true);
            fileService.getFileUrl(msg.attachment_id)
                .then(res => {
                    setOriginalUrl(res.url);
                })
                .catch(console.error)
                .finally(() => setLoadingOriginal(false));
        } else if (!originalUrl && msg.content && (msg.content.startsWith('http') || msg.content.startsWith('blob:'))) {
            setOriginalUrl(msg.content);
        }
    };

    const inferDownloadFileName = () => {
        const source = typeof msg.content === 'string' ? msg.content.trim() : '';
        const extractName = (value: string) => {
            if (!value) return '';
            if (/^https?:\/\//i.test(value)) {
                try {
                    const u = new URL(value);
                    return decodeURIComponent(u.pathname.split('/').pop() || '');
                } catch {
                    return '';
                }
            }
            const base = value.split('?')[0].split('#')[0].split(/[\\/]/).pop() || '';
            try {
                return decodeURIComponent(base);
            } catch {
                return base;
            }
        };

        let name = extractName(source);
        if (!name || /^blob:/i.test(name)) {
            name = msg.attachment_id || 'download';
        }
        name = name.replace(/[\\/:*?"<>|]/g, '_').trim() || 'download';
        const hasExt = /\.[a-zA-Z0-9]{1,8}$/.test(name);
        if (!hasExt && isImage) {
            name = `${name}.png`;
        }
        return name;
    };

    const handleDownload = async () => {
        if (!msg.attachment_id) {
            message.warning('缺少附件ID，无法下载');
            return;
        }
        setIsDownloading(true);
        try {
            const savedPath = await localFileService.downloadAndSaveAs(msg.attachment_id, inferDownloadFileName());
            if (!savedPath) {
                message.info('已取消下载');
                return;
            }
            message.success(`已保存到：${savedPath}`);
        } catch {
            message.error('下载失败');
        } finally {
            setIsDownloading(false);
        }
    };

    const renderContent = () => {
        if (isImage) {
            return (
                <>
                    {thumbUrl ? (
                        <div onClick={handleImageClick} style={{ cursor: 'pointer', position: 'relative', display: 'inline-block' }}>
                            <img
                                src={thumbUrl}
                                alt="image"
                                style={{ maxWidth: '200px', borderRadius: '8px', display: 'block' }}
                            />
                            <div style={{
                                position: 'absolute',
                                top: 0, left: 0, right: 0, bottom: 0,
                                background: 'rgba(0,0,0,0.1)',
                                borderRadius: '8px',
                                opacity: 0,
                                transition: 'opacity 0.2s',
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center'
                            }}
                            className="image-overlay"
                            onMouseEnter={(e) => e.currentTarget.style.opacity = '1'}
                            onMouseLeave={(e) => e.currentTarget.style.opacity = '0'}
                            >
                                <EyeOutlined style={{ color: '#fff', fontSize: '24px' }} />
                            </div>
                        </div>
                    ) : (
                        <div 
                            onClick={handleImageClick}
                            style={{ 
                                width: '200px', 
                                height: '150px', 
                                background: '#eee', 
                                display: 'flex', 
                                flexDirection: 'column',
                                alignItems: 'center', 
                                justifyContent: 'center',
                                borderRadius: '8px',
                                color: '#999',
                                padding: '8px',
                                textAlign: 'center',
                                cursor: 'pointer'
                            }}
                            title="Click to view original"
                        >
                            <FileImageOutlined style={{ fontSize: '24px', marginBottom: '8px' }} />
                            <span style={{ fontSize: '12px' }}>Image not available locally</span>
                            <span style={{ fontSize: '10px' }}>(Click to view)</span>
                        </div>
                    )}

                    <Modal
                        open={isModalVisible}
                        onCancel={() => setIsModalVisible(false)}
                        footer={[
                            <Button 
                                key="download" 
                                type="primary" 
                                icon={<DownloadOutlined />} 
                                onClick={handleDownload}
                                disabled={!msg.attachment_id}
                                loading={isDownloading || loadingOriginal}
                            >
                                下载
                            </Button>
                        ]}
                        width={800}
                        centered
                    >
                        <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', minHeight: '200px' }}>
                            {originalUrl ? (
                                <img src={originalUrl} alt="Original" style={{ maxWidth: '100%', maxHeight: '70vh' }} />
                            ) : (
                                <span>Loading original image...</span>
                            )}
                        </div>
                    </Modal>
                </>
            );
        }

        if (isFile) {
            return (
                <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <FileOutlined style={{ fontSize: '24px' }} />
                    <div style={{ display: 'flex', flexDirection: 'column' }}>
                        <span>{filePlaceholder}</span>
                        <Button
                            type="link"
                            icon={<DownloadOutlined />}
                            onClick={handleDownload}
                            loading={isDownloading}
                            disabled={!msg.attachment_id}
                            style={{ color: isMe ? '#fff' : '#1890ff', padding: 0, height: 'auto' }}
                        >
                            下载
                        </Button>
                    </div>
                </div>
            );
        }

        return msg.content;
    };

    return (
        <List.Item id={`chat-msg-${msg.id}`} data-msg-id={msg.id} style={{ 
            display: 'flex', 
            justifyContent: isMe ? 'flex-end' : 'flex-start',
            width: '100%',
            border: 'none',
            padding: '4px 0',
            background: isHighlighted ? '#fffbe6' : 'transparent',
            transition: 'background 0.3s ease'
        }}>
            <div style={{ 
                display: 'flex', 
                flexDirection: isMe ? 'row-reverse' : 'row',
                maxWidth: '70%',
                alignItems: 'flex-start'
            }}>
                <div style={{ margin: isMe ? '0 0 0 8px' : '0 8px 0 0', flexShrink: 0, display: 'flex', flexDirection: 'column', alignItems: 'center', maxWidth: 72 }}>
                    <Avatar src={avatarUrl} icon={<UserOutlined />} />
                    <span style={{ marginTop: 4, fontSize: 12, color: '#8c8c8c', lineHeight: '16px', textAlign: 'center', width: '100%', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {senderName}
                    </span>
                </div>
                <div style={{
                    background: isMe ? '#1890ff' : '#fff',
                    color: isMe ? '#fff' : '#000',
                    padding: '8px 12px',
                    borderRadius: '8px',
                    boxShadow: '0 1px 2px rgba(0,0,0,0.1)',
                    wordBreak: 'break-word'
                }}>
                    {renderContent()}
                    {isRobotMessage && payloadText && (
                        <details style={{ marginTop: 8, border: '1px solid #d9d9d9', borderRadius: 6, background: '#fafafa' }}>
                            <summary style={{ cursor: 'pointer', padding: '6px 8px', color: '#595959', fontSize: 12 }}>payload / memory</summary>
                            <pre style={{ margin: 0, padding: '8px', fontSize: 12, lineHeight: 1.5, whiteSpace: 'pre-wrap', wordBreak: 'break-word', color: '#262626' }}>
                                {payloadText}
                            </pre>
                        </details>
                    )}
                </div>
            </div>
        </List.Item>
    );
};

const MessageList: React.FC<MessageListProps> = ({ messages, focusMessageId, onFocusDone, canLoadPrevious = false, isLoadingPrevious = false, onLoadPrevious }) => {
    const user = useAuthStore((state) => state.user);
    const friends = useChatStore((state) => state.friends);
    const activeConversationId = useChatStore((state) => state.activeConversationId);
    const groupMembersByGroupId = useChatStore((state) => state.groupMembersByGroupId);
    const bottomRef = useRef<HTMLDivElement>(null);
    const listRef = useRef<HTMLDivElement>(null);
    const prependAnchorRef = useRef<{ height: number; top: number } | null>(null);
    const [avatarUrlByUserId, setAvatarUrlByUserId] = useState<Record<string, string>>({});
    const [highlightedMessageId, setHighlightedMessageId] = useState<string | null>(null);
    const [shouldAutoScroll, setShouldAutoScroll] = useState(true);
    const [isNearTop, setIsNearTop] = useState(false);

    useEffect(() => {
        if (focusMessageId || !shouldAutoScroll) return;
        const timer = window.setTimeout(() => {
            bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
        }, 300);
        return () => window.clearTimeout(timer);
    }, [messages, focusMessageId, shouldAutoScroll]);

    useEffect(() => {
        if (!focusMessageId) return;
        const timer = window.setTimeout(() => {
            const target = document.getElementById(`chat-msg-${focusMessageId}`);
            if (target) {
                target.scrollIntoView({ behavior: 'smooth', block: 'center' });
                setShouldAutoScroll(false);
                setHighlightedMessageId(focusMessageId);
                window.setTimeout(() => {
                    setHighlightedMessageId((prev) => prev === focusMessageId ? null : prev);
                }, 1800);
            }
            onFocusDone?.();
        }, 80);
        return () => window.clearTimeout(timer);
    }, [focusMessageId, messages, onFocusDone]);

    useEffect(() => {
        let active = true;
        const loadAvatarUrls = async () => {
            const entries: Array<[string, string]> = [];
            if (user?.id) {
                if (user.avatarThumbLocalPath) {
                    const localUrl = await localFileService.getLocalFileUrl(user.avatarThumbLocalPath);
                    if (localUrl) {
                        entries.push([user.id, localUrl]);
                    }
                } else if (user.avatarURL) {
                    entries.push([user.id, user.avatarURL]);
                }
            }
            for (const friend of friends) {
                if (!friend.id) continue;
                if (friend.avatarThumbLocalPath) {
                    const localUrl = await localFileService.getLocalFileUrl(friend.avatarThumbLocalPath);
                    if (localUrl) {
                        entries.push([friend.id, localUrl]);
                        continue;
                    }
                }
                if (friend.avatarURL) {
                    entries.push([friend.id, friend.avatarURL]);
                }
            }
            if (!active) return;
            setAvatarUrlByUserId((prev) => {
                const next = { ...prev };
                for (const [id, url] of entries) {
                    next[id] = url;
                }
                return next;
            });
        };
        loadAvatarUrls();
        return () => {
            active = false;
        };
    }, [user?.id, user?.avatarThumbLocalPath, user?.avatarURL, friends]);

    useEffect(() => {
        if (isLoadingPrevious) return;
        const el = listRef.current;
        const anchor = prependAnchorRef.current;
        if (!el || !anchor) return;
        const delta = el.scrollHeight - anchor.height;
        el.scrollTop = anchor.top + Math.max(delta, 0);
        prependAnchorRef.current = null;
    }, [messages, isLoadingPrevious]);

    const onScrollMessageList = () => {
        const el = listRef.current;
        if (!el) return;
        const distanceToBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
        setShouldAutoScroll(distanceToBottom <= 80);
        setIsNearTop(el.scrollTop <= 24);
    };

    const handleLoadPrevious = async () => {
        if (!onLoadPrevious || isLoadingPrevious) return;
        const el = listRef.current;
        if (el) {
            prependAnchorRef.current = { height: el.scrollHeight, top: el.scrollTop };
        }
        await onLoadPrevious();
    };

    const handleJumpToBottom = () => {
        bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
        setShouldAutoScroll(true);
    };

    const senderNameByUserId = useMemo(() => {
        const map: Record<string, string> = {};
        map[ROBOT_USER_ID] = ROBOT_DISPLAY_NAME;
        if (user?.id) {
            map[user.id] = user.nickname || user.displayName || user.username || '我';
        }
        for (const friend of friends) {
            if (!friend.id) continue;
            map[friend.id] = friend.nickname || friend.displayName || friend.username || friend.id;
        }
        const groupMembers = activeConversationId ? (groupMembersByGroupId[activeConversationId] || []) : [];
        for (const member of groupMembers) {
            if (!member.user_id) continue;
            map[member.user_id] = member.nickname || member.username || member.user_id;
        }
        return map;
    }, [user, friends, activeConversationId, groupMembersByGroupId]);

    return (
        <div ref={listRef} onScroll={onScrollMessageList} style={{ flex: 1, overflowY: 'auto', padding: '20px', background: '#f5f5f5', position: 'relative' }}>
            {isNearTop && canLoadPrevious && (
                <div style={{ display: 'flex', justifyContent: 'center', marginBottom: 12 }}>
                    <Button onClick={handleLoadPrevious} loading={isLoadingPrevious}>加载上一页</Button>
                </div>
            )}
            <List
                dataSource={messages}
                split={false}
                rowKey={(msg) => msg.id || `${msg.room_id || ''}-${msg.timestamp || ''}-${msg.content}`}
                renderItem={(msg) => (
                    <MessageItem
                        msg={msg}
                        isMe={msg.from_user_id === user?.id}
                        avatarUrl={msg.from_user_id ? avatarUrlByUserId[msg.from_user_id] : undefined}
                        senderName={msg.from_user_id ? (senderNameByUserId[msg.from_user_id] || (msg.from_user_id === ROBOT_USER_ID ? ROBOT_DISPLAY_NAME : `${msg.from_user_id.slice(0, 6)}`)) : '未知用户'}
                        isHighlighted={highlightedMessageId === msg.id}
                    />
                )}
            />
            <Button
                type="primary"
                shape="circle"
                icon={<DownOutlined />}
                onClick={handleJumpToBottom}
                disabled={messages.length === 0}
                style={{
                    position: 'fixed',
                    right: 28,
                    bottom: 96,
                    zIndex: 1200,
                    opacity: 0.58,
                    boxShadow: '0 4px 12px rgba(0,0,0,0.2)'
                }}
            />
            <div ref={bottomRef} />
        </div>
    );
};

export default MessageList;
