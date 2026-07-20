# esgw e2e 镜像：run.sh 预编译的静态 linux 二进制（CGO_ENABLED=0）打入 scratch。
# 零外部依赖、无需拉取基础镜像；二进制架构由 run.sh 按 docker server arch 编译。
FROM scratch
COPY generated/esgw /esgw
ENTRYPOINT ["/esgw"]
