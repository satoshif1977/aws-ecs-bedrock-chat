terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
  # Phase 4 はローカル state で管理（Phase 5 以降で S3 + DynamoDB に移行予定）
}

provider "aws" {
  region = var.aws_region
}

# ── アカウント情報 ─────────────────────────────────────────
data "aws_caller_identity" "current" {}

# ── ECR リポジトリ情報（CI/CD モジュールに ARN を渡すために取得）
data "aws_ecr_repository" "app" {
  name = var.ecr_repository_name
}

# ── 共通タグ ───────────────────────────────────────────────
locals {
  tags = {
    Project     = var.project
    Environment = var.env
    ManagedBy   = "Terraform"
  }
}

# ── VPC モジュール ─────────────────────────────────────────
# パブリックサブネット 2AZ（学習用: NAT Gateway なし構成）
module "vpc" {
  source  = "../../modules/vpc"
  project = var.project
  env     = var.env
  tags    = local.tags
}

# ── Security Group モジュール ──────────────────────────────
# ALB 用 SG / ECS Task 用 SG を作成
module "sg" {
  source         = "../../modules/sg"
  project        = var.project
  env            = var.env
  vpc_id         = module.vpc.vpc_id
  container_port = var.container_port
  tags           = local.tags
}

# ── ALB モジュール ─────────────────────────────────────────
# インターネット向け ALB + Target Group + HTTP Listener
module "alb" {
  source                = "../../modules/alb"
  project               = var.project
  env                   = var.env
  vpc_id                = module.vpc.vpc_id
  subnet_ids            = module.vpc.public_subnet_ids
  alb_security_group_id = module.sg.alb_security_group_id
  container_port        = var.container_port
  tags                  = local.tags
}

# ── DynamoDB モジュール ────────────────────────────────────
# チャット会話履歴の永続化ストア（PAY_PER_REQUEST / TTL 7日）
module "dynamodb" {
  source  = "../../modules/dynamodb"
  project = var.project
  env     = var.env
  tags    = local.tags
}

# ── IAM モジュール ─────────────────────────────────────────
# Task Execution Role / Task Role を作成
module "iam" {
  source              = "../../modules/iam"
  project             = var.project
  env                 = var.env
  region              = var.aws_region
  account_id          = data.aws_caller_identity.current.account_id
  dynamodb_table_arn  = module.dynamodb.table_arn
  tags                = local.tags
}

# ── CI/CD モジュール ───────────────────────────────────────
# GitHub Actions 用 OIDC Provider + IAM Role を作成
# アクセスキー不要で GitHub から安全に AWS 操作できる
module "cicd" {
  source              = "../../modules/cicd"
  project             = var.project
  env                 = var.env
  region              = var.aws_region
  account_id          = data.aws_caller_identity.current.account_id
  github_repo         = var.github_repo
  ecr_repository_arn  = data.aws_ecr_repository.app.arn
  tags                = local.tags
}

# ── ECS モジュール ─────────────────────────────────────────
# Cluster / Task Definition / CloudWatch Log Group / ECS Service を作成
module "ecs" {
  source                     = "../../modules/ecs"
  project                    = var.project
  env                        = var.env
  region                     = var.aws_region
  ecr_image_uri              = var.ecr_image_uri
  task_cpu                   = var.task_cpu
  task_memory                = var.task_memory
  container_port             = var.container_port
  task_execution_role_arn    = module.iam.task_execution_role_arn
  task_role_arn              = module.iam.task_role_arn
  log_retention_days         = var.log_retention_days
  desired_count              = var.desired_count
  subnet_ids                 = module.vpc.public_subnet_ids
  ecs_task_security_group_id = module.sg.ecs_task_security_group_id
  alb_target_group_arn       = module.alb.target_group_arn
  dynamodb_table_name        = module.dynamodb.table_name
  tags                       = local.tags
}
