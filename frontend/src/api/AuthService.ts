import apiClient from "./apiClient";
import {
   LoginRequest, LoginResponse,
   RegisterRequest, RegisterResponse,
   LogoutResponse
} from '../types/api'
import { REQUEST_URI } from "../configs/config";

export const authService = {
    login: async (credentials: LoginRequest): Promise<LoginResponse> => {
        const uri = REQUEST_URI.get('login');
        if (uri === undefined) {
            throw new Error('Invalid URI');
        }
        const data = await apiClient.post<LoginResponse>(uri, credentials)
        return data;
    },
    register: async (credentials: RegisterRequest): Promise<RegisterResponse> => {
        const uri = REQUEST_URI.get('register');
        if (uri === undefined) {
            throw new Error('Invalid URI');
        }
        const data = await apiClient.post<RegisterResponse>(uri, credentials)
        
        return data;
    },
    logout: async (refreshToken?: string | null): Promise<LogoutResponse> => {
        const uri = REQUEST_URI.get('logout');
        if (uri === undefined) {
            throw new Error('Invalid URI');
        }
        const payload = refreshToken ? { refresh_token: refreshToken } : undefined;
        const data = await apiClient.post<LogoutResponse>(uri, payload);
        return data;
    }
}
