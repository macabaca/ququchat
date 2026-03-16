import { User, Friend, FriendRequest, Group, GroupMember } from "./models";

// Auth (Existing)
export interface RegisterRequest {
    username: string;
    password: string;
    email?: string;
    phone?: string;
}

export interface RegisterResponse {
    user: User;
}

export interface LoginRequest {
    username: string;
    password: string;
}

export interface LoginResponse {
    accessToken: string;
    refreshToken: string;
    user: User;
}

export interface RefreshResponse {
    accessToken: string;
    refreshToken: string;
}

export interface LogoutResponse {
    message: string;
}

export interface ApiError {
    error: string;
}

// Friends
export interface AddFriendRequest {
    target_user_code: number;
    message?: string;
}

export interface AddFriendResponse {
    message: string;
    request?: FriendRequest;
    friend?: Friend; // If already friends
}

export interface RemoveFriendRequest {
    target_user_code: number;
}

export interface ListFriendsResponse {
    friends: Friend[];
}

export interface ListFriendRequestsResponse {
    requests: FriendRequest[];
}

export interface RespondFriendRequestRequest {
    request_id: string;
    action: 'accept' | 'reject';
}

export interface RespondFriendRequestResponse {
    message: string;
    request: FriendRequest;
}

// Groups
export interface CreateGroupRequest {
    name: string;
    member_ids?: string[];
}

export interface CreateGroupResponse {
    group: Group;
}

export interface GetGroupResponse {
    group: Group;
}

export interface ListMyGroupsResponse {
    groups: Group[];
}

export interface GroupMemberActionRequest {
    user_id?: string; // For remove
    user_ids?: string[]; // For add
}

export interface GroupMembersResponse {
    members: GroupMember[];
}

export interface GroupAdminActionResponse {
    message: string;
    updated_count: number;
}

export interface GetHistoryResponse {
    messages: import("./models").Message[];
}

export interface Attachment {
    id: string;
    uploader_user_id: string;
    file_name: string;
    storage_key: string;
    mime_type: string;
    size_bytes: number;
    hash: string;
    storage_provider: string;
    image_width?: number;
    image_height?: number;
    thumb_attachment_id?: string;
    thumb_width?: number;
    thumb_height?: number;
    created_at: string;
}

export interface UploadFileResponse {
    attachment: Attachment;
}

export interface UploadAvatarResponse {
    avatar_attachment_id: string;
    attachment: Attachment;
}

export interface GetFileUrlResponse {
    url: string;
}
