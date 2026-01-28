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
    bio?: string | null;
    status?: string; // 'active'
    createdAt: string; // Date strings from JSON
    updatedAt: string;
}

// 好友信息 (Friend List Response)
export interface Friend extends User {
    // Friend specific fields if any, usually just User details
}

// 群组表
export interface Group {
    id: string;
    name: string;
    owner_id: string;
    member_count: number;
    my_role?: 'owner' | 'admin' | 'member';
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
    type: 'friend_message' | 'group_message';
    from_user_id?: string;
    to_user_id?: string; // For friend messages
    room_id?: string;    // For group messages
    content: string;
    timestamp?: number; // Unix timestamp
    status?: 'sending' | 'sent' | 'failed'; // Frontend status
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
