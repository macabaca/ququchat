import apiClient from "./apiClient";
import { 
    CreateGroupRequest, 
    CreateGroupResponse, 
    GetGroupResponse, 
    ListMyGroupsResponse, 
    GroupMemberActionRequest, 
    GroupMembersResponse 
} from "../types/api";

export const groupService = {
    createGroup: async (data: CreateGroupRequest): Promise<CreateGroupResponse> => {
        return await apiClient.post('/groups/create', data);
    },

    getMyGroups: async (): Promise<ListMyGroupsResponse> => {
        return await apiClient.get('/groups/my');
    },

    getGroupDetails: async (groupId: string): Promise<GetGroupResponse> => {
        return await apiClient.get(`/groups/${groupId}`);
    },

    dismissGroup: async (groupId: string): Promise<{ message: string }> => {
        return await apiClient.post(`/groups/${groupId}/dismiss`);
    },

    leaveGroup: async (groupId: string): Promise<{ message: string }> => {
        return await apiClient.post(`/groups/${groupId}/leave`);
    },

    addMembers: async (groupId: string, data: GroupMemberActionRequest): Promise<{ message: string; added_count: number }> => {
        return await apiClient.post(`/groups/${groupId}/members/add`, data);
    },

    removeMember: async (groupId: string, data: GroupMemberActionRequest): Promise<{ message: string }> => {
        return await apiClient.post(`/groups/${groupId}/members/remove`, data);
    },

    getGroupMembers: async (groupId: string): Promise<GroupMembersResponse> => {
        return await apiClient.get(`/groups/${groupId}/members`);
    }
};
