import { atom } from 'jotai';
import { db } from '../api/db';
import { Room, Message } from '../types/models';

// source atoms
export const roomListAtom = atom<Room[]>([]);
export const activeRoomIdAtom = atom<string | null>(null);  // ID 是 string

// 消息不存储在原子中，只是一个缓存
export const messagesCacheAtom = atom<Record<string, Message[]>>({});   // key: roomID, value: Message[]

// derived atoms
// 只读原子，获取当前激活的消息
export const activeMessagesAtom = atom((get) => {
    const activeID = get(activeRoomIdAtom);
    if (!activeID) return [];

    const cache = get(messagesCacheAtom);
    return cache[activeID] || [];
});

// 只读原子，获取当前房间对象
export const activeRoomAtom = atom((get) => {
    const rooms = get(roomListAtom);
    const activeID = get(activeRoomIdAtom);
    return rooms.find(room => room.id === activeID) || null;
});

// 动作原子

// 从本地数据库加载消息到缓存
export const loadMessagedFromDBAtom = atom(
    null,
    async (get, set, roomID: string) => {
        // 从 Dexie 读取
        const messages = await db.messages
            .where('roomID')
            .equals(roomID)
            .sortBy('createdAt');       // 按时间排序

        // 更新内存缓存
        set(messagesCacheAtom, (prevCache) => ({
            ...prevCache,
            [roomID]: messages
        }));
    }
);

// 保存一条新消息
export const saveMessageAtom = atom(
    null,
    async (get, set, message: Message) => {
        // 写入本地数据库
        try {
            await db.messages.add(message);
        } catch (e) {
            console.error('Failed to save message to DB', e);
        }

        const cache = get(messagesCacheAtom);
        const roomMessages = cache[message.roomID] || [];

        // 检查消息是否已在缓存中
        if (!roomMessages.find(msg => msg.id === message.id)) {
            set(messagesCacheAtom, {
                ...cache,
                [message.roomID]: [...roomMessages, message],
            });
        }
    }
);

// 发送消息（由 MessageInput 调用）