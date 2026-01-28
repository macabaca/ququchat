import React from 'react';
import { Card } from 'antd';
import AppLogo from '../common/AppLogo';
import TitleBar from '../common/TitleBar';

interface AuthLayoutProps {
  children: React.ReactNode;
}

const AuthLayout: React.FC<AuthLayoutProps> = ({ children }) => {
  return (
    <>
      <TitleBar />
      <div className="auth-layout-container">
        <Card
          bordered={false}
          className="auth-card"
          bodyStyle={{ padding: '48px 40px 40px' }} 
        >
          <div className="auth-logo">
            <AppLogo size="large" />
          </div>
          {children}
        </Card>
      </div>
    </>
  );
};

export default AuthLayout;
