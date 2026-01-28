import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';
import { Friend, Group, Message, FriendRequest, Conversation } from '../types/models';
import { friendService } from '../api/FriendService';
import { groupService } from '../api/GroupService';
import { WebSocketService } from '../api/WebSocketService';
import { useAuthStore } from './authStore';

interface ChatState {
    friends: Friend[];
    groups: Group[];
    friendRequests: FriendRequest[];
    conversations: Conversation[]; // Derived or managed list of active chats
    activeConversationId: string | null;
    messages: Record<string, Message[]>; // conversationId -> messages
    
    isConnected: boolean;
    isLoading: boolean;
    error: string | null;

    wsService: WebSocketService | null;

    // Actions
    init: () => Promise<void>;
    connectWebSocket: () => void;
    disconnectWebSocket: () => void;
    
    fetchFriends: () => Promise<void>;
    fetchGroups: () => Promise<void>;
    fetchFriendRequests: () => Promise<void>;
    
    setActiveConversation: (id: string) => void;
    sendMessage: (content: string, type: 'text' | 'image') => void;
    
    // Handlers for incoming data
    handleIncomingMessage: (message: Message) => void;
}

export const useChatStore = create<ChatState>()(
    persist(
        (set, get) => ({
            friends: [],
            groups: [],
            friendRequests: [],
            conversations: [],
            activeConversationId: null,
            messages: {},
            
            isConnected: false,
            isLoading: false,
            error: null,
            
            wsService: null,

            init: async () => {
                set({ isLoading: true });
                try {
                    await Promise.all([
                        get().fetchFriends(),
                        get().fetchGroups(),
                        get().fetchFriendRequests()
                    ]);
                    get().connectWebSocket();
                } catch (error: any) {
                    set({ error: error.message });
                } finally {
                    set({ isLoading: false });
                }
            },

            connectWebSocket: () => {
                const token = useAuthStore.getState().accessToken;
                if (!token) return;

                const ws = new WebSocketService(token);
                ws.addMessageHandler(get().handleIncomingMessage);
                ws.addStatusHandler((isConnected) => set({ isConnected }));
                ws.connect();
                set({ wsService: ws });
            },

            disconnectWebSocket: () => {
                const ws = get().wsService;
                if (ws) {
                    ws.disconnect();
                    set({ wsService: null, isConnected: false });
                }
            },

            fetchFriends: async () => {
                const response = await friendService.listFriends();
                set({ friends: response.friends });
            },

            fetchGroups: async () => {
                const response = await groupService.getMyGroups();
                set({ groups: response.groups });
            },

            fetchFriendRequests: async () => {
                const response = await friendService.listIncomingRequests();
                set({ friendRequests: response.requests });
            },

            setActiveConversation: (id: string) => {
                set({ activeConversationId: id });
                // Reset unread counts logic here if implemented
            },

            sendMessage: (content: string, type: 'text' | 'image') => {
                const { activeConversationId, friends, groups, wsService, messages } = get();
                const user = useAuthStore.getState().user;
                if (!activeConversationId || !wsService || !user) return;

                // Determine if it's a friend or group chat
                const isGroup = groups.some(g => g.id === activeConversationId);
                const messageType = isGroup ? 'group_message' : 'friend_message';
                
                const msgPayload: any = {
                    type: messageType,
                    content
                };

                if (isGroup) {
                    msgPayload.room_id = activeConversationId;
                } else {
                    msgPayload.to_user_id = activeConversationId;
                }

                wsService.sendMessage(msgPayload);

                // Optimistically add message to UI
                const newMessage: Message = {
                    id: `temp-${Date.now()}`,
                    type: messageType,
                    from_user_id: user.id,
                    to_user_id: !isGroup ? activeConversationId : undefined,
                    room_id: isGroup ? activeConversationId : undefined,
                    content: content,
                    timestamp: Date.now() / 1000,
                    status: 'sending'
                };

                const chatMessages = messages[activeConversationId] || [];
                set({
                    messages: {
                        ...messages,
                        [activeConversationId]: [...chatMessages, newMessage]
                    }
                });
            },

            handleIncomingMessage: (message: Message) => {
                const { messages, user } = get();
                // Determine conversation ID
                let conversationId = '';
                
                if (message.type === 'group_message') {
                    conversationId = message.room_id!;
                } else if (message.type === 'friend_message') {
                    // If I sent it (e.g. from another device), conversation is to_user_id
                    // If I received it, conversation is from_user_id
                    const myId = useAuthStore.getState().user?.id;
                    conversationId = message.from_user_id === myId ? message.to_user_id! : message.from_user_id!;
                }

                if (!conversationId) return;

                const chatMessages = messages[conversationId] || [];
                // Check if message already exists (e.g. optimistic update confirmation)
                // Real implementation would replace the temp message
                set({
                    messages: {
                        ...messages,
                        [conversationId]: [...chatMessages, message]
                    }
                });
            }
        }),
        {
            name: 'chat-storage',
            storage: createJSONStorage(() => localStorage),
            partialize: (state) => ({ 
                // Only persist these fields
                friends: state.friends, 
                groups: state.groups,
                conversations: state.conversations,
                messages: state.messages 
            }),
        }
    )
);
