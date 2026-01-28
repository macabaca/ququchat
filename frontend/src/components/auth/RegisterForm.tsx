import React from 'react';
import { Form, Input, Button, message, Spin } from 'antd';
import { UserOutlined, LockOutlined, MailOutlined, PhoneOutlined } from '@ant-design/icons';
import { Link, useNavigate } from 'react-router-dom';
import { useAuthStore } from '../../stores/authStore';
import { RegisterRequest } from '../../types/api';

const RegisterForm: React.FC = () => {
    const navigate = useNavigate();
    const register = useAuthStore((state) => state.register);
    const isLoading = useAuthStore((state) => state.isLoading);
    const error = useAuthStore((state) => state.error);

    const onFinish = async (values: any) => {
        // Construct the request object based on the API definition
        const requestData: RegisterRequest = {
            username: values.username,
            password: values.password,
            email: values.email || undefined,
            phone: values.phone || undefined,
        };

        try {
            await register(requestData);
            message.success('Registration successful! Please log in.');
            navigate('/login');
        } catch (e: any) {
            // Error is already handled in store, but we can show message here too if needed
            // The store sets the error state, which we display below
            message.error(e.message || 'Registration failed');
        }
    };

    return (
        <Spin spinning={isLoading} tip="Creating account...">
            <div style={{ position: 'relative' }}>
                <div style={{ textAlign: 'center', marginBottom: 24 }}>
                    <h2 style={{ fontSize: '24px', fontWeight: 600, color: '#333', marginBottom: '8px' }}>Create an Account</h2>
                    <p style={{ color: '#8c8c8c' }}>Join QuQu Chat today</p>
                </div>

                <Form
                    name="register"
                    initialValues={{ remember: true }}
                    onFinish={onFinish}
                    layout="vertical"
                    size="large"
                    requiredMark={false}
                >
                    <Form.Item
                        name="username"
                        rules={[
                            { required: true, message: 'Please input your username!' },
                            { whitespace: true, message: 'Username cannot be empty' }
                        ]}
                        style={{ marginBottom: 20 }}
                    >
                        <Input 
                            prefix={<UserOutlined className="login-input-prefix" />} 
                            placeholder="Username" 
                            className="login-input"
                        />
                    </Form.Item>

                    <Form.Item
                        name="email"
                        rules={[
                            { type: 'email', message: 'The input is not valid E-mail!' },
                        ]}
                        style={{ marginBottom: 20 }}
                    >
                        <Input 
                            prefix={<MailOutlined className="login-input-prefix" />} 
                            placeholder="Email (Optional)" 
                            className="login-input"
                        />
                    </Form.Item>

                    <Form.Item
                        name="phone"
                        style={{ marginBottom: 20 }}
                    >
                        <Input 
                            prefix={<PhoneOutlined className="login-input-prefix" />} 
                            placeholder="Phone (Optional)" 
                            className="login-input"
                        />
                    </Form.Item>

                    <Form.Item
                        name="password"
                        rules={[
                            { required: true, message: 'Please input your password!' },
                            { min: 6, message: 'Password must be at least 6 characters' }
                        ]}
                        style={{ marginBottom: 20 }}
                    >
                        <Input.Password 
                            prefix={<LockOutlined className="login-input-prefix" />} 
                            placeholder="Password" 
                            className="login-input"
                        />
                    </Form.Item>

                    <Form.Item
                        name="confirm"
                        dependencies={['password']}
                        hasFeedback
                        rules={[
                            { required: true, message: 'Please confirm your password!' },
                            ({ getFieldValue }) => ({
                                validator(_, value) {
                                    if (!value || getFieldValue('password') === value) {
                                        return Promise.resolve();
                                    }
                                    return Promise.reject(new Error('The two passwords that you entered do not match!'));
                                },
                            }),
                        ]}
                        style={{ marginBottom: 24 }}
                    >
                        <Input.Password 
                            prefix={<LockOutlined className="login-input-prefix" />} 
                            placeholder="Confirm Password" 
                            className="login-input"
                        />
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
                            Sign Up
                        </Button>
                    </Form.Item>

                    <div className="auth-footer-links">
                        <span style={{ color: '#6b7280' }}>Already have an account? </span>
                        <Link to="/login" style={{ marginLeft: 6, fontWeight: 600, color: '#1677ff' }}>
                            Log in
                        </Link>
                    </div>
                </Form>
            </div>
        </Spin>
    );
};

export default RegisterForm;
