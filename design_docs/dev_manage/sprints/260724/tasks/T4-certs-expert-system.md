# T4：Certificates、Expert 与 System

## 目标

完成托管证书工作流、编译产物/专家入口和系统信息页面。

## 步骤

1. 证书 list/upload/detail/delete；
2. 引用保护和私钥只输入一次提示；
3. compiled config/code viewer；
4. system/build/Envoy compatibility；
5. API tests。

## 进展

- 2026-07-22：完成。证书上传/列表/引用保护删除、私钥一次性提示、xDS/static 编译只读视图和系统兼容窗口已落地。
- 私钥未进入 query cache 或响应展示；服务端原子落盘与 `0600` 权限沿用 S5 门禁。
