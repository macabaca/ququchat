import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';
import { Friend, Group, Message, FriendRequest, Conversation, GroupMember } from '../types/models';
import { friendService } from '../api/FriendService';
import { groupService } from '../api/GroupService';
import { WebSocketService } from '../api/WebSocketService';
import { useAuthStore } from './authStore';
import { messageService } from '../api/MessageService';
import { messageDao, roomStateDao, MessageRow } from '../api/db_sqlite';

interface ChatState {
    friends: Friend[];
    groups: Group[];
    activeGroupDetails: Group | null;
    groupMembersByGroupId: Record<string, GroupMember[]>;
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
    clearError: () => void;
    createGroup: (name: string, memberIds?: string[]) => Promise<Group>;
    fetchGroupDetails: (groupId: string) => Promise<Group>;
    fetchGroupMembers: (groupId: string) => Promise<GroupMember[]>;
    inviteGroupMembers: (groupId: string, userIds: string[]) => Promise<number>;
    removeGroupMember: (groupId: string, userId: string) => Promise<void>;
    addGroupAdmins: (groupId: string, userIds: string[]) => Promise<number>;
    dismissGroup: (groupId: string) => Promise<void>;
    leaveGroup: (groupId: string) => Promise<void>;
    
    setActiveConversation: (id: string) => void;
    clearActiveConversation: () => void;
    sendMessage: (content: string, type: 'text' | 'image' | 'file', attachmentId?: string, thumbId?: string, cachePath?: string) => void;
    
    // Handlers for incoming data
    handleIncomingMessage: (message: Message) => void;

            // Load messages from SQLite
    loadMessages: (roomId: string) => Promise<void>;

    // Incremental Sync
    syncRoomMessages: (roomId: string) => Promise<void>;
}

