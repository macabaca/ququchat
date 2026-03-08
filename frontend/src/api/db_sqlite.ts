import { sqliteClient } from './sqliteClient';
import { User, Message, Room } from '../types/models';

// 定义数据库表的接口
export interface UserRow {
    id: string;
    user_code: number;
    username: string;
    nickname: string | null;
    avatar_url: string | null;
    status: string;
    created_at: number;
    updated_at: number;
}

export interface RoomRow {
    id: string;
    type: string; // 'dm' | 'group'
    name: string;
    avatar: string | null;
    owner_id: string | null;
    created_at: number;
    updated_at: number;
    last_message_content: string | null;
    last_message_time: number | null;
    unread_count: number;
}

export interface MessageRow {
    id: string;
    room_id: string;
    sequence_id: number;
    sender_id: string;
    content_type: string; // 'text' | 'image' | 'file' | 'system'
    content_text: string | null;
    cache_path: string | null;          // 图片路径，只有图片时可用
    attachment_id?: string | null;
    payload_json: string | null; // JSON string
    created_at: number;
    status: string; // 'sending' | 'sent' | 'failed'
}

export interface RoomStateRow {
    room_id: string;
    last_sequence_id: number;
    last_synced_at: number;
}

// 初始化数据库表结构
export const initDatabase = async () => {
    const queries = [
        `CREATE TABLE IF NOT EXISTS users (
            id TEXT PRIMARY KEY,
            user_code INTEGER,
            username TEXT NOT NULL,
            nickname TEXT,
            avatar_url TEXT,
            status TEXT,
            created_at INTEGER,
            updated_at INTEGER
        )`,
        `CREATE TABLE IF NOT EXISTS rooms (
            id TEXT PRIMARY KEY,
            type TEXT NOT NULL,
            name TEXT,
            avatar TEXT,
            owner_id TEXT,
            created_at INTEGER,
            updated_at INTEGER,
            last_message_content TEXT,
            last_message_time INTEGER,
            unread_count INTEGER DEFAULT 0
        )`,
        `CREATE TABLE IF NOT EXISTS messages (
            id TEXT PRIMARY KEY,
            room_id TEXT NOT NULL,
            sequence_id INTEGER,
            sender_id TEXT NOT NULL,
            content_type TEXT NOT NULL,
            content_text TEXT,
            cache_path TEXT,
            attachment_id TEXT,
            payload_json TEXT,
            created_at INTEGER,
            status TEXT DEFAULT 'sent',
            FOREIGN KEY(room_id) REFERENCES rooms(id)
        )`,
        `CREATE INDEX IF NOT EXISTS idx_messages_room_seq ON messages(room_id, sequence_id)`,
        `CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at)`,
        `CREATE INDEX IF NOT EXISTS idx_messages_cache_path ON messages(cache_path)`,
        
        `CREATE TABLE IF NOT EXISTS room_states (
            room_id TEXT PRIMARY KEY,
            last_sequence_id INTEGER DEFAULT 0,
            last_synced_at INTEGER DEFAULT 0,
            FOREIGN KEY(room_id) REFERENCES rooms(id)
        )`
    ];

    for (const query of queries) {
        await sqliteClient.execute(query);
    }
    console.log('Database initialized successfully');
};

// User DAO
export const userDao = {
    async upsert(user: User) {
        const sql = `
            INSERT INTO users (id, user_code, username, nickname, avatar_url, status, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(id) DO UPDATE SET
                user_code = excluded.user_code,
                username = excluded.username,
                nickname = excluded.nickname,
                avatar_url = excluded.avatar_url,
                status = excluded.status,
                updated_at = excluded.updated_at
        `;
        const params = [
            user.id,
            user.user_code,
            user.username,
            user.nickname,
            user.avatarURL,
            user.status,
            new Date(user.createdAt).getTime(), // assuming createdAt is ISO string
            new Date(user.updatedAt).getTime()
        ];
        await sqliteClient.execute(sql, params);
    },

    async get(id: string): Promise<UserRow | null> {
        return await sqliteClient.queryOne<UserRow>('SELECT * FROM users WHERE id = ?', [id]);
    },

    async getAll(): Promise<UserRow[]> {
        return await sqliteClient.query<UserRow>('SELECT * FROM users');
    }
};

