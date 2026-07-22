# 安全清单与 M1 边界

## 上线清单

- 裸机保持管理 API `127.0.0.1:8080`；容器示例只把 8080 发布到宿主 loopback。远程访问应置于有 TLS、身份认证和访问控制的反向代理/VPN 后。
- 自动化部署通过 secret 注入至少 12 字符的 `ESGW_INITIAL_ADMIN_PASSWORD`，不要写进镜像、仓库或 shell 历史。未注入时，首次管理员引导只开放 30 分钟。
- ADS 在 M1 只允许 loopback；跨主机明文 xDS 不受支持。external xDS 使用同机 sidecar/netns。
- Envoy admin 优先 Unix socket，绝不直接暴露到公网；它没有应用层鉴权。
- `/var/lib/esgw` 使用 0700，配置使用 0640，托管私钥 `tls.key` 使用 0600；备份按密钥材料加密和限权。
- systemd unit 启用 `NoNewPrivileges`、`ProtectSystem=strict`、`ProtectHome`，仅保留 `CAP_NET_BIND_SERVICE`。不需要低端口时可在本地 unit override 中移除 capability。
- 容器以 uid/gid 65532 运行；宿主 bind directory 必须预先赋予该 UID/GID，禁止为了省事使用 0777 或 root 容器。
- 只使用受支持的 Envoy 1.37–1.39，并执行发布校验和/SBOM 检查。

## 已实现的保护

本地密码使用 Argon2id；会话为服务端可撤销 token，状态修改要求同源 cookie 与 `X-ESGW-Request: 1`；登录有速率限制。配置经严格解析、语义编译和 Envoy validate，失败不会替换 last-good。证书 API 永不返回私钥，证书/私钥写入采用临时目录与原子 rename。

## M1 明确边界

- 管理 HTTP 服务本身不终止 TLS；跨主机暴露必须由可信反向代理/VPN 保护。
- 跨主机 xDS mTLS、Kubernetes 自动发现、HA/多管理节点不在 M1。
- 托管私钥在磁盘上依赖文件权限，尚未接入 KMS/静态加密。
- all-in-one 容器的管理面和数据面共享容器生命周期；严格故障域隔离使用 separated 或 systemd。
- 统计历史、自动备份调度、在线签名/镜像推送不在 M1。

发现安全问题时请避免在公开 issue 中附带密钥、完整配置、数据库或日志中的会话信息。
