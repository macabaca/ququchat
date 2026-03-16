import React, { useEffect, useRef } from 'react';
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
    const cacheUserAvatar = useAuthStore((state) => state.cacheUserAvatar);
    const initUserIdRef = useRef<string | null>(null);
    const resetAndReloadAI = useAIChatStore((state) => state.resetAndReload);
    const isAIViewActive = useAIChatStore((state) => state.isAIViewActive);

    useEffect(() => {
        console.log('[ChatLayout] effect start', { userId: user?.id });
        if (user?.id && initUserIdRef.current !== user.id) {
            initUserIdRef.current = user.id;
            init();
            resetAndReloadAI();
            cacheUserAvatar();
        }
        return () => {
            console.log('[ChatLayout] effect cleanup', { userId: user?.id });
            const currentUserId = useAuthStore.getState().user?.id;
            if (!currentUserId) {
                disconnectWebSocket();
                initUserIdRef.current = null;
            }
        };
    }, [user?.id, init, resetAndReloadAI, disconnectWebSocket, cacheUserAvatar]);

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
