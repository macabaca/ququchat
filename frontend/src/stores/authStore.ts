import { create } from 'zustand';
import { createJSONStorage, persist } from 'zustand/middleware';
import { User } from "../types/models"
import { LoginRequest, RegisterRequest, RegisterResponse } from '../types/api';
import { authService } from '../api/AuthService';
import { localFileService } from '../api/LocalFileService';

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
    setTokens: (accessToken: string, refreshToken: string) => void;

    login: (credentials: LoginRequest) => Promise<void>;
    register: (credentials: RegisterRequest) => Promise<RegisterResponse>;
    logout: () => void;
    cacheUserAvatar: () => Promise<void>;
    updateAvatar: (file: File) => Promise<void>;
}

// 创建 store
export const useAuthStore = create<AuthState>()(
    persist(
        (set, get) => ({
            accessToken: null,
            refreshToken: null,
            user: null,

            isAuthenticated: false,
            isLoading: false,
            error: null,

            setAccessToken: (token: string) => {
                set({ accessToken: token })
            },
            setTokens: (accessToken: string, refreshToken: string) => {
                set({ accessToken, refreshToken, isAuthenticated: true })
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
                    await get().cacheUserAvatar();
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
                    console.log(response);
                    
                    set({ isLoading: false });
                    // 控制台打印响应便于调试
                    console.log('注册成功，用户信息: ', response.user);
                    return response;
                } catch (error: any) {
                    console.log(error);
                    
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
            cacheUserAvatar: async () => {
                const currentUser = get().user;
                if (!currentUser?.id || !window.electronAPI) return;
                try {
                    const [origRes, thumbRes] = await Promise.all([
                        authService.getAvatarUrl(currentUser.id),
                        authService.getAvatarThumbUrl(currentUser.id)
                    ]);
                    const [origPath, thumbPath] = await Promise.all([
                        localFileService.downloadAndSaveAvatarUrl(currentUser.id, origRes.url, false),
                        localFileService.downloadAndSaveAvatarUrl(currentUser.id, thumbRes.url, true)
                    ]);
                    set((state) => {
                        if (!state.user || state.user.id !== currentUser.id) return {};
                        return {
                            user: {
                                ...state.user,
                                avatarLocalPath: origPath || state.user.avatarLocalPath || null,
                                avatarThumbLocalPath: thumbPath || state.user.avatarThumbLocalPath || null
                            }
                        };
                    });
                } catch (error) {
                    console.warn('cacheUserAvatar failed', error);
                }
            },
            updateAvatar: async (file: File) => {
                const currentUser = get().user;
                if (!currentUser?.id) return;
                await authService.uploadAvatar(file);
                await get().cacheUserAvatar();
            }
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
