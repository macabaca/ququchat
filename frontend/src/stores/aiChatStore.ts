import { create } from 'zustand';
import { createJSONStorage, persist, StateStorage } from 'zustand/middleware';
import { aiConversationDao, aiMessageDao, AIConversationRow, AIMessageRow } from '../api/db_sqlite';
import { llmService, LLMMessage, LLMMessageRole } from '../api/LLMService';
import { useAuthStore } from './authStore';

interface AIConfig {
    apiKey: string;
    baseUrl: string;
    model: string;
    temperature: number;
}

interface AIChatState {
    config: AIConfig;
    conversations: AIConversationRow[];
    activeConversationId: string | null;
    messagesByConversation: Record<string, AIMessageRow[]>;
    isStreaming: boolean;
    error: string | null;
    isAIViewActive: boolean;
    streamingConversationId: string | null;
    currentAbortController: AbortController | null;

    init: () => Promise<void>;
    resetAndReload: () => Promise<void>;
    setConfig: (partial: Partial<AIConfig>) => void;
    setActiveConversation: (id: string) => void;
    clearActiveConversation: () => void;
    setAIViewActive: (active: boolean) => void;
    createConversation: (title?: string | null) => Promise<AIConversationRow>;
    loadMessages: (conversationId: string) => Promise<AIMessageRow[]>;
    sendMessage: (conversationId: string, content: string, signal?: AbortSignal) => Promise<AIMessageRow>;
    deleteConversation: (conversationId: string) => Promise<void>;
}

const defaultConfig: AIConfig = {
    apiKey: '',
    baseUrl: 'https://api.openai.com',
    model: 'gpt-4o-mini',
    temperature: 0.7
};

const generateId = () => {
    if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
        return crypto.randomUUID();
    }
    return `id_${Date.now()}_${Math.random().toString(16).slice(2)}`;
};

const toLLMMessages = (messages: AIMessageRow[]): LLMMessage[] => {
    return messages.map((m) => ({
        role: m.role as LLMMessageRole,
        content: m.content
    }));
};

const getUserId = () => useAuthStore.getState().user?.id || 'guest';

const getScopedKey = (key: string) => {
    const userId = getUserId();
    return `${key}:${userId}`;
};

const getPersistedConfig = (userId: string): AIConfig => {
    const raw = localStorage.getItem(`ai-chat-config:${userId}`);
    if (!raw) return defaultConfig;
    try {
        const parsed = JSON.parse(raw);
        const cfg = parsed?.state?.config;
        if (!cfg) return defaultConfig;
        const apiKey = typeof cfg?.apiKey === 'string' ? cfg.apiKey : '';
        return { ...defaultConfig, ...cfg, apiKey };
    } catch {
        return defaultConfig;
    }
};

const userScopedStorage: StateStorage = {
    getItem: (name) => localStorage.getItem(getScopedKey(name)),
    setItem: (name, value) => localStorage.setItem(getScopedKey(name), value),
    removeItem: (name) => localStorage.removeItem(getScopedKey(name))
};

