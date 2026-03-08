import React, { useEffect, useRef, useState } from 'react';
import { List, Avatar, Typography, Image, Button, Modal } from 'antd';
import { UserOutlined, FileOutlined, DownloadOutlined, EyeOutlined, FileImageOutlined } from '@ant-design/icons';
import { Message } from '../../types/models';
import { useAuthStore } from '../../stores/authStore';
import { fileService } from '../../api/FileService';

interface MessageListProps {
    messages: Message[];
}

const MessageItem: React.FC<{ msg: Message; isMe: boolean }> = ({ msg, isMe }) => {
    const [thumbUrl, setThumbUrl] = useState<string>('');
    const [downloadUrl, setDownloadUrl] = useState<string>('');
    const [isModalVisible, setIsModalVisible] = useState(false);
    const [originalUrl, setOriginalUrl] = useState<string>('');
    const [loadingOriginal, setLoadingOriginal] = useState(false);

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

    useEffect(() => {
        const isBlob = typeof msg.content === 'string' && msg.content.startsWith('blob:');
        setThumbUrl('');
        setDownloadUrl('');

        if (isImage) {
            if (isBlob) {
                // 1. 本地 Blob 预览，最高优先级，直接使用
                setThumbUrl(msg.content);
            } else if (msg.cache_path && window.electronAPI) {
                // 1.5 Local Cache
                window.electronAPI.fs.readFile(msg.cache_path)
                    .then((buffer) => {
                        const blob = new Blob([buffer]);
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
        } else if (isFile) {
            if (msg.cache_path && window.electronAPI) {
                window.electronAPI.fs.readFile(msg.cache_path)
                    .then((buffer) => {
                        const blob = new Blob([buffer]);
                        const url = URL.createObjectURL(blob);
                        setDownloadUrl(url);
                    })
                    .catch((err) => {
                        console.error("Failed to load local file", err);
                    });
            } else if (contentIsUrl) {
                setDownloadUrl(msg.content);
            } else if (msg.attachment_id) {
                fileService.getFileUrl(msg.attachment_id).then(res => setDownloadUrl(res.url)).catch(console.error);
            }
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
                                onClick={() => window.open(originalUrl, '_blank')}
                                disabled={!originalUrl}
                                loading={loadingOriginal}
                            >
                                Download Original
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
                        {downloadUrl && (
                            <a href={downloadUrl} target="_blank" rel="noopener noreferrer" style={{ color: isMe ? '#fff' : '#1890ff', textDecoration: 'underline' }}>
                                <DownloadOutlined /> Download
                            </a>
                        )}
                    </div>
                </div>
            );
        }

        return msg.content;
    };

    return (
        <List.Item style={{ 
            display: 'flex', 
            justifyContent: isMe ? 'flex-end' : 'flex-start',
            width: '100%',
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
                    {renderContent()}
                </div>
            </div>
        </List.Item>
    );
};

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
                rowKey={(msg) => msg.id || `${msg.room_id || ''}-${msg.timestamp || ''}-${msg.content}`}
                renderItem={(msg) => <MessageItem msg={msg} isMe={msg.from_user_id === user?.id} />}
            />
            <div ref={bottomRef} />
        </div>
    );
};

export default MessageList;
