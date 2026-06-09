# Changelog

All notable changes to this project will be documented in this file.

## [Unreleased]

## [1.7.0] - 2026-06-09

### Added
- `lambda_go/healthcheck/`: ECS サービス / ALB ターゲットグループ / DynamoDB テーブルのヘルス状態を確認する Go Lambda 関数
  - AWS SDK Go v2 使用・interface によるモック設計でテスト容易性を確保
  - 環境変数: `ECS_CLUSTER_NAME` / `ECS_SERVICE_NAME` / `ALB_TARGET_GROUP_ARN` / `DYNAMODB_TABLE_NAME`
  - `main_test.go`: モックを使ったユニットテスト 14 件
- `.github/workflows/go-test.yml`: Go CI（build + test -race + vet）

## [1.6.0] - 2026-06-01

### Changed
- Bedrock モデルを `Claude 3.5 Haiku`（ap-northeast-1 で使用不可）→ `Claude Haiku 4.5` に更新
  - `retrieve_and_generate()` 用: `anthropic.claude-haiku-4-5-20251001-v1:0`（基盤モデル ARN 直指定）
  - IAM ポリシーから廃止モデル ARN を削除

## [1.5.0] - 2026-05-29

### Changed
- デフォルトブランチを `master` → `main` に統一・`master` ブランチ削除
- CI ワークフロー（`deploy.yml`）のブランチ指定を `main` のみに修正
- Dependabot: `hashicorp/aws` v5→v6（`terraform plan 0c/0d` 確認済み）・`hashicorp/time` v0.13→v0.14・`python` 3.11-slim→3.14-slim・`streamlit` >=1.57.0・`boto3` >=1.43.14 を更新
- Dependabot: `actions/checkout` v6・`actions/setup-python` v6・`hashicorp/setup-terraform` v4・`codecov/codecov-action` v6 を更新

## [1.4.1] - 2026-05-26

### Fixed
- README のディレクトリ構成に `cicd/` モジュールと `knowledge_docs/` を追記

## [1.4.0] - 2026-05-19

### Added
- CONTRIBUTING.md 追加（PR プロセス・スタイルガイド）

## [1.3.0] - 2026-05-18

### Added
- ECS タスク環境変数テーブルを README に追加

### Changed
- `MODEL_ID` / `REGION` を環境変数から取得可能に変更（ハードコード解消）

## [1.2.0] - 2026-05-12

### Added
- SECURITY.md 追加
- pyproject.toml 追加（pytest + ruff 設定）
- Dependabot 設定追加
- README にトラブルシューティング・ローカル開発テスト方法セクション追加

### Changed
- Claude 3 Haiku → Claude 3.5 Haiku（`anthropic.claude-3-5-haiku-20241022-v1:0`）に移行（EOL: 2026-09-10）
- `.gitignore` に `.ruff_cache` / `.pytest_cache` を追加

## [1.1.0] - 2026-04-13

### Added
- Phase 9: Bedrock Knowledge Base RAG 連携を追加
  - `RetrieveAndGenerate` API で Streamlit チャット画面に RAG モード追加
  - タスクロールに `InvokeModel` 権限を追加
  - AWS4 スタイルのアーキテクチャ構成図を更新

### Fixed
- RAG モデル ARN を推論プロファイル形式（`us.anthropic.claude-3-haiku-*`）に修正

## [1.0.0] - 2026-03-25

### Added
- 初回実装：ECS/Fargate + Amazon Bedrock（Claude 3 Haiku）チャットアプリ
  - Phase 1〜5: Streamlit チャット UI・Dockerfile・ECS/Fargate デプロイ
  - Phase 6: DynamoDB 会話履歴連携
  - Phase 7: ストリーミングレスポンス対応（`InvokeModelWithResponseStream`）
  - Phase 8: GitHub Actions CI/CD パイプライン追加
- Terraform IaC（ECS / Fargate / ALB / DynamoDB / IAM / CloudWatch Logs）
- GitHub Actions CI（Docker build + Checkov セキュリティスキャン）
