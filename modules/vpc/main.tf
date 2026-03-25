# ── VPC ───────────────────────────────────────────────────
resource "aws_vpc" "this" {
  cidr_block           = var.vpc_cidr
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = merge(var.tags, { Name = "${var.project}-${var.env}-vpc" })
}

# ── Internet Gateway ───────────────────────────────────────
# VPC とインターネットを繋ぐゲートウェイ
resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id

  tags = merge(var.tags, { Name = "${var.project}-${var.env}-igw" })
}

# ── パブリックサブネット（2AZ）─────────────────────────────
# ALB と ECS Task を配置する（学習用: NAT Gateway コスト削減のため Public 構成）
resource "aws_subnet" "public" {
  count             = length(var.public_subnet_cidrs)
  vpc_id            = aws_vpc.this.id
  cidr_block        = var.public_subnet_cidrs[count.index]
  availability_zone = var.availability_zones[count.index]

  # ECS Task の Public IP は assign_public_ip で付与するため false
  map_public_ip_on_launch = false

  tags = merge(var.tags, {
    Name = "${var.project}-${var.env}-public-${count.index + 1}"
  })
}

# ── ルートテーブル（パブリック）────────────────────────────
resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id

  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }

  tags = merge(var.tags, { Name = "${var.project}-${var.env}-public-rt" })
}

resource "aws_route_table_association" "public" {
  count          = length(aws_subnet.public)
  subnet_id      = aws_subnet.public[count.index].id
  route_table_id = aws_route_table.public.id
}