export const useChatStore = create<ChatState>()(
    persist(
        (set, get) => ({
            friends: [],
            groups: [],
            activeGroupDetails: null,
            groupMembersByGroupId: {},
            friendRequests: [],
            conversations: [],
            activeConversationId: null,
            messages: {},
            
            isConnected: false,
            isLoading: false,
            error: null,
            
            wsService: null,
            clearError: () => set({ error: null }),

            init: async () => {
                set({ isLoading: true });
                try {
                    await Promise.all([
                        get().fetchFriends(),
                        get().fetchGroups(),
                        get().fetchFriendRequests()
                    ]);
                    
                    // Sync messages for all rooms (friends & groups)
                    const { friends, groups } = get();
                    const syncPromises = [
                        ...friends.map(f => f.room_id ? get().syncRoomMessages(f.room_id) : Promise.resolve()),
                        ...groups.map(g => get().syncRoomMessages(g.id))
                    ];
                    await Promise.allSettled(syncPromises);

                    get().connectWebSocket();
                } catch (error: any) {
                    set({ error: error?.error || error?.message || '初始化聊天数据失败' });
                } finally {
                    set({ isLoading: false });
                }
            },

            connectWebSocket: () => {
                const token = useAuthStore.getState().accessToken;
                if (!token) return;
                if (get().wsService) return;

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
                try {
                    const response = await friendService.listFriends();
                    set({ friends: response.friends });
                } catch (error: any) {
                    const msg = error?.error || error?.message || '获取好友列表失败';
                    set({ error: msg });
                    throw error;
                }
            },

            fetchGroups: async () => {
                try {
                    const response = await groupService.getMyGroups();
                    set({ groups: response.groups });
                } catch (error: any) {
                    const msg = error?.error || error?.message || '获取群组列表失败';
                    set({ error: msg });
                    throw error;
                }
            },

            fetchFriendRequests: async () => {
                try {
                    const response = await friendService.listIncomingRequests();
                    set({ friendRequests: response.requests });
                } catch (error: any) {
                    const msg = error?.error || error?.message || '获取好友请求失败';
                    set({ error: msg });
                    throw error;
                }
            },

            createGroup: async (name: string, memberIds: string[] = []) => {
                try {
                    const response = await groupService.createGroup({ name, member_ids: memberIds });
                    set((state) => ({
                        groups: [...state.groups, response.group],
                        error: null
                    }));
                    return response.group;
                } catch (error: any) {
                    const msg = error?.error || error?.message || '创建群组失败';
                    set({ error: msg });
                    throw error;
                }
            },

            fetchGroupDetails: async (groupId: string) => {
                try {
                    const response = await groupService.getGroupDetails(groupId);
                    const detail = response.group;
                    set((state) => ({
                        activeGroupDetails: detail,
                        groups: state.groups.map((group) =>
                            group.id === detail.id ? { ...group, ...detail } : group
                        ),
                        error: null
                    }));
                    return detail;
                } catch (error: any) {
                    const msg = error?.error || error?.message || '获取群详情失败';
                    set({ error: msg });
                    throw error;
                }
            },

            fetchGroupMembers: async (groupId: string) => {
                try {
                    const response = await groupService.getGroupMembers(groupId);
                    set((state) => ({
                        groupMembersByGroupId: {
                            ...state.groupMembersByGroupId,
                            [groupId]: response.members
                        },
                        error: null
                    }));
                    return response.members;
                } catch (error: any) {
                    const msg = error?.error || error?.message || '获取群成员失败';
                    set({ error: msg });
                    throw error;
                }
            },

            inviteGroupMembers: async (groupId: string, userIds: string[]) => {
                try {
                    const response = await groupService.addMembers(groupId, { user_ids: userIds });
                    await Promise.all([get().fetchGroups(), get().fetchGroupMembers(groupId), get().fetchGroupDetails(groupId)]);
                    return response.added_count ?? 0;
                } catch (error: any) {
                    const msg = error?.error || error?.message || '邀请成员失败';
                    set({ error: msg });
                    throw error;
                }
            },

            removeGroupMember: async (groupId: string, userId: string) => {
                try {
                    await groupService.removeMember(groupId, { user_id: userId });
                    await Promise.all([get().fetchGroups(), get().fetchGroupMembers(groupId), get().fetchGroupDetails(groupId)]);
                } catch (error: any) {
                    const msg = error?.error || error?.message || '移除成员失败';
                    set({ error: msg });
                    throw error;
                }
            },

            addGroupAdmins: async (groupId: string, userIds: string[]) => {
                try {
                    const response = await groupService.addAdmins(groupId, userIds);
                    await Promise.all([get().fetchGroupMembers(groupId), get().fetchGroupDetails(groupId)]);
                    return response.updated_count ?? 0;
                } catch (error: any) {
                    const msg = error?.error || error?.message || '设置管理员失败';
                    set({ error: msg });
                    throw error;
                }
            },

            dismissGroup: async (groupId: string) => {
                try {
                    await groupService.dismissGroup(groupId);
                    await get().fetchGroups();
                    if (get().activeConversationId === groupId) {
                        set({ activeConversationId: null });
                    }
                } catch (error: any) {
                    const msg = error?.error || error?.message || '解散群组失败';
                    set({ error: msg });
                    throw error;
                }
            },

            leaveGroup: async (groupId: string) => {
                try {
                    await groupService.leaveGroup(groupId);
                    await get().fetchGroups();
                    if (get().activeConversationId === groupId) {
                        set({ activeConversationId: null });
                    }
                } catch (error: any) {
                    const msg = error?.error || error?.message || '退出群组失败';
                    set({ error: msg });
                    throw error;
                }
            },

            setActiveConversation: (id: string) => {
                set({ activeConversationId: id });
                // Load messages from SQLite when conversation becomes active
                get().loadMessages(id);
                // Reset unread counts logic here if implemented
            },
            clearActiveConversation: () => {
                set({ activeConversationId: null });
            },

            sendMessage: (content: string, type: 'text' | 'image' | 'file' = 'text', attachmentId?: string, thumbId?: string, cachePath?: string) => {
                const { activeConversationId, friends, groups, wsService, messages } = get();
                const user = useAuthStore.getState().user;
                if (!activeConversationId || !wsService || !user) return;

                const isGroup = groups.some(g => g.id === activeConversationId);

                const activeFriend = !isGroup
                    ? friends.find((f) => (f as any).room_id === activeConversationId || f.id === activeConversationId)
                    : null;

                if (!isGroup && !activeFriend) return;

                const roomId = isGroup
                    ? activeConversationId
                    : ((activeFriend as any).room_id ?? activeConversationId);

                const messageType = isGroup ? 'group_message' : 'friend_message';

                const msgPayload: any = {
                    type: messageType,
                    content
                };

                if (type === 'image' || type === 'file') {
                    // For image/file, content should be the URL/path returned by upload
                    // The 'attachmentId' parameter in sendMessage is actually the URL/content
                    msgPayload.content = content; 
                    msgPayload.is_image = (type === 'image'); // Match Go struct field
                    
                    // Add attachment_id and thumb_attachment_id if available (for UI rendering optimization)
                    if (attachmentId) {
                         msgPayload.attachment_id = attachmentId;
                    }
                    if (thumbId) {
                         msgPayload.thumb_attachment_id = thumbId;
                    }
                } else {
                    msgPayload.content = content;
                }

                if (isGroup) {
                    msgPayload.room_id = roomId;
                } else {
                    msgPayload.to_user_id = activeFriend!.id;
                }

                wsService.sendMessage(msgPayload);

                const newMessage: Message = {
                    id: `temp-${Date.now()}`,
                    type: messageType,
                    from_user_id: user.id,
                    to_user_id: !isGroup ? activeFriend!.id : undefined,
                    room_id: roomId,
                    content: content,
                    timestamp: Date.now() / 1000,
                    status: 'sending',
                    attachment_id: attachmentId,
                    thumb_attachment_id: thumbId,
                    cache_path: cachePath || null,
                    is_image: type === 'image'
                };

                const chatMessages = messages[roomId] || [];
                set({
                    messages: {
                        ...messages,
                        [roomId]: [...chatMessages, newMessage]
                    }
                });

                // Persist optimistic message immediately to SQLite, including local cache_path
                messageService.saveMessage(newMessage).catch((e) => {
                    console.error('Failed to persist outgoing message', e);
                });
            },

            handleIncomingMessage: async (message: Message) => {
                const { friends } = get();

                const myId = useAuthStore.getState().user?.id;
                let conversationId = message.room_id || '';

                if (!conversationId && message.type === 'friend_message' && myId) {
                    const otherUserId =
                        message.from_user_id === myId ? (message.to_user_id || '') : (message.from_user_id || '');
                    const friend = friends.find((f) => f.id === otherUserId);
                    conversationId = friend?.room_id || '';
                }

                if (!conversationId) return;

                const chatMessages = get().messages[conversationId] || [];
                const incomingTs = message.timestamp || Date.now() / 1000;
                let matchedTempIndex = -1;

                if (myId && message.from_user_id === myId) {
                    const idx = [...chatMessages]
                        .reverse()
                        .findIndex((m) => {
                            const isTemp = typeof m.id === 'string' && m.id.startsWith('temp-');
                            if (!isTemp) return false;
                            if (m.status !== 'sending') return false;
                            if (m.type !== message.type) return false;
                            if (m.content !== message.content) return false;
                            const dt = Math.abs((m.timestamp || 0) - incomingTs);
                            return dt <= 10;
                        });

                    if (idx !== -1) {
                        matchedTempIndex = chatMessages.length - 1 - idx;
                        const matchedTemp = chatMessages[matchedTempIndex];
                        if (!message.cache_path && matchedTemp?.cache_path) {
                            message.cache_path = matchedTemp.cache_path;
                        }
                        if (!message.attachment_id && matchedTemp?.attachment_id) {
                            message.attachment_id = matchedTemp.attachment_id;
                        }
                        if (!message.thumb_attachment_id && matchedTemp?.thumb_attachment_id) {
                            message.thumb_attachment_id = matchedTemp.thumb_attachment_id;
                        }
                    }
                }

                // Persist to SQLite using MessageService
                try {
                    const savedRow = await messageService.saveMessage(message);
                    if (savedRow.cache_path) {
                        message.cache_path = savedRow.cache_path;
                    }
                    
                    if (message.sequence_id) {
                         await roomStateDao.upsert({
                            room_id: conversationId,
                            last_sequence_id: message.sequence_id,
                            last_synced_at: Date.now()
                        });
                    }
                } catch (e) {
                    console.error('Failed to persist incoming message', e);
                }

                // If this is the active conversation, reload from SQLite or just append to state?
                // For simplicity and consistency, let's append to state but ensuring cache_path is there.
                // However, loadMessages is the source of truth now.
                // But reloading whole list on every message might be heavy. 
                // Let's stick to appending to state for real-time feel, as we already updated SQLite.
                
                const latestMessages = get().messages[conversationId] || [];
                if (message.id && latestMessages.some((m) => m.id === message.id)) {
                    return;
                }

                const normalizedMessage: Message = {
                    ...message,
                    room_id: conversationId,
                    status: 'sent'
                };

                if (myId && normalizedMessage.from_user_id === myId) {
                    if (matchedTempIndex !== -1) {
                        const realIndex = matchedTempIndex;
                        set((state) => {
                            const current = state.messages[conversationId] || [];
                            if (realIndex < 0 || realIndex >= current.length) {
                                if (normalizedMessage.id && current.some((m) => m.id === normalizedMessage.id)) {
                                    return state;
                                }
                                return {
                                    messages: {
                                        ...state.messages,
                                        [conversationId]: [...current, normalizedMessage]
                                    }
                                };
                            }
                            const next = current.slice();
                            const previous = next[realIndex];
                            next[realIndex] = {
                                ...normalizedMessage,
                                cache_path: normalizedMessage.cache_path || previous?.cache_path || null,
                                attachment_id: normalizedMessage.attachment_id || previous?.attachment_id,
                                thumb_attachment_id: normalizedMessage.thumb_attachment_id || previous?.thumb_attachment_id
                            };
                            return {
                                messages: {
                                    ...state.messages,
                                    [conversationId]: next
                                }
                            };
                        });
                        return;
                    }
                }

                set((state) => {
                    const current = state.messages[conversationId] || [];
                    if (normalizedMessage.id && current.some((m) => m.id === normalizedMessage.id)) {
                        return state;
                    }
                    return {
                        messages: {
                            ...state.messages,
                            [conversationId]: [...current, normalizedMessage]
                        }
                    };
                });
            },

            loadMessages: async (roomId: string) => {
                try {
                    const rows = await messageDao.getByRoomId(roomId, 50, 0);
                    const loadedMessages: Message[] = rows.map(row => {
                        let msg: Message;
                        if (row.payload_json) {
                            try {
                                msg = JSON.parse(row.payload_json);
                            } catch (e) {
                                msg = {
                                    id: row.id,
                                    room_id: row.room_id,
                                    sequence_id: row.sequence_id,
                                    from_user_id: row.sender_id,
                                    content: row.content_text || '',
                                    type: row.content_type as any,
                                    timestamp: row.created_at / 1000,
                                    status: row.status as any
                                };
                            }
                        } else {
                             msg = {
                                id: row.id,
                                room_id: row.room_id,
                                sequence_id: row.sequence_id,
                                from_user_id: row.sender_id,
                                content: row.content_text || '',
                                type: row.content_type as any,
                                timestamp: row.created_at / 1000,
                                status: row.status as any
                            };
                        }

                        // Ensure cache_path is populated from the row, which is the source of truth for local files
                        if (row.cache_path) {
                            msg.cache_path = row.cache_path;
                        }
                        if (row.attachment_id && !msg.attachment_id) {
                            msg.attachment_id = row.attachment_id;
                        }
                        // Ensure image flag can be recovered after restart even if payload_json is incomplete
                        if (row.content_type === 'image') {
                            msg.is_image = true;
                        }
                        
                        return msg;
                    }).reverse();

                    set(state => ({
                        messages: {
                            ...state.messages,
                            [roomId]: loadedMessages
                        }
                    }));
                } catch (e) {
                    console.error("Failed to load messages from SQLite", e);
                }
            },

            syncRoomMessages: async (roomId: string) => {
                try {
                    // 1. Get local state
                    const state = await roomStateDao.get(roomId);
                    const lastSequenceId = state?.last_sequence_id || 0;

                    // 2. Fetch from remote
                    const response = await messageService.getHistoryAfter(roomId, lastSequenceId);
                    const newMessages = response.messages;

                    if (newMessages.length === 0) {
                         // Even if no new messages, ensure we load local messages if empty
                         // But usually loadMessages is called on setActiveConversation
                         return;
                    }

                    // 3. Persist to SQLite
                    let maxSeq = lastSequenceId;
                    for (const msg of newMessages) {
                        if (msg.sequence_id && msg.sequence_id > maxSeq) {
                            maxSeq = msg.sequence_id;
                        }
                        // saveMessage handles downloading images if thumb_attachment_id is present
                        const savedRow = await messageService.saveMessage(msg);
                        if (savedRow.cache_path) {
                            msg.cache_path = savedRow.cache_path;
                        }
                    }

                    // 4. Update room state
                    await roomStateDao.upsert({
                        room_id: roomId,
                        last_sequence_id: maxSeq,
                        last_synced_at: Date.now()
                    });

                    // 5. Reload messages from SQLite to ensure consistency and get cache paths
                    await get().loadMessages(roomId);

                } catch (e) {
                    console.error(`Failed to sync room ${roomId}`, e);
                }
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
                // messages: state.messages // Messages are now loaded from SQLite
            }),
        }
    )
);
