import React, { useEffect } from 'react';
import { Modal, Form, Input, InputNumber } from 'antd';
import { useAIChatStore } from '../../stores/aiChatStore';

interface ModelConfigModalProps {
    open: boolean;
    onClose: () => void;
}

const ModelConfigModal: React.FC<ModelConfigModalProps> = ({ open, onClose }) => {
    const [form] = Form.useForm();
    const config = useAIChatStore((state) => state.config);
    const setConfig = useAIChatStore((state) => state.setConfig);

    useEffect(() => {
        if (!open) return;
        form.setFieldsValue({
            baseUrl: config.baseUrl,
            model: config.model,
            apiKey: config.apiKey || '',
            temperature: config.temperature
        });
    }, [open, config, form]);

    const handleOk = async () => {
        const values = await form.validateFields();
        setConfig({
            baseUrl: values.baseUrl,
            model: values.model,
            apiKey: values.apiKey || '',
            temperature: values.temperature
        });
        onClose();
    };

    return (
        <Modal
            title="模型配置"
            open={open}
            onOk={handleOk}
            onCancel={onClose}
            okText="保存"
            cancelText="取消"
            destroyOnClose
        >
            <Form form={form} layout="vertical" requiredMark={false}>
                <Form.Item
                    label="Base URL"
                    name="baseUrl"
                    rules={[{ required: true, message: '请输入 Base URL' }]}
                >
                    <Input placeholder="https://api.openai.com" />
                </Form.Item>
                <Form.Item
                    label="模型"
                    name="model"
                    rules={[{ required: true, message: '请输入模型名称' }]}
                >
                    <Input placeholder="gpt-4o-mini" />
                </Form.Item>
                <Form.Item label="API Key" name="apiKey">
                    <Input.Password placeholder="sk-..." />
                </Form.Item>
                <Form.Item
                    label="Temperature"
                    name="temperature"
                    rules={[{ required: true, message: '请输入 Temperature' }]}
                >
                    <InputNumber min={0} max={2} step={0.1} style={{ width: '100%' }} />
                </Form.Item>
            </Form>
        </Modal>
    );
};

export default ModelConfigModal;
