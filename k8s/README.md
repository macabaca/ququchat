# ququchat k8s 部署文档

## 前置条件

- docker
- kind
- kubectl
- helm
- infra 服务器已运行（PostgreSQL、Redis、RabbitMQ、Qdrant、MinIO）

---

## 启动服务

### 1. 创建 kind 集群

```bash
kind create cluster --name ququchat --config=k8s/kind-cluster.yaml
```

去掉 control-plane 的 taint（允许 ingress controller 调度）：

```bash
kubectl taint nodes ququchat-control-plane node-role.kubernetes.io/control-plane:NoSchedule-
```

### 2. 构建并加载镜像

```bash
docker build --target api -t ququchat-api:local .
kind load docker-image ququchat-api:local --name ququchat
```

### 3. 创建 ConfigMap 和 Secret

```bash
kubectl create configmap ququchat-config --from-file=config.yaml=./internal/config/config.yaml
kubectl create secret generic ququchat-secret --from-env-file=.env
```

### 4. 部署应用

```bash
kubectl apply -f k8s/app/api-deployment.yaml
kubectl apply -f k8s/app/api-service.yaml
```

### 5. 安装 ingress-nginx

```bash
helm repo add ingress-nginx https://helm-charts.itboon.top/ingress-nginx
helm repo update

helm install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx \
  --create-namespace \
  --set controller.image.registry=registry.aliyuncs.com \
  --set controller.image.image=google_containers/nginx-ingress-controller \
  --set controller.image.tag=v1.15.1 \
  --set controller.image.digest="" \
  --set controller.admissionWebhooks.patch.image.registry=registry.aliyuncs.com \
  --set controller.admissionWebhooks.patch.image.image=google_containers/kube-webhook-certgen \
  --set controller.admissionWebhooks.patch.image.tag=v1.6.9 \
  --set controller.admissionWebhooks.patch.image.digest="" \
  --set controller.hostNetwork=true \
  --set-string controller.nodeSelector."ingress-ready"=true
```

### 6. 部署 Ingress 规则

```bash
kubectl apply -f k8s/app/api-ingress.yaml
```

### 7. 验证

```bash
curl -s -o /dev/null -w "%{http_code}" http://localhost/
# 返回 200 表示成功
```

---

## 常用运维指令

### 查看状态

```bash
# 查看所有 Pod
kubectl get pods -o wide

# 查看所有服务
kubectl get svc

# 查看 Ingress
kubectl get ingress
```

### 查看日志

```bash
# 查看所有 api Pod 日志（实时）
kubectl logs -f -l app=ququchat-api --max-log-requests=2

# 查看单个 Pod 日志
kubectl logs <pod-name>

# 查看 ingress-nginx 日志
kubectl logs ingress-nginx-controller-<hash>
```

### 更新部署

```bash
# 重新构建并加载镜像
docker build --target api -t ququchat-api:local .
kind load docker-image ququchat-api:local --name ququchat

# 滚动重启
kubectl rollout restart deployment/ququchat-api

# 等待部署完成
kubectl rollout status deployment/ququchat-api
```

### 更新配置

```bash
# 更新 ConfigMap
kubectl delete configmap ququchat-config
kubectl create configmap ququchat-config --from-file=config.yaml=./internal/config/config.yaml
kubectl rollout restart deployment/ququchat-api
```

### 扩缩容

```bash
# 调整副本数
kubectl scale deployment ququchat-api --replicas=3
```

### 进入 Pod 调试

```bash
kubectl exec -it <pod-name> -- /bin/sh
```

### 端口转发（调试单个 Pod）

```bash
kubectl port-forward pod/<pod-name> 8081:8080
```

---

## 关闭服务

### 仅停止应用（保留集群）

```bash
kubectl delete -f k8s/app/
```

### 完全删除集群

```bash
kind delete cluster --name ququchat
```
