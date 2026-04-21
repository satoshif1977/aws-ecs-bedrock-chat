# ── CloudWatch Log Group ───────────────────────────────────
# コンテナのログ出力先
# /ecs/{project}-{env} 配下にストリームが自動作成される

resource "aws_cloudwatch_log_group" "this" {
  name              = "/ecs/${var.project}-${var.env}"
  retention_in_days = var.log_retention_days

  tags = var.tags
}

# ── ECS Cluster ────────────────────────────────────────────
# Fargate タスクを束ねる論理グループ
# Container Insights を有効にして CPU/メモリ/ネットワークを可視化

resource "aws_ecs_cluster" "this" {
  name = "${var.project}-${var.env}-cluster"

  setting {
    name  = "containerInsights"
    value = "enabled"
  }

  tags = var.tags
}

# ── Task Definition ────────────────────────────────────────
# コンテナの実行設定（どのイメージを・どのリソースで・どう動かすか）
# ECS Service がこの定義を参照して実際に起動する

resource "aws_ecs_task_definition" "this" {
  family                   = "${var.project}-${var.env}"
  requires_compatibilities = ["FARGATE"]
  network_mode             = "awsvpc" # Fargate では awsvpc 固定
  cpu                      = var.task_cpu
  memory                   = var.task_memory
  execution_role_arn       = var.task_execution_role_arn
  task_role_arn            = var.task_role_arn

  container_definitions = jsonencode([{
    name      = "app"
    image     = var.ecr_image_uri
    essential = true

    portMappings = [{
      containerPort = var.container_port
      protocol      = "tcp"
    }]

    # CloudWatch Logs へ出力する設定
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.this.name
        "awslogs-region"        = var.region
        "awslogs-stream-prefix" = "ecs"
      }
    }

    # コンテナ内の環境変数
    environment = [
      { name = "AWS_DEFAULT_REGION", value = var.region },
      { name = "DYNAMODB_TABLE_NAME", value = var.dynamodb_table_name },
      { name = "KNOWLEDGE_BASE_ID", value = var.knowledge_base_id }
    ]
  }])

  tags = var.tags
}

# ── ECS Service ────────────────────────────────────────────
# Task Definition を実際に起動・維持するサービス
# ALB の Target Group にコンテナ IP を自動登録する

resource "aws_ecs_service" "this" {
  name             = "${var.project}-${var.env}-service"
  cluster          = aws_ecs_cluster.this.id
  task_definition  = aws_ecs_task_definition.this.arn
  desired_count    = var.desired_count
  launch_type      = "FARGATE"
  platform_version = "LATEST"

  network_configuration {
    subnets         = var.subnet_ids
    security_groups = [var.ecs_task_security_group_id]
    # パブリックサブネット構成のため Public IP を付与（ECR pull / Bedrock API に必要）
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = var.alb_target_group_arn
    container_name   = "app"
    container_port   = var.container_port
  }

  # CI/CD デプロイ時に task_definition が外部から更新されても Terraform が戻さないようにする
  lifecycle {
    ignore_changes = [task_definition]
  }

  tags = var.tags
}
