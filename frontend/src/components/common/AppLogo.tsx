import React from 'react';
import { MessageOutlined } from '@ant-design/icons';
import { Typography } from 'antd';

const { Title } = Typography;

const AppLogo: React.FC<{ size?: 'large' | 'small' }> = ({ size = 'large' }) => {
  const iconSize = size === 'large' ? 48 : 32;
  const titleLevel = size === 'large' ? 1 : 3;

  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', marginBottom: '24px' }}>
      <MessageOutlined style={{ fontSize: iconSize, color: '#1677ff', marginRight: '16px' }} />
      <Title level={titleLevel} style={{ margin: 0 }}>QuQu Chat</Title>
    </div>
  );
};

export default AppLogo;