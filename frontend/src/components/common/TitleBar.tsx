import React, { useEffect, useState } from 'react';
import { 
  MinusOutlined, 
  BorderOutlined, 
  CloseOutlined, 
  CompressOutlined,
  GlobalOutlined,
  QuestionCircleOutlined,
  SafetyCertificateOutlined,
  UserOutlined,
  LogoutOutlined,
  CopyOutlined,
  SettingOutlined
} from '@ant-design/icons';
import { Button, Dropdown, MenuProps, Space, message, Modal, Upload, Avatar } from 'antd';
import { useNavigate } from 'react-router-dom';
import { useAuthStore } from '../../stores/authStore';
import { useChatStore } from '../../stores/chatStore';
import { authService } from '../../api/AuthService';
import ModelConfigModal from '../ai/ModelConfigModal';
import { localFileService } from '../../api/LocalFileService';

const TitleBar: React.FC = () => {
  const [isMaximized, setIsMaximized] = useState(false);
  const [isLoggingOut, setIsLoggingOut] = useState(false);
  const [isModelModalOpen, setIsModelModalOpen] = useState(false);
  const [isAvatarModalOpen, setIsAvatarModalOpen] = useState(false);
  const [isAvatarUploading, setIsAvatarUploading] = useState(false);
  const [avatarUrl, setAvatarUrl] = useState<string>('');
  const navigate = useNavigate();
  const { isAuthenticated, user, refreshToken, logout, updateAvatar } = useAuthStore();
  const disconnectWebSocket = useChatStore((state) => state.disconnectWebSocket);

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

  const userMenuItems: MenuProps['items'] = [
    { key: 'user_code', label: `用户码：${user?.user_code ?? '-'}`, disabled: true },
    { key: 'copy_user_code', label: '复制用户码', icon: <CopyOutlined /> },
    { key: 'model_config', label: '模型配置', icon: <SettingOutlined /> },
    { key: 'update_avatar', label: '更新头像' },
    { key: 'logout', label: '退出登录', icon: <LogoutOutlined /> }
  ];

  const handleCopyUserCode = async () => {
    const code = user?.user_code;
    if (!code && code !== 0) {
      message.error('当前账号没有 user code');
      return;
    }
    try {
      await navigator.clipboard.writeText(String(code));
      message.success('已复制 user code');
    } catch {
      message.error('复制失败，请手动记录');
    }
  };

  const handleLogout = async () => {
    if (isLoggingOut) return;
    setIsLoggingOut(true);
    try {
      await authService.logout(refreshToken);
    } catch (e: any) {
      const msg = e?.error || e?.message || '退出登录失败';
      message.error(msg);
    } finally {
      console.log('[Auth][logout] manual logout via TitleBar');
      disconnectWebSocket();
      logout();
      localStorage.removeItem('chat-storage');
      setIsLoggingOut(false);
      navigate('/login', { replace: true });
    }
  };

  useEffect(() => {
    let active = true;
    const loadAvatar = async () => {
      if (!user) {
        setAvatarUrl('');
        return;
      }
      if (user.avatarThumbLocalPath) {
        const localUrl = await localFileService.getLocalFileUrl(user.avatarThumbLocalPath);
        if (!active) return;
        if (localUrl) {
          setAvatarUrl(localUrl);
          return;
        }
      }
      setAvatarUrl(user.avatarURL || '');
    };
    loadAvatar();
    return () => {
      active = false;
    };
  }, [user?.id, user?.avatarThumbLocalPath, user?.avatarURL]);

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
        {isAuthenticated && (
          <Dropdown
            menu={{
              items: userMenuItems,
              onClick: ({ key }) => {
                if (key === 'copy_user_code') handleCopyUserCode();
                if (key === 'model_config') setIsModelModalOpen(true);
                if (key === 'update_avatar') setIsAvatarModalOpen(true);
                if (key === 'logout') handleLogout();
              }
            }}
            placement="bottomRight"
            trigger={['click']}
          >
            <div className="title-bar-button" title={user?.user_code ? `${user?.username} (${user.user_code})` : (user?.username || 'User')} style={{ width: 'auto', padding: '0 10px' }}>
              <Space size={6}>
                <Avatar src={avatarUrl || undefined} size={20} icon={<UserOutlined />} />
                <span style={{ fontSize: '12px' }}>
                  {user?.username || 'User'}
                  {typeof user?.user_code === 'number' ? ` (${user.user_code})` : ''}
                </span>
              </Space>
            </div>
          </Dropdown>
        )}
      <Modal
        open={isAvatarModalOpen}
        title="更新头像"
        onCancel={() => {
          if (isAvatarUploading) return;
          setIsAvatarModalOpen(false);
        }}
        footer={null}
        destroyOnClose
      >
        <Upload
          accept="image/*"
          showUploadList={false}
          beforeUpload={async (file) => {
            if (isAvatarUploading) return false;
            setIsAvatarUploading(true);
            try {
              await updateAvatar(file as File);
              message.success('头像已更新');
              setIsAvatarModalOpen(false);
            } catch (e: any) {
              const msg = e?.error || e?.message || '头像更新失败';
              message.error(msg);
            } finally {
              setIsAvatarUploading(false);
            }
            return false;
          }}
        >
          <Button loading={isAvatarUploading} type="primary" block>
            选择图片上传
          </Button>
        </Upload>
      </Modal>
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
      <ModelConfigModal open={isModelModalOpen} onClose={() => setIsModelModalOpen(false)} />
    </div>
  );
};

export default TitleBar;
