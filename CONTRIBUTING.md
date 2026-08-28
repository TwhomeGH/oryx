# 參與貢獻指南

欢迎参与 Oryx（本 fork）的开发！

> **项目定位**：本仓库是 [ossrs/oryx](https://github.com/ossrs/oryx) 的社区维护分支（fork），
> 作为独立社区项目持续维护。上游疑似停止维护，本 fork 会持续套用上游未处理的 PR 并独立演进。
> 贡献时请以**本 fork**（`TwhomeGH/oryx`）为准。

## 我该怎么开始？

1. **先看文档**：仓库 `docs/` 目录有完整的技术文档（部署、开发、前端、安全等），
   先读 [docs/README.md](docs/README.md) 了解全貌。
2. **找问题**：从 [Issues: good first issue](https://github.com/TwhomeGH/oryx/issues?q=is%3Aopen+is%3Aissue+label%3A%22good+first+issue%22) 开始。
   没有合适的 issue？也可以直接提新 issue 描述你想做的改进。
3. **提交 PR**：fork 本仓库 → 新建分支 → 提交 → 提 PR 到 `TwhomeGH/oryx`。
4. **等待审查**：我们会尽快 review。

## 提交 PR 的约定

- **Commit 信息用中文**：简明描述改了什么、为什么（项目惯例，便于中文维护者阅读）。
- **改动尽量小**：一个 PR 聚焦一件事，方便 review 和回滚。
- **同步更新文档**：如果改动涉及使用方式、接口、配置，请在 `docs/` 补充或修改对应文档。
- **跑测试**：
  - 前端改动：`cd ui && npm test && npm run build`
  - 后端改动：见 [docs/local-test.md](docs/local-test.md)（本机跑 CI 同款集成测试）
- **新增翻译**：若改 UI 文案，需同步更新 `ui/src/resources/locale_zh.json`、`locale_en.json`、`locale_ja.json`。

## 关于上游（ossrs/oryx）

本 fork 会**选择性**套用上游有价值的 PR（用 [scripts/pick-pr.ps1](scripts/pick-pr.ps1) 挑取），
但**不追求与上游完全同步**。遇到上游修复可以提 PR 引用它，我们会评估是否套用。

## 支持本项目

- 提 Issue / PR、参与讨论就是最好的贡献。
- 想赞助维护工作：见 [docs/fork-sponsorship.md](docs/fork-sponsorship.md)。

谢谢参与！
