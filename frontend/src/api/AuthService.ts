import apiClient from "./apiClient";
import {
   LoginRequest, LoginResponse, LoginError,
   RegisterRequest, RegisterResponse, RegisterError 
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
    }
}