# aws-ecs-bedrock-chat

Amazon ECS/Fargate + Amazon Bedrock（Claude Haiku 4.5）によるチャットアプリの PoC。
Streamlit Web UI を Docker コンテナ化し、ALB 経由でインターネット公開する構成を Terraform で IaC 管理。

---

## アーキテクチャ

![Architecture](docs/architecture.drawio.png)

| コンポーネント | 内容 |
|---|---|
| **ALB** | Internet-facing / HTTP:80 / Multi-AZ（1a・1c） |
| **ECS Fargate** | 0.25 vCPU / 0.5 GB / Streamlit Port 8501 |
| **Amazon Bedrock** | Claude Haiku 4.5（jp 推論プロファイル経由） |
| **Amazon DynamoDB** | 会話履歴の永続化（PAY_PER_REQUEST / TTL: 7日） |
| **Amazon ECR** | コンテナイメージ管理（bedrock-chat:latest） |
| **CloudWatch Logs** | ECS タスクログ（7日保持） |
| **IAM** | Task Execution Role / Task Role（最小権限） |
| **VPC** | パブリックサブネット 2AZ / NAT Gateway なし（学習用コスト最適化） |

---

## デモ

ALB の DNS 名でブラウザからアクセスし、Claude Haiku 4.5 とチャットできます。
URL に自動で `?session_id=xxxx` が付与され、**ブラウザをリロードしても会話履歴が DynamoDB から復元**されます。

![App Demo Phase5](docs/screenshots/app-demo.png)

**Phase 6: DynamoDB 会話履歴連携** — URL に `?session_id=xxxx` が付与され、リロード後も履歴が復元されます。

![App Demo Phase6](docs/screenshots/app-demo-phase6.png)

| 機能 | 説明 |
|---|---|
| チャット | Claude Haiku 4.5 に自然言語で質問できる |
| 履歴永続化 | リロード後も DynamoDB から会話履歴を自動復元 |
| 会話リセット | サイドバーのボタンで DynamoDB の履歴ごとクリア |

---

## 技術スタック

| カテゴリ | 技術 |
|---|---|
| アプリ | Python 3.11 / Streamlit / boto3 |
| コンテナ | Docker / Amazon ECR |
| オーケストレーション | Amazon ECS / AWS Fargate |
| ロードバランサー | Application Load Balancer（Multi-AZ） |
| AI | Amazon Bedrock / Claude Haiku 4.5（クロスリージョン推論プロファイル） |
| DB | Amazon DynamoDB（会話履歴の永続化） |
| IaC | Terraform（モジュール構成） |
| 監視 | Amazon CloudWatch Logs |

---

## ディレクトリ構成

```
aws-ecs-bedrock-chat/
├── app/
│   ├── app.py               # Streamlit チャットアプリ
│   └── requirements.txt
├── Dockerfile
├── .dockerignore
├── environments/
│   └── dev/
│       ├── main.tf          # モジュール統合
│       ├── variables.tf
│       ├── outputs.tf
│       └── terraform.tfvars.example
├── modules/
│   ├── vpc/                 # VPC / サブネット / IGW / ルートテーブル
│   ├── sg/                  # ALB SG / ECS Task SG
│   ├── alb/                 # ALB / Target Group / Listener
│   ├── ecs/                 # Cluster / Task Definition / Service
│   ├── iam/                 # Task Execution Role / Task Role
│   └── dynamodb/            # 会話履歴テーブル（PAY_PER_REQUEST / TTL）
└── docs/
    ├── architecture.drawio
    ├── architecture.drawio.png
    └── screenshots/
```

---

## デプロイ手順

### 前提条件

- AWS CLI 設定済み（`ap-northeast-1`）
- Terraform >= 1.5
- Docker
- Amazon Bedrock で Claude Haiku 4.5 の利用申請済み

### 1. ECR リポジトリ作成 & Docker イメージ push

```bash
# ECR リポジトリ作成
aws ecr create-repository --repository-name bedrock-chat --region ap-northeast-1

# Docker ビルド & push
aws ecr get-login-password --region ap-northeast-1 | \
  docker login --username AWS --password-stdin <ACCOUNT_ID>.dkr.ecr.ap-northeast-1.amazonaws.com

docker build -t bedrock-chat ./app
docker tag bedrock-chat:latest <ACCOUNT_ID>.dkr.ecr.ap-northeast-1.amazonaws.com/bedrock-chat:latest
docker push <ACCOUNT_ID>.dkr.ecr.ap-northeast-1.amazonaws.com/bedrock-chat:latest
```

### 2. Terraform 変数ファイル作成

```bash
cp environments/dev/terraform.tfvars.example environments/dev/terraform.tfvars
# terraform.tfvars を編集して ecr_image_uri を実際の URI に変更
```

### 3. Terraform apply

```bash
cd environments/dev
terraform init
terraform plan
terraform apply
```

### 4. アクセス確認

```bash
# ALB の DNS 名を確認
terraform output alb_dns_name

# ブラウザで http://<ALB_DNS_NAME> にアクセス
```

### 5. リソース削除

```bash
terraform destroy
```

---

## IAM 設計（最小権限）

| ロール | 権限 |
|---|---|
| **Task Execution Role** | ECR pull / CloudWatch Logs 書き込み |
| **Task Role** | `bedrock:InvokeModel`（Claude Haiku 4.5 推論プロファイル + 基盤モデル ARN のみ） |
| **Task Role** | `dynamodb:GetItem` / `dynamodb:PutItem`（会話履歴テーブルのみ） |

---

## ポイント（副業・面談説明用）

- **ECS Fargate × ALB** のコンテナ公開パターンを Terraform モジュールで実装
- **クロスリージョン推論プロファイル**（`jp.*`）経由で Claude Haiku 4.5 を呼び出す方法を習得
  - on-demand throughput 非対応モデルは推論プロファイル ARN + 基盤モデル ARN の両方を IAM で許可する必要がある
- **NAT Gateway なし**のパブリックサブネット構成で学習コストを最小化（`assign_public_ip = true`）
- ECS Service に `lifecycle { ignore_changes = [task_definition] }` を設定し、CI/CD デプロイ時の差分を抑制
- **ECS はステートレス**なため、会話履歴は DynamoDB に外出し。リロード・再起動後も履歴が保持される
- **URL クエリパラメータ**（`?session_id=xxxx`）で session_id を永続化し、Streamlit のセッション管理と組み合わせた
- DynamoDB の **TTL 設定**で 7 日後に古いセッションを自動削除し、運用コストを抑制
- `terraform destroy` で全リソースをクリーンアップ可能

---

## コスト目安（検証時）

| リソース | 概算 |
|---|---|
| ECS Fargate（0.25vCPU / 0.5GB × 1タスク） | ~$0.01/時 |
| ALB | ~$0.02/時 |
| CloudWatch Logs | 無料枠内（7日保持・少量） |
| Bedrock（Claude Haiku 4.5） | ~$0.001/1K tokens |
| DynamoDB（PAY_PER_REQUEST / 少量書き込み） | ほぼ無料枠内 |

> 検証後は `terraform destroy` でリソース削除を推奨。

---

## 関連リポジトリ

- [aws-bedrock-agent](https://github.com/satoshif1977/aws-bedrock-agent) - Bedrock Agent + Lambda FAQ ボット
- [aws-rag-knowledgebase](https://github.com/satoshif1977/aws-rag-knowledgebase) - S3 + API Gateway + Lambda + Bedrock RAG PoC
- [terraform-3tier-webapp](https://github.com/satoshif1977/terraform-3tier-webapp) - 3層 Web アーキテクチャ Terraform 実装
