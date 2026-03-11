import React, { useEffect, useRef, useState } from 'react';
import { Layout, Typography, Input, Button, Space, message, Popconfirm } from 'antd';
import { useAIChatStore } from '../../stores/aiChatStore';

const { Header, Content } = Layout;
const { Title, Text } = Typography;
const { TextArea } = Input;

const AIChatWindow: React.FC = () => {
    const {
        activeConversationId,
        conversations,
        messagesByConversation,
        sendMessage,
        isStreaming,
        createConversation,
        setActiveConversation,
        deleteConversation
    } = useAIChatStore();
    const [input, setInput] = useState('');
    const [isSending, setIsSending] = useState(false);
    const listRef = useRef<HTMLDivElement | null>(null);

    const activeConversation = conversations.find((c) => c.id === activeConversationId);
    const currentMessages = activeConversationId ? (messagesByConversation[activeConversationId] || []) : [];

    useEffect(() => {
        if (!activeConversationId) return;
        const el = listRef.current;
        if (!el) return;
        requestAnimationFrame(() => {
            el.scrollTop = el.scrollHeight;
        });
    }, [activeConversationId]);

    const handleSend = async () => {
        const text = input.trim();
        if (!text || !activeConversationId) return;
        setIsSending(true);
        setInput('');
        try {
            await sendMessage(activeConversationId, text);
        } catch (e: any) {
            if (e?.name !== 'AbortError') {
                const msg = e?.message || e?.error || '模型请求失败';
                message.error(msg);
            }
        } finally {
            setIsSending(false);
        }
    };

    const handleCreate = async () => {
        try {
            message.info('正在创建 AI 对话...');
            const conv = await createConversation();
            setActiveConversation(conv.id);
            message.success(`已创建 AI 对话 (${conv.id.slice(0, 8)})`);
        } catch (e: any) {
            const msg = e?.message || e?.error || '创建对话失败';
            message.error(msg);
        }
    };

    const handleDeleteActive = async () => {
        if (!activeConversationId) return;
        try {
            await deleteConversation(activeConversationId);
            message.success('对话已删除');
        } catch (e: any) {
            const msg = e?.message || e?.error || '删除对话失败';
            message.error(msg);
        }
    };

    if (!activeConversationId) {
        return (
            <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#999' }}>
                <Space direction="vertical" align="center">
                    <Text>选择或创建一个 AI 对话</Text>
                    <Text type="secondary">当前会话数：{conversations.length}</Text>
                    <Button type="primary" onClick={handleCreate}>新建对话</Button>
                </Space>
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
                <Title level={4} style={{ margin: 0 }}>{activeConversation?.title || 'AI 对话'}</Title>
                <Popconfirm
                    title="确认删除该对话？"
                    onConfirm={handleDeleteActive}
                    okText="删除"
                    cancelText="取消"
                >
                    <Button danger>删除对话</Button>
                </Popconfirm>
            </Header>
            <Content style={{ display: 'flex', flexDirection: 'column', background: '#fff', padding: '16px', gap: '12px' }}>
                <div ref={listRef} style={{ flex: 1, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: '12px' }}>
                    {currentMessages.length === 0 && (
                        <Text style={{ color: '#999' }}>开始与 AI 对话</Text>
                    )}
                    {currentMessages.map((msg) => (
                        <div
                            key={msg.id}
                            style={{
                                display: 'flex',
                                justifyContent: msg.role === 'user' ? 'flex-end' : 'flex-start'
                            }}
                        >
                            <div
                                style={{
                                    maxWidth: '70%',
                                    padding: '10px 12px',
                                    borderRadius: 8,
                                    background: msg.role === 'user' ? '#e6f7ff' : '#f5f5f5',
                                    whiteSpace: 'pre-wrap'
                                }}
                            >
                                {msg.content || ''}
                            </div>
                        </div>
                    ))}
                </div>
                <div style={{ display: 'flex', gap: '8px' }}>
                    <TextArea
                        value={input}
                        onChange={(e) => setInput(e.target.value)}
                        autoSize={{ minRows: 2, maxRows: 6 }}
                        placeholder="输入你的问题..."
                        onPressEnter={(e) => {
                            if (e.shiftKey) return;
                            e.preventDefault();
                            handleSend();
                        }}
                    />
                    <Button type="primary" onClick={handleSend} loading={isSending || isStreaming}>
                        发送
                    </Button>
                </div>
            </Content>
        </Layout>
    );
};

export default AIChatWindow;
