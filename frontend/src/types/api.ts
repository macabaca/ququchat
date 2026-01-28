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
