import React, { Component, ErrorInfo, ReactNode } from 'react';
import { Result, Button } from 'antd';

interface Props {
  children: ReactNode;
}

interface State {
  hasError: boolean;
  error: Error | null;
  errorInfo: ErrorInfo | null;
}

class ErrorBoundary extends Component<Props, State> {
  public state: State = {
    hasError: false,
    error: null,
    errorInfo: null,
  };

  public static getDerivedStateFromError(error: Error): State {
    // 当错误发生时，更新 state，以便下一次渲染能够显示降级 UI
    return { hasError: true, error, errorInfo: null };
  }

  public componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    // 你也可以将错误日志上报给服务器
    console.error("ErrorBoundary caught an error:", error, errorInfo);
    this.setState({ errorInfo });
  }

  // 尝试重置状态并重新渲染子组件
  private handleTryAgain = () => {
    this.setState({ hasError: false, error: null, errorInfo: null });
  };

  public render() {
    if (this.state.hasError) {
      // 渲染自定义的降级 UI
      return (
        <div style={{ margin: '20px' }}>
            <Result
                status="error"
                title="应用渲染出错"
                subTitle="抱歉，组件渲染时发生了一个意外错误。"
                extra={[
                    <Button type="primary" key="try-again" onClick={this.handleTryAgain}>
                        尝试重新渲染
                    </Button>,
                ]}
            >
                <div style={{ background: 'rgba(0,0,0,0.05)', padding: '16px', borderRadius: '4px' }}>
                    <details>
                        <summary>点击查看错误详情</summary>
                        <pre style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all', marginTop: '10px' }}>
                            {this.state.error && this.state.error.toString()}
                            <br />
                            {this.state.errorInfo && this.state.errorInfo.componentStack}
                        </pre>
                    </details>
                </div>
            </Result>
        </div>
      );
    }

    // 如果没有错误，正常渲染子组件
    return this.props.children;
  }
}

export default ErrorBoundary;