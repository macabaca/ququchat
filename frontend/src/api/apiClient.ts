import axios, {InternalAxiosRequestConfig, AxiosResponse, AxiosError} from 'axios'
import { BASE_URL, WHITE_LIST } from "../configs/config"
import { useAuthStore } from '../stores/authStore';
import { RefreshResponse, ApiError } from '../types/api';

// API Client
// 用于统一发送请求
const apiClient = axios.create({
    baseURL: BASE_URL
});

// 检查URI是否在白名单中，如何在白名单中，不用检查 JWT
function isInWhiteList(uri: string): boolean {
    for (let i in WHITE_LIST) {
        if (i === uri) return true;
    }
    return false;
}

apiClient.interceptors.request.use((config: InternalAxiosRequestConfig) => {
    if (config.headers) {
        const uri: string = config.headers.uri
        // 如果 URI 在白名单中就不附加 JWT
        if (!isInWhiteList(uri)) {
            const token = useAuthStore.getState().accessToken;
            config.headers.Authorization = `Bearer ${token}`;
        }
    }
    return config
},
(error: AxiosError) => {
    return Promise.reject(error);
});

// 响应拦截器应自动刷新令牌
let isRefreshing = false;
let failedQueue: Array<{ resolve: (value: unknown) => void; reject: (reason: unknown) => void }> = [];

const processQueue = (error: Error | null, token: string | null = null) => {
    failedQueue.forEach(prom => {
        if (error) {
            prom.reject(error);
        } else {
            prom.resolve(token);
        }
    });
    failedQueue = [];
}


apiClient.interceptors.response.use(
    (response) => {
        return response.data;
    },
    async (error: AxiosError<ApiError>) => {
        const originalRequest = error.config as InternalAxiosRequestConfig & {_retry?: boolean };
        const { logout, setAccessToken, refreshToken } = useAuthStore.getState();

        // 如果不是 401 或 401 不是因为 JWT 过期，直接拒绝
        if (error.response?.status !== 401 || error.response.data?.error !== '访问令牌已过期') {
            return Promise.reject(error.response?.data || error.message);
        }

        // 如果正在刷新，请求挂起
        if (isRefreshing) {
            return new Promise((resolve, reject) => {
                failedQueue.push({
                    resolve,
                    reject
                });
            }).then(token => {
                if (originalRequest.headers) {
                    originalRequest.headers.Authorization = `Bearer ${token}`;
                }
                return apiClient(originalRequest);
            });
        }

        // 第一个 401 开始刷新
        originalRequest._retry = true;
        isRefreshing = true;

        if (!refreshToken) {
            logout();
            isRefreshing = false;
            return Promise.reject(error.response?.data);
        }

        try {
            // 调用刷新接口，创建新的 axios 实例避免触发拦截循环
            const refreshResponse: RefreshResponse = (
                await axios.post('/api/auth/refresh', {
                    refresh_token: refreshToken
                })
            ).data;

            // 刷新成功
            const { accessToken: newAccessToken, refreshToken: newRefreshToken } = refreshResponse;

            // 更新 Store
            useAuthStore.getState().login({
                ...useAuthStore.getState(),     // 保留 user
                accessToken: newAccessToken,
                refreshToken: newRefreshToken,  // 更新轮换后的 refresh token
                user: useAuthStore.getState().user! // 确保 user 不被清空
            });

            // 更新当前请求的 header
            if (originalRequest.headers) {
                originalRequest.headers['Authorization'] = `Bearer ${newAccessToken}`;
            }

            // 处理挂起的队列
            processQueue(null, newAccessToken);

            // 重试原始请求
            return apiClient(originalRequest);
        } catch (refreshError: any) {
            // 刷新失败
            processQueue(refreshError, null);
            logout();

            return Promise.reject(refreshError.respoonse?.data || refreshError.message);
        } finally {
            isRefreshing = false;
        }
    }
);

export default apiClient;