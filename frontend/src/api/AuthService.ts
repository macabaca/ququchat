import apiClient from "./apiClient";
import {
   LoginRequest, LoginResponse,
   RegisterRequest, RegisterResponse,
   LogoutResponse,
   GetFileUrlResponse,
   UploadAvatarResponse
} from '../types/api'
import { REQUEST_URI } from "../configs/config";

export const authService = {
    login: async (credentials: LoginRequest): Promise<LoginResponse> => {
        const uri = REQUEST_URI.get('login');
        if (uri === undefined) {
            throw new Error('Invalid URI');
        }
        const data = await apiClient.post<LoginResponse>(uri, credentials) as unknown as LoginResponse;
        return data;
    },
    register: async (credentials: RegisterRequest): Promise<RegisterResponse> => {
        const uri = REQUEST_URI.get('register');
        if (uri === undefined) {
            throw new Error('Invalid URI');
        }
        const data = await apiClient.post<RegisterResponse>(uri, credentials) as unknown as RegisterResponse;
        return data;
    },
    logout: async (refreshToken?: string | null): Promise<LogoutResponse> => {
        const uri = REQUEST_URI.get('logout');
        if (uri === undefined) {
            throw new Error('Invalid URI');
        }
        const payload = refreshToken ? { refresh_token: refreshToken } : undefined;
        const data = await apiClient.post<LogoutResponse>(uri, payload) as unknown as LogoutResponse;
        return data;
    },
    getAvatarUrl: async (userId: string): Promise<GetFileUrlResponse> => {
        return await apiClient.get(`/users/${userId}/avatar/url`);
    },
    getAvatarThumbUrl: async (userId: string): Promise<GetFileUrlResponse> => {
        return await apiClient.get(`/users/${userId}/avatar/thumb/url`);
    },
    uploadAvatar: async (file: File): Promise<UploadAvatarResponse> => {
        const formData = new FormData();
        formData.append('file', file);
        return await apiClient.post('/users/me/avatar', formData, {
            headers: { 'Content-Type': 'multipart/form-data' }
        });
    }
}
