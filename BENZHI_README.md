# MiniEureka 评测说明

本项目是基于Go语言的分布式注册中心：基于 AP 模型、轻量级 Gossip 协议集群自愈与服务拓扑大屏（Mini Eureka），旨在解决分布式注册中心：基于 AP 模型、轻量级 Gossip 协议集群自愈与服务拓扑大屏（Mini Eureka）相关的工程问题，使用了Go、React，功能有基于 AP 模型、轻量级 Gossip、前端页面、基于分段锁与内存二级 Map 的高性能服务字、高并发心跳保活（TTL）与 Gossip。

Go 模块位于 `backend/`。评测入口：在该目录执行 `go test ./...`。
