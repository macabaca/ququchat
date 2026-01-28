import { create } from 'zustand';
import { createJSONStorage, persist, StateStorage } from 'zustand/middleware';
import { User } from "../types/models"
import { LoginRequest, RegisterRequest, RegisterResponse } from '../types/api';
import { authService } from '../api/AuthService';

// 定义 Store 的状态和操作
interface AuthState {
    accessToken: string | null;
    refreshToken: string | null;
    user: User | null;
    isAuthenticated: boolean;
    isLoading: boolean;     // 用于 UI 显示加载状态
    error: string | null; // 用于存储错误信息

    // 设置 accessToken
    setAccessToken: (token: string) => void;

    login: (credentials: LoginRequest) => Promise<void>;
    register: (credentials: RegisterRequest) => Promise<RegisterResponse>;
    logout: () => void;
}

// 创建 store
export const useAuthStore = create<AuthState>()(
    persist(
        (set) => ({
            accessToken: null,
            refreshToken: null,
            user: null,

            isAuthenticated: false,
            isLoading: false,
            error: null,

            setAccessToken: (token: string) => {
                set({ accessToken: token })
            },
            login: async (credentials: LoginRequest) => {
                set({ isLoading: true, error: null });
                try {
                    const response = await authService.login(credentials);
                    set({
                        accessToken: response.accessToken,
                        refreshToken: response.refreshToken,
                        user: response.user,
                        isAuthenticated: true,
                        isLoading: false,
                        error: null,
                    });
                } catch (error: any) {
                    const errMessage = error.error || '登录失败, 请检查您的凭据';
                    set({
                        error: errMessage,
                        isLoading: false,
                    })
                    throw new Error(errMessage);
                }
            },

            register: async (credentials: RegisterRequest) => {
                set({ isLoading: true, error: null });
                try {
                    const response = await authService.register(credentials);
                    set({ isLoading: false });
                    // 控制台打印响应便于调试
                    console.log('注册成功，用户信息: ', response.user);
                    return response;
                } catch (error: any) {
                    const errorMessage = error.error || '注册失败, 该用户名已被注册';
                    set({
                        error: errorMessage,
                        isLoading: false,
                    })
                    throw new Error(errorMessage);
                }
            },

            logout: () => {
                set({
                    accessToken: null,
                    refreshToken: null,
                    user: null,
                    isAuthenticated: false,
                    error: null,
                });
            },
        }),
        {
            name: 'auth-storage', // name of the item in the storage (must be unique)
            storage: createJSONStorage(() => localStorage),
            onRehydrateStorage: () => (state?: AuthState) =>{
                if(state){
                    state.isAuthenticated = !!state.accessToken && !!state.refreshToken;
                    state.isLoading = false;    // 重置加载状态
                    state.error = null;        // 重置错误信息
                }
            },
        }
    )
);