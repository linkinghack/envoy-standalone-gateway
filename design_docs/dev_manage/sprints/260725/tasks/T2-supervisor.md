# T2：进程记录、接管与退避

## 目标

实现不会因管理面重启误杀数据面的 M-PROC supervisor，并对运行期崩溃提供有界恢复。

## 步骤

1. proc.json 原子记录与 PID 存活/身份检查；
2. Runner/Process 生产与 fake 实现；
3. epoch 0 spawn、record 一致接管和 keep/restart 分支；
4. 退出分类、指数退避、稳定窗口和 degraded；
5. fault tests 与事件证据。

## 进展

- 待开始。