// Room DAO
export const roomDao = {
    async upsert(room: RoomRow) {
        const sql = `
            INSERT INTO rooms (id, type, name, avatar, owner_id, created_at, updated_at, last_message_content, last_message_time, unread_count)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(id) DO UPDATE SET
                type = excluded.type,
                name = excluded.name,
                avatar = excluded.avatar,
                owner_id = excluded.owner_id,
                updated_at = excluded.updated_at,
                last_message_content = excluded.last_message_content,
                last_message_time = excluded.last_message_time,
                unread_count = excluded.unread_count
        `;
        const params = [
            room.id,
            room.type,
            room.name,
            room.avatar,
            room.owner_id,
            room.created_at,
            room.updated_at,
            room.last_message_content,
            room.last_message_time,
            room.unread_count
        ];
        await sqliteClient.execute(sql, params);
    },

    async get(id: string): Promise<RoomRow | null> {
        return await sqliteClient.queryOne<RoomRow>('SELECT * FROM rooms WHERE id = ?', [id]);
    },

    async getAll(): Promise<RoomRow[]> {
        return await sqliteClient.query<RoomRow>('SELECT * FROM rooms ORDER BY updated_at DESC');
    }
};

// Message DAO
export const messageDao = {
    async upsert(msg: MessageRow) {
        const sql = `
            INSERT INTO messages (id, room_id, sequence_id, sender_id, content_type, content_text, cache_path, attachment_id, payload_json, created_at, status)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(id) DO UPDATE SET
                sequence_id = COALESCE(excluded.sequence_id, messages.sequence_id),
                content_type = COALESCE(excluded.content_type, messages.content_type),
                content_text = COALESCE(excluded.content_text, messages.content_text),
                cache_path = COALESCE(excluded.cache_path, messages.cache_path),
                attachment_id = COALESCE(excluded.attachment_id, messages.attachment_id),
                payload_json = COALESCE(excluded.payload_json, messages.payload_json),
                created_at = COALESCE(excluded.created_at, messages.created_at),
                status = COALESCE(excluded.status, messages.status)
        `;
        const params = [
            msg.id,
            msg.room_id,
            msg.sequence_id,
            msg.sender_id,
            msg.content_type,
            msg.content_text,
            msg.cache_path,
            msg.attachment_id,
            msg.payload_json,
            msg.created_at,
            msg.status
        ];
        await sqliteClient.execute(sql, params);
    },

    async getImageCache(): Promise<{ cache_path: string }[]> {
        const sql = `
            SELECT cache_path FROM messages WHERE cache_path IS NOT NULL`;

        return await sqliteClient.query<{ cache_path: string }>(sql);
    },

    async getByRoomId(roomId: string, limit: number = 50, offset: number = 0): Promise<MessageRow[]> {
        const sql = `
            SELECT * FROM messages 
            WHERE room_id = ? 
            ORDER BY sequence_id DESC, created_at DESC 
            LIMIT ? OFFSET ?
        `;
        return await sqliteClient.query<MessageRow>(sql, [roomId, limit, offset]);
    },
    
    async getAfterSequence(roomId: string, sequenceId: number): Promise<MessageRow[]> {
         const sql = `
            SELECT * FROM messages 
            WHERE room_id = ? AND sequence_id > ?
            ORDER BY sequence_id ASC
        `;
        return await sqliteClient.query<MessageRow>(sql, [roomId, sequenceId]);
    }
};

// RoomState DAO
export const roomStateDao = {
    async upsert(state: RoomStateRow) {
        const sql = `
            INSERT INTO room_states (room_id, last_sequence_id, last_synced_at)
            VALUES (?, ?, ?)
            ON CONFLICT(room_id) DO UPDATE SET
                last_sequence_id = excluded.last_sequence_id,
                last_synced_at = excluded.last_synced_at
        `;
        const params = [state.room_id, state.last_sequence_id, state.last_synced_at];
        await sqliteClient.execute(sql, params);
    },

    async get(roomId: string): Promise<RoomStateRow | null> {
        return await sqliteClient.queryOne<RoomStateRow>('SELECT * FROM room_states WHERE room_id = ?', [roomId]);
    }
};
