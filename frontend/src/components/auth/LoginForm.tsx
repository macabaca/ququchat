import React from 'react';
import { Form, Input, Button, message, Spin, Checkbox } from 'antd';
import { UserOutlined, LockOutlined, QrcodeOutlined } from '@ant-design/icons';
import { Link, useNavigate } from 'react-router-dom';
import { useAuthStore } from '../../stores/authStore';
import { LoginRequest } from '../../types/api';

const LoginForm: React.FC = () => {
    const navigate = useNavigate();
    const login = useAuthStore((state) => state.login);
    const isLoading = useAuthStore((state) => state.isLoading);
    const error = useAuthStore((state) => state.error);

    const onFinish = async (values: LoginRequest) => {
        try {
            await login(values);
            message.success('Welcome back!');
            navigate('/');
        } catch (e: any) {
            message.error(e.message || 'Login failed');
        }
    };

    return (
        <Spin spinning={isLoading} tip="Logging in...">
            <div style={{ position: 'relative' }}>
                <div style={{ textAlign: 'center', marginBottom: 24 }}>
                    <p style={{ color: '#8c8c8c', fontSize: '16px', margin: 0 }}>Welcome back to QuQu Chat</p>
                </div>

                {/* QR Code Login Toggle */}
                <div 
                    style={{ 
                        position: 'absolute', 
                        top: -90, 
                        right: -30, 
                        cursor: 'pointer',
                        opacity: 0.8,
                        transition: 'all 0.3s'
                    }}
                    title="Scan QR Code"
                    className="qr-code-toggle"
                >
                    <QrcodeOutlined style={{ fontSize: '28px', color: '#1677ff' }} />
                </div>

                <Form
                    name="normal_login"
                    initialValues={{ remember: true }}
                    onFinish={onFinish}
                    layout="vertical"
                    size="large"
                    requiredMark={false}
                >
                    <Form.Item
                        name="username"
                        rules={[{ required: true, message: 'Please input your username!' }]}
                        style={{ marginBottom: 20 }}
                    >
                        <Input 
                            prefix={<UserOutlined className="login-input-prefix" />} 
                            placeholder="Username / Email / Phone" 
                            className="login-input"
                        />
                    </Form.Item>
                    <Form.Item
                        name="password"
                        rules={[{ required: true, message: 'Please input your password!' }]}
                        style={{ marginBottom: 20 }}
                    >
                        <Input.Password 
                            prefix={<LockOutlined className="login-input-prefix" />} 
                            placeholder="Password" 
                            className="login-input"
                        />
                    </Form.Item>

                    <Form.Item style={{ marginBottom: 24 }}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: '14px' }}>
                            <Form.Item name="remember" valuePropName="checked" noStyle>
                                <Checkbox>Remember me</Checkbox>
                            </Form.Item>
                            <Link to="/forgot-password" style={{ color: '#1677ff', fontWeight: 500 }}>
                                Forgot password?
                            </Link>
                        </div>
                    </Form.Item>

                    {error && (
                        <div style={{ marginBottom: 24, textAlign: 'center' }}>
                            <p style={{ color: '#ff4d4f', margin: 0 }}>{error}</p>
                        </div>
                    )}

                    <Form.Item style={{ marginBottom: 24 }}>
                        <Button 
                            type="primary" 
                            htmlType="submit" 
                            className="login-form-button"
                            loading={isLoading}
                            block
                        >
                            Log in
                        </Button>
                    </Form.Item>

                    <div className="auth-footer-links">
                        <span style={{ color: '#6b7280' }}>Don't have an account? </span>
                        <Link to="/register" style={{ marginLeft: 6, fontWeight: 600, color: '#1677ff' }}>
                            Sign up now
                        </Link>
                    </div>
                </Form>
            </div>
        </Spin>
    );
};

export default LoginForm;
