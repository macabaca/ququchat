import React, { useEffect } from 'react';
import TitleBar from '../common/TitleBar';
import { Layout } from 'antd';
import Sidebar from './Sidebar';
import ChatWindow from './ChatWindow';
import { useChatStore } from '../../stores/chatStore';
import { useAuthStore } from '../../stores/authStore';

const ChatLayout: React.FC = () => {
    const { init, disconnectWebSocket } = useChatStore();
    const user = useAuthStore((state) => state.user);

    useEffect(() => {
        if (user) {
            init();
        }
        return () => {
            disconnectWebSocket();
        };
    }, [user, init, disconnectWebSocket]);

    return (
        <div style={{ height: '100vh', display: 'flex', flexDirection: 'column' }}>
            <TitleBar />
            <div style={{ flex: 1, marginTop: '60px', display: 'flex', overflow: 'hidden' }}>
                <Layout style={{ height: '100%', width: '100%' }}>
                    <Sidebar />
                    <ChatWindow />
                </Layout>
            </div>
        </div>
    );
};

export default ChatLayout;
