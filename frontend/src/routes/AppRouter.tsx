import React from 'react';
import { BrowserRouter as Router, Routes, Route, Navigate } from 'react-router-dom';

import { useAuthStore } from '../stores/authStore';
import LoginForm from '../components/auth/LoginForm';
import RegisterForm from '../components/auth/RegisterForm';
import AuthLayout from '../components/layout/AuthLayout';
import ChatLayout from '../components/chat/ChatLayout';
import PageTransition from '../components/common/PageTransition';

const AppRouter: React.FC = () => {
    const isAuthenticated = useAuthStore((state: any) => state.isAuthenticated);
    // You might want to use a global loading state if hydration takes time, 
    // but with synchronous localStorage, it's usually instant. 
    // However, if we added async token verification on mount, we'd use this.
    // const isLoading = useAuthStore((state: any) => state.isLoading);

    // if (isLoading) {
    //    return <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100vh' }}><Spin size="large" /></div>;
    // }

    return (
        <Router>
            <PageTransition>
                <Routes>
                    <Route 
                        path='/login'
                        element={
                            isAuthenticated ? (
                                <Navigate to='/' replace />
                            ) : (
                                <AuthLayout>
                                    <LoginForm />
                                </AuthLayout>
                            )
                        }
                    />
                    <Route 
                        path='/register'
                        element={
                            isAuthenticated ? (
                                <Navigate to='/' replace />
                            ) : (
                                <AuthLayout>
                                    <RegisterForm />
                                </AuthLayout>
                            )
                        }
                    />
                    <Route
                        path="/"
                        element={
                            isAuthenticated ? (
                                <ChatLayout />
                            ) : (
                                <Navigate to="/login" replace />
                            )
                        }
                    />
                </Routes>
            </PageTransition>
        </Router>
    )
}

export default AppRouter;
