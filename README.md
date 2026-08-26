# Mini Eureka

Mini Eureka 是一个 Go 编写的轻量级 AP 注册中心。它使用分片内存字典、TTL 时间轮和无主 Gossip，在节点部分失联时继续接受注册与发现，并在网络恢复后自动收敛。React 控制台把实例健康、成员拓扑和真实 Gossip delivery receipt 转换为可操作的状态墙与传播飞线。

## 1. 能力概览

- 64 分片 `service -> instance` 二级 Map，点查、注册、心跳和注销平均 O(1)。
- 15 秒延迟、30 秒摘除、60 秒红色投影和紧凑版本栅栏。
- HLC + lease epoch + lease ID，旧客户端不能续租或删除新租约。
- 随机扇出、UDP 小消息、HTTP 分页反熵、HMAC 和重放防护。
- `ALIVE/SUSPECT/DEAD` 成员状态、seed 引导与无 Leader 自愈。
- React 实例墙、筛选、详情、稳定拓扑、真实 delivery 飞线和 WebSocket/轮询降级。
- 三节点 Docker Compose，浏览器只访问一个 localhost 入口。

边界：V1 的注册状态和版本栅栏都在内存中。单节点重启可从同伴反熵恢复；全部节点同时丢失内存后由客户端重新注册。项目不提供 Raft/强一致、全节点断电恢复、认证平台或 Eureka 协议兼容层。

## 2. 架构

```text
                             ┌─ registry-1 ─┐
service clients ─ REST ──────┼─ registry-2 ─┼─ UDP Gossip / HTTP anti-entropy
                             └─ registry-3 ─┘
                                      │
browser :18780 ─ frontend nginx ─ REST snapshot + WebSocket events
```

三个注册节点都可写。Nginx 以同源方式代理 API/WS，并在上游失败时切换节点。完整并发、租约和合并规则见 [Architecture](docs/Architecture.md)，接口见 [API](docs/API.md)。

## 3. 快速启动

前置条件：Docker Engine 与 Docker Compose。支持 AMD64/ARM64 基础镜像。

```bash
cp .env.example .env
# 非本地演示必须修改 MINIEUREKA_GOSSIP_SECRET
docker compose up --build
```

打开：<http://localhost:18780>

调试 API：

- node-1：<http://127.0.0.1:18781/healthz>
- node-2：<http://127.0.0.1:18782/healthz>
- node-3：<http://127.0.0.1:18783/healthz>

停止服务：

```bash
docker compose down
```

本项目不使用数据 volume；`down` 后客户端应重新注册。不要使用 `docker compose down -v` 作为普通操作模板，因为该命令可能删除同一 Compose 项目的其他 volume。

## 4. 控制台演示

默认 Compose 启用演示模式，并由 node-1 生成演示服务和心跳：

1. 在实例墙按服务、节点或状态筛选。
2. 打开带“演示”标记的实例详情。
3. 点击“模拟下线”，在自定义确认框中确认。
4. 观察该 event ID 对应的红色状态、接收端 delivery 飞线和事件时间线。
5. 其他实例继续心跳；红色记录在展示窗口结束后从控制台消失，但紧凑版本栅栏继续阻止旧租约复活。

飞线来自目标节点验签并 Apply 后返回的 receipt；UDP `sendto` 成功本身不会被画成送达。

## 5. 服务接入

### 注册

```bash
curl -sS -X POST http://localhost:18780/api/v1/services/orders/instances \
  -H 'Content-Type: application/json' \
  -d '{
    "instance_id":"orders-1",
    "registration_id":"reg-orders-1-boot-a",
    "host":"127.0.0.1",
    "port":9001,
    "protocol":"http",
    "metadata":{"zone":"local"}
  }'
```

保存响应中的 `lease_id`。相同 `registration_id` 可安全重试；一次真正的新注册使用新的 registration ID。

### 心跳

建议每 10 秒心跳，默认租约 TTL 为 30 秒：

```bash
curl -sS -X PUT http://localhost:18780/api/v1/services/orders/instances/orders-1/heartbeat \
  -H 'Content-Type: application/json' \
  -d '{"lease_id":"<lease_id>","operation_id":"heartbeat-0001"}'
```

### 发现

```bash
curl -sS http://localhost:18780/api/v1/services/orders/instances
```

发现结果只含 `ACTIVE`/`DELAYED`。控制台通过 `/api/v1/dashboard/snapshot` 读取仍在红色展示窗口内的 `EVICTED` 投影。

