export const ROBOT_USER_ID = '00000000-0000-0000-0000-00000000a1b2';
export const ROBOT_DISPLAY_NAME = '机器人';

// 对应 users 表
export interface User {
    id: string; // Changed to string (uuid) based on auth docs
    user_code?: number; // Added based on friend docs
    username: string;
    password?: string; // Optional in frontend
    email?: string | null;
    phone?: string | null;
    displayName?: string | null;
    nickname?: string | null; // Added based on friend docs response
    avatarURL?: string | null;
    avatarLocalPath?: string | null;
    avatarThumbLocalPath?: string | null;
    bio?: string | null;
    status?: string; // 'active'
    createdAt: string; // Date strings from JSON
    updatedAt: string;
}

// 好友信息 (Friend List Response)
export interface Friend extends User {
    // Friend specific fields if any, usually just User details
    room_id: string;
    status?: string;
    nickname?: string;
}

// 群组表
export interface Group {
    id: string;
    name: string;
    owner_id: string;
    member_count: number;
    my_role?: 'owner' | 'admin' | 'member';
    status?: 'active' | 'left' | 'dismissed';
    created_at: string;
    updated_at?: string;
}

// 群组成员
export interface GroupMember {
    user_id: string;
    username: string;
    nickname?: string;
    role: 'owner' | 'admin' | 'member';
    joined_at: string;
}

// 好友请求
export interface FriendRequest {
    id: string;
    from_user_id: string;
    to_user_id: string;
    status: 'pending' | 'accepted' | 'rejected' | 'canceled';
    message?: string | null;
    created_at: string;
    responded_at?: string | null;
    // Sender info for display
    from_user?: User; 
}

// 消息
export interface Message {
    id?: string; // Optional for pending messages
    room_id?: string;    // For group messages
    sequence_id?: number;

    type: 'friend_message' | 'group_message';
    from_user_id?: string;
    to_user_id?: string; // For friend messages

    content: string;
    timestamp?: number; // Unix timestamp

    created_at?: number;

    status?: 'sending' | 'sent' | 'failed'; // Frontend status
    
    // File/Image specific
    attachment_id?: string;
    thumb_attachment_id?: string;
    is_image?: boolean;
    cache_path?: string | null;
    payload_json?: Record<string, any> | string | null;
}

export interface Conversation {
    id: string; // user_id or group_id
    type: 'friend' | 'group';
    name: string;
    avatar?: string;
    lastMessage?: string;
    unreadCount: number;
    updatedAt: number;
}
