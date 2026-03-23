# CLIProxyAPI Fork 使用说明

> 这个首页 README 只说明这份 fork 在另一台电脑拉取后，想复用我这套
> CPA / Docker 配置时，还需要自己补哪些东西。
>
> 原项目说明我保留了，放到下面两个文件里：
>
> - [原版 English README](README_ORIGINAL_EN.md)
> - [原版中文 README](README_CN.md)

## 这个 fork 默认已经包含

- CLIProxyAPI 源码
- Docker 部署文件
  - `Dockerfile`
  - `docker-compose.yml`
  - `docker-build.sh`
  - `docker-build.ps1`
- 示例配置
  - `config.example.yaml`
  - `.env.example`
- 认证目录骨架
  - `auths/.gitkeep`

## 拉取后你还需要自己准备

### 1. `config.yaml`

这个文件 **不会跟着 Git 一起下来**，需要自己从示例复制：

```powershell
Copy-Item "config.example.yaml" "config.yaml"
```

至少需要按你的实际环境补这些内容：

- `api-keys`
- 各 provider 的 API Key / OAuth / base-url / model alias
- 是否启用 `usage-statistics-enabled`
- 是否启用管理面板

### 2. `auths/` 目录里的真实认证数据

`docker-compose.yml` 默认会把：

```text
./auths -> /root/.cli-proxy-api
```

也就是说下面这些 **真实数据不会在 Git 里**：

- Claude / Gemini / Codex / Qwen / iFlow 的 OAuth/token 文件
- 本地认证缓存
- 使用统计数据库 `usage_stats.db`

如果你开启了：

```yaml
usage-statistics-enabled: true
```

那么 SQLite 统计库会自动写到默认数据目录里；在当前 Docker 挂载方式下，
通常就是 `auths/usage_stats.db`。

### 3. `.env`（可选）

默认本地文件存储场景下，`.env` **不是必需**。

但如果你想固定镜像、改挂载路径、改部署模式，或者接入远程存储，
就需要自己创建 `.env`。

常见会用到的变量有：

- `CLI_PROXY_IMAGE`
- `CLI_PROXY_CONFIG_PATH`
- `CLI_PROXY_AUTH_PATH`
- `CLI_PROXY_LOG_PATH`
- `DEPLOY`
- `VERSION`
- `COMMIT`
- `BUILD_DATE`

### 4. `logs/` 目录

日志目录不随 Git 同步，建议本地先创建：

```powershell
New-Item -ItemType Directory -Force "logs" | Out-Null
```

### 5. Docker / Docker Compose 运行环境

另一台电脑至少要有：

- Docker
- Docker Compose
- 对以下端口的可用性检查

默认 `docker-compose.yml` 会暴露这些端口：

- `8317`
- `8085`
- `1455`
- `54545`
- `51121`
- `11451`

## 如果你要启用内置 Web 管理界面

请重点检查 `config.yaml` 里的这几项：

```yaml
remote-management:
  allow-remote: false
  secret-key: "请改成你自己的强密码"
  disable-control-panel: false
  panel-github-repository: "https://github.com/router-for-me/Cli-Proxy-API-Management-Center"
```

说明：

- `secret-key` 不配置的话，管理 API 会直接关闭
- `disable-control-panel: false` 时，内置控制面板才可用
- 控制面板资源默认来自上面这个 `panel-github-repository`

## `docker-compose.yml` 的默认行为

当前默认配置是：

- 镜像：`eceasy/cli-proxy-api:latest`
- `pull_policy: always`

这意味着：

- 换一台电脑直接启动时，**默认会拉远端最新镜像**
- 不一定和你这台机器当前正在跑的镜像完全一致

如果你希望多台电脑尽量一致，建议至少做一件事：

1. 在 `.env` 里固定 `CLI_PROXY_IMAGE`
2. 或者直接本地 build 后再启动

## 最小启动步骤

```powershell
git clone -b "feature/persistent-stats-v6.8.55" <你的-fork-地址>
cd "CLIProxyAPI"
Copy-Item "config.example.yaml" "config.yaml"
New-Item -ItemType Directory -Force "auths","logs" | Out-Null
docker compose up -d
```

## 一句话总结

**代码、Docker 文件、UI 面板入口配置可以跟着 Git 走；**
**真实配置、认证数据、日志、统计库这些运行态内容，需要你自己补。**