## 6. 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `NODE_ID` | 必填 | 稳定节点身份，集群内唯一 |
| `CLUSTER_ID` | `minieureka-local` | 防止不同集群互相接纳 |
| `HTTP_ADDR` | `:8080` | HTTP 监听地址 |
| `HTTP_ADVERTISE_ADDR` | 自动 | peer 可访问的 HTTP 地址 |
| `GOSSIP_ADDR` | `:7946` | UDP 监听地址 |
| `GOSSIP_ADVERTISE_ADDR` | 自动 | peer 可访问的 Gossip 地址 |
| `GOSSIP_SEEDS` | 空 | 逗号分隔 seed 地址；首节点可为空 |
| `GOSSIP_SECRET` | 必填 | 节点消息 HMAC 密钥；不得写入日志 |
| `LOG_LEVEL` | `info` | `debug/info/warn/error` |
| `RATE_LIMIT_PER_MINUTE` | `10000`（Compose 演示为 `500000`） | 单节点按来源 IP 的每分钟 API 请求上限；压测后生产环境应按容量与入口策略调低 |
| `DEMO_MODE` | `false` | 启用演示管理端点 |
| `DEMO_SEED` | `false` | 当前节点生成演示实例和心跳 |

阈值、分片、事件 ring、请求体和同步上限的完整配置以 `backend/internal/config` 及启动校验为准。

## 7. 真实模式与演示模式

项目没有外部 provider、付费 API 或伪造的实时数据集成，因此不存在“mock 响应替代真实服务商”的切换。

`DEMO_MODE` 只控制本地演示能力：

- `false`：关闭模拟下线与网络故障端点；真实客户端仍通过相同注册/心跳/发现 API 工作。
- `true`：开放 `/api/v1/demo/*`，但模拟下线只允许 `demo=true` 的实例。
- `DEMO_SEED=true`：额外启动本地实例生成器；生产/真实接入必须关闭。

演示实例仍走正式 Registry、TTL、Gossip 和事件链路，不会绕过核心引擎或生成假飞线。QA/CI 不调用任何计费服务，成本为 `¥0`。

## 8. 测试与验证

本地工具链：

```bash
make test          # Go 单元/集成测试
make test-race     # Go race detector
make vet
make build
make frontend-test
make frontend-build
make compose-config
```

完整验证：

```bash
make verify
docker compose up --build -d
pytest -q tests/api_smoke.py
npm --prefix frontend-admin run e2e
```

每次实际 QA 命令、环境和结果记录在 `docs/QA_Record.md`；性能环境和数字记录在 `docs/Performance.md`。没有运行的项目会标为未验证或 BLOCKED，而不会写成通过。

## 9. 可观察性

- `/healthz`：进程存活，不依赖 quorum。
- `/readyz`：HTTP/UDP/worker 和首次 seed 观察已就绪。
- `/metrics`：Prometheus text format；包含请求、实例状态、Gossip、成员、事件丢弃和 goroutine 指标。
- JSON 结构化日志由 `LOG_LEVEL` 控制；不记录 Gossip secret、请求体或完整元数据值。

WebSocket 断开不会使页面空白：控制台每 3 秒用 HTTP 快照降级，并在重连后按 cursor 去重。cursor 已离开事件 ring 时执行完整 resync。

## 10. 安全说明

- V1 面向 localhost/可信内网，没有最终用户账号认证；不要直接暴露到公网。
- 节点 Gossip 与内部反熵使用共享 HMAC 密钥、时间窗和 nonce 重放防护。
- 所有 JSON、路径字段、元数据、cursor 和消息大小均必须在解析边界校验。
- Compose 示例 secret 只用于本地演示。任何非演示环境必须通过 `.env` 或部署平台注入独立的高熵 secret。
- 容器以非 root、只读根文件系统和 dropped capabilities 运行。

## 11. 常见故障

### 页面打开但没有数据

```bash
docker compose ps
docker compose logs --tail=100 frontend registry-1
curl -sS http://127.0.0.1:18781/readyz
```

WebSocket 失败时页面应显示“轮询模式”；若快照也失败，检查 Nginx upstream 与三个注册节点健康状态。

### 节点无法加入

确认所有节点的 `CLUSTER_ID` 与 `GOSSIP_SECRET` 相同，`NODE_ID` 不重复，seed 使用容器内 `host:7946` 而非宿主端口。

### 端口冲突

复制 `.env.example` 后修改 `MINIEUREKA_UI_PORT` 或 `MINIEUREKA_NODE{1,2,3}_PORT`，然后重新执行 `docker compose config --quiet`。

### 全部节点重启后实例消失

这是 V1 已声明的内存边界。服务客户端应重试注册；磁盘快照/WAL 属于 V2 候选。

## 12. 项目文档

- [需求规格](docs/Requirements.md)
- [实施路线](docs/Roadmap.md)
- [架构与并发规则](docs/Architecture.md)
- [HTTP/事件 API](docs/API.md)
- [大屏设计规范](docs/DesignSpec.md)
- [实施日志](docs/ImplementationLog.md)
- [QA 记录](docs/QA_Record.md)
- [审计报告](docs/AuditReport.md)
