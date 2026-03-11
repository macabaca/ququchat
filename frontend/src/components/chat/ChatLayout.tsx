import React, { useEffect } from 'react';
import TitleBar from '../common/TitleBar';
import { Layout } from 'antd';
import Sidebar from './Sidebar';
import ChatWindow from './ChatWindow';
import AIChatWindow from '../ai/AIChatWindow';
import { useChatStore } from '../../stores/chatStore';
import { useAuthStore } from '../../stores/authStore';
import { useAIChatStore } from '../../stores/aiChatStore';

const ChatLayout: React.FC = () => {
    const { init, disconnectWebSocket } = useChatStore();
    const user = useAuthStore((state) => state.user);
    const resetAndReloadAI = useAIChatStore((state) => state.resetAndReload);
    const isAIViewActive = useAIChatStore((state) => state.isAIViewActive);

    useEffect(() => {
        if (user) {
            init();
            resetAndReloadAI();
        }
        return () => {
            disconnectWebSocket();
        };
    }, [user, init, resetAndReloadAI, disconnectWebSocket]);

    return (
        <div style={{ height: '100vh', display: 'flex', flexDirection: 'column' }}>
            <TitleBar />
            <div style={{ flex: 1, marginTop: '60px', display: 'flex', overflow: 'hidden' }}>
                <Layout style={{ height: '100%', width: '100%' }}>
                    <Sidebar />
                    {isAIViewActive ? <AIChatWindow /> : <ChatWindow />}
                </Layout>
            </div>
        </div>
    );
};

export default ChatLayout;