export const useAIChatStore = create<AIChatState>()(
    persist(
        (set, get) => ({
            config: defaultConfig,
            conversations: [],
            activeConversationId: null,
            messagesByConversation: {},
            isStreaming: false,
            error: null,
            isAIViewActive: false,
            streamingConversationId: null,
            currentAbortController: null,

            init: async () => {
                const userId = getUserId();
                const conversations = await aiConversationDao.getAll(userId);
                const activeConversationId = conversations[0]?.id ?? null;
                set({ conversations, activeConversationId });
                if (activeConversationId) {
                    await get().loadMessages(activeConversationId);
                }
            },

            resetAndReload: async () => {
                const userId = getUserId();
                const config = getPersistedConfig(userId);
                set({
                    config,
                    conversations: [],
                    activeConversationId: null,
                    messagesByConversation: {},
                    isStreaming: false,
                    error: null
                });
                await get().init();
            },

            setConfig: (partial) => {
                set((state) => ({ config: { ...state.config, ...partial } }));
            },

            setActiveConversation: (id) => {
                set({ activeConversationId: id });
            },

            clearActiveConversation: () => {
                set({ activeConversationId: null });
            },

            setAIViewActive: (active) => {
                set({ isAIViewActive: active });
            },

            createConversation: async (title) => {
                try {
                    const userId = getUserId();
                    const now = Date.now();
                    const row: AIConversationRow = {
                        id: generateId(),
                        user_id: userId,
                        title: title ?? 'New Chat',
                        created_at: now,
                        updated_at: now
                    };
                    await aiConversationDao.upsert(row);
                    set((state) => ({
                        conversations: [row, ...state.conversations],
                        activeConversationId: row.id
                    }));
                    return row;
                } catch (error: any) {
                    const msg = error?.message || error?.error || '创建对话失败';
                    set({ error: msg });
                    throw error;
                }
            },

            loadMessages: async (conversationId) => {
                const userId = getUserId();
                const list = await aiMessageDao.listByConversation(conversationId, userId);
                set((state) => ({
                    messagesByConversation: {
                        ...state.messagesByConversation,
                        [conversationId]: list
                    }
                }));
                return list;
            },

            sendMessage: async (conversationId, content, signal) => {
                const userId = getUserId();
                const { config } = get();
                if (!config.apiKey) {
                    throw new Error('API key required');
                }

                let messages = get().messagesByConversation[conversationId];
                if (!messages) {
                    messages = await get().loadMessages(conversationId);
                }

                const now = Date.now();
                const userMessage: AIMessageRow = {
                    id: generateId(),
                    user_id: userId,
                    conversation_id: conversationId,
                    role: 'user',
                    content,
                    created_at: now
                };
                await aiMessageDao.insert(userMessage);

                const assistantMessage: AIMessageRow = {
                    id: generateId(),
                    user_id: userId,
                    conversation_id: conversationId,
                    role: 'assistant',
                    content: '',
                    created_at: now + 1
                };
                await aiMessageDao.insert(assistantMessage);

                const updatedMessages = [...messages, userMessage, assistantMessage];
                const abortController = signal ? null : new AbortController();

                set((state) => ({
                    messagesByConversation: {
                        ...state.messagesByConversation,
                        [conversationId]: updatedMessages
                    },
                    isStreaming: true,
                    streamingConversationId: conversationId,
                    currentAbortController: abortController,
                    error: null
                }));

                const history = toLLMMessages([...messages, userMessage]);

                let accumulated = '';
                try {
                    await llmService.sendMessageStream({
                        config,
                        messages: history,
                        onDelta: async (delta) => {
                            accumulated += delta;
                            await aiMessageDao.updateContent(assistantMessage.id, userId, accumulated);
                            set((state) => {
                                const list = state.messagesByConversation[conversationId] || [];
                                const next = list.map((m) =>
                                    m.id === assistantMessage.id
                                        ? { ...m, content: accumulated }
                                        : m
                                );
                                return {
                                    messagesByConversation: {
                                        ...state.messagesByConversation,
                                        [conversationId]: next
                                    }
                                };
                            });
                        },
                        signal: signal ?? abortController?.signal
                    });

                    const conv = get().conversations.find((c) => c.id === conversationId);
                    if (conv && (!conv.title || conv.title === 'New Chat')) {
                        const title = content.slice(0, 20);
                        await aiConversationDao.updateTitle(conversationId, userId, title);
                    } else {
                        await aiConversationDao.touch(conversationId, userId);
                    }

                    set((state) => ({
                        conversations: state.conversations.map((c) =>
                            c.id === conversationId
                                ? { ...c, updated_at: Date.now(), title: c.title }
                                : c
                        )
                    }));

                    return assistantMessage;
                } catch (error: any) {
                    if (error?.name !== 'AbortError') {
                        const msg = error?.message || error?.error || '模型请求失败';
                        set({ error: msg });
                    }
                    throw error;
                } finally {
                    set({ isStreaming: false, streamingConversationId: null, currentAbortController: null });
                }
            },

            deleteConversation: async (conversationId) => {
                const userId = getUserId();
                const { isStreaming, streamingConversationId, currentAbortController } = get();
                if (isStreaming && streamingConversationId === conversationId && currentAbortController) {
                    currentAbortController.abort();
                }
                await aiMessageDao.deleteByConversation(conversationId, userId);
                await aiConversationDao.delete(conversationId, userId);
                set((state) => {
                    const conversations = state.conversations.filter((c) => c.id !== conversationId);
                    const { [conversationId]: _, ...rest } = state.messagesByConversation;
                    const activeConversationId =
                        state.activeConversationId === conversationId
                            ? conversations[0]?.id ?? null
                            : state.activeConversationId;
                    return { conversations, messagesByConversation: rest, activeConversationId };
                });
            }
        }),
        {
            name: 'ai-chat-config',
            storage: createJSONStorage(() => userScopedStorage),
            partialize: (state) => ({ config: state.config })
        }
    )
);

useAuthStore.subscribe(async (state, prev) => {
    const userId = state.user?.id || 'guest';
    const prevUserId = prev.user?.id || 'guest';
    if (userId === prevUserId) return;
    await useAIChatStore.getState().resetAndReload();
});
