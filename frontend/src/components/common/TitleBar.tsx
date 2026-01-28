import React, { useState } from 'react';
import { 
  MinusOutlined, 
  BorderOutlined, 
  CloseOutlined, 
  CompressOutlined,
  GlobalOutlined,
  QuestionCircleOutlined,
  SafetyCertificateOutlined
} from '@ant-design/icons';
import { Button, Dropdown, MenuProps, Space } from 'antd';

const TitleBar: React.FC = () => {
  const [isMaximized, setIsMaximized] = useState(false);

  const handleMinimize = () => {
    window.electronAPI?.minimize();
  };

  const handleMaximize = () => {
    window.electronAPI?.maximize();
    setIsMaximized(!isMaximized);
  };

  const handleClose = () => {
    window.electronAPI?.close();
  };

  const items: MenuProps['items'] = [
    { key: '1', label: 'English' },
    { key: '2', label: '中文 (简体)' },
  ];

  return (
    <div className="title-bar">
      {/* Left: Logo & Title */}
      <div className="title-bar-left">
        <div className="app-logo-container">
           {/* Replace with actual logo or icon */}
           <div className="logo-circle">Q</div>
        </div>
        <span className="app-title">QuQu Chat</span>
      </div>

      {/* Center: Navigation Menu (Non-draggable region) */}
      <div className="title-bar-nav">
        <div className="nav-item">
          <SafetyCertificateOutlined />
          <span>Security</span>
        </div>
        <div className="nav-item">
          <QuestionCircleOutlined />
          <span>Help</span>
        </div>
        <Dropdown menu={{ items }} placement="bottom">
          <div className="nav-item">
            <GlobalOutlined />
            <span>Language</span>
          </div>
        </Dropdown>
      </div>

      {/* Right: Window Controls */}
      <div className="title-bar-controls">
        <div className="title-bar-button minimize" onClick={handleMinimize} title="Minimize">
          <MinusOutlined style={{ fontSize: '12px' }} />
        </div>
        <div className="title-bar-button maximize" onClick={handleMaximize} title={isMaximized ? "Restore" : "Maximize"}>
          {isMaximized ? <CompressOutlined style={{ fontSize: '12px' }} /> : <BorderOutlined style={{ fontSize: '12px' }} />}
        </div>
        <div className="title-bar-button close" onClick={handleClose} title="Close">
          <CloseOutlined style={{ fontSize: '12px' }} />
        </div>
      </div>
    </div>
  );
};

export default TitleBar;
