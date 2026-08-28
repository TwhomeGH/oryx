# ORYX

> 注意：本仓库是 [ossrs/oryx](https://github.com/ossrs/oryx) 的社区维护分支。由于原项目疑似已停止维护，
> 本项目会持续套用上游未处理的 PR，并继续进行维护。

> Note: This is a community-maintained fork of [ossrs/oryx](https://github.com/ossrs/oryx).
> As the upstream project appears to be inactive, this fork will continue to apply outstanding
> unmerged PRs and keep the project maintained.

[![](https://img.shields.io/twitter/follow/srs_server?style=social)](https://twitter.com/srs_server)
[![](https://badgen.net/discord/members/bQUPDRqy79)](https://discord.gg/bQUPDRqy79)
[![](https://ossrs.net/wiki/images/wechat-badge4.svg)](https://ossrs.net/lts/zh-cn/contact#discussion)
[![](https://ossrs.net/wiki/images/do-btn-srs-125x20.svg)](https://marketplace.digitalocean.com/apps/srs)
[![](https://opencollective.com/srs-server/tiers/badge.svg)](https://opencollective.com/srs-server)

Oryx（原 SRS Stack）是一体化、开箱即用、开源的视频解决方案，用于在云端或自托管环境下搭建
在线视频服务，包括直播与 WebRTC。

Oryx 使用 Go、React.js、SRS、FFmpeg 构建，支持 RTMP、WebRTC、HLS、HTTP-FLV、SRT 等协议，
提供鉴权、多平台转播、录制、转码、虚拟直播、自动 HTTPS、易用的 HTTP Open API 等功能，
并集成了 Redis 与 OpenAI 服务（AI 字幕、AI 助手、视频翻译、OCR 等）。

[![](https://ossrs.io/lts/en-us/img/Oryx-5-sd.png?v=1)](https://ossrs.io/lts/en-us/img/Oryx-5-hd.png)

## 快速开始

```bash
docker run --restart always -d --name oryx \
  -v oryx-data:/data \
  -p 2022:2022 -p 2443:2443 -p 1935:1935 \
  -p 8000:8000/udp -p 10080:10080/udp \
  ghcr.io/twhomegh/oryx:v5.15.20
```

启动后打开 <http://localhost:2022> 进入管理界面。

> 在中国大陆可使用 `registry.cn-hangzhou.aliyuncs.com/ossrs/oryx:5` 加速 Docker 拉取。

## 文档

| 文档 | 内容 |
|---|---|
| [部署指南](docs/deploy-guide.md) | 快速开始、端口对照、`/data` 目录、环境变量，及部署相关文档入口 |
| [Docker 使用说明](docs/docker-usage.md) | 本 fork 自有 GHCR 镜像、compose 部署、版本更新与发布 |
| [功能列表](docs/features.md) | 已实现与规划中的功能 |
| [FAQ 与教学](docs/faq-tutorials.md) | 常见问题与各类使用场景教学 |
| [开发者指南](DEVELOPER.md) | API、架构、环境变量（开发用） |
| [全部文档索引](docs/README.md) | 本项目所有技术文档 |

## 赞助

**本 fork 与上游 [ossrs/oryx](https://github.com/ossrs/oryx) 是独立项目，赞助渠道完全分开。**

- **支持本 fork 的维护与开发**：Twitch 频道 <https://www.twitch.tv/coffeelatte0709/about>（订阅／打赏／bits 皆可）
- **支持原项目 SRS**：原项目 [OpenCollective](https://opencollective.com/srs-server)

> 赞助原项目不会直接资助本 fork 的维护。若希望本 fork 持续改进，请使用上方 Twitch 管道。
> 详细说明见 [Fork 定位与赞助管道](docs/fork-sponsorship.md)。

## 许可证

Oryx 是开源项目，使用 [MIT](https://spdx.org/licenses/MIT.html) 许可证。

我们使用的开源项目：

- [FFmpeg](https://ffmpeg.org/)：跨平台音视频录制、转换与推流解决方案。
- [Redis](https://redis.io/)：内存数据存储，广泛用于缓存、向量数据库等。
- [youtube-dl](https://github.com/ytdl-org/youtube-dl)：从 YouTube 等网站下载视频的命令行工具。
- [React.js](https://react.dev/)：Web 与原生用户界面库。
- [Go](https://golang.org/)：构建简单、安全、可扩展系统的编程语言。

2022.11
