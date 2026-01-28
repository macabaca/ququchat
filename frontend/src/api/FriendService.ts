import apiClient from "./apiClient";
import { 
    AddFriendRequest, 
    AddFriendResponse, 
    ListFriendsResponse, 
    ListFriendRequestsResponse, 
    RemoveFriendRequest, 
    RespondFriendRequestRequest, 
    RespondFriendRequestResponse 
} from "../types/api";

export const friendService = {
    listFriends: async (): Promise<ListFriendsResponse> => {
        return await apiClient.get('/friends/list');
    },

    addFriend: async (data: AddFriendRequest): Promise<AddFriendResponse> => {
        return await apiClient.post('/friends/add', data);
    },

    removeFriend: async (data: RemoveFriendRequest): Promise<{ message: string }> => {
        return await apiClient.post('/friends/remove', data);
    },

    listIncomingRequests: async (): Promise<ListFriendRequestsResponse> => {
        return await apiClient.get('/friends/requests/incoming');
    },

    respondToRequest: async (data: RespondFriendRequestRequest): Promise<RespondFriendRequestResponse> => {
        return await apiClient.post('/friends/requests/respond', data);
    }
};
