# ── Application Load Balancer ──────────────────────────────
# インターネット向け ALB（2 つのパブリックサブネットに配置）
resource "aws_lb" "this" {
  name               = "${var.project}-${var.env}-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [var.alb_security_group_id]
  subnets            = var.subnet_ids

  tags = var.tags
}

# ── Target Group ───────────────────────────────────────────
# Fargate は IP タイプを使用（EC2 インスタンスではなくコンテナの IP を直接登録）
resource "aws_lb_target_group" "this" {
  name        = "${var.project}-${var.env}-tg"
  port        = var.container_port
  protocol    = "HTTP"
  vpc_id      = var.vpc_id
  target_type = "ip"

  health_check {
    path                = "/"
    healthy_threshold   = 2
    unhealthy_threshold = 3
    timeout             = 5
    interval            = 30
    matcher             = "200"
  }

  tags = var.tags
}

# ── HTTP Listener ──────────────────────────────────────────
# ポート 80 で受け付けて Target Group へ転送
resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.this.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.this.arn
  }
}
