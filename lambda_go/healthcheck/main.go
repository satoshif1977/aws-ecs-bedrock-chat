// healthcheck は ECS サービス / ALB ターゲットグループ / DynamoDB テーブルの
// ヘルス状態を確認する Lambda 関数。
//
// 環境変数:
//
//	ECS_CLUSTER_NAME      - ECS クラスター名
//	ECS_SERVICE_NAME      - ECS サービス名
//	ALB_TARGET_GROUP_ARN  - ALB ターゲットグループの ARN
//	DYNAMODB_TABLE_NAME   - DynamoDB テーブル名
//
// Lambda が受け取るイベントは任意（スケジュール実行を想定）。
// 異常検知時: HealthSummary の Healthy フィールドが false になる（パニックはしない）。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
)

// ── インターフェース ──────────────────────────────────────────────────────────

// ECSClient は ECS サービス状態確認に必要なメソッドを定義する。
type ECSClient interface {
	DescribeServices(
		ctx context.Context,
		input *ecs.DescribeServicesInput,
		opts ...func(*ecs.Options),
	) (*ecs.DescribeServicesOutput, error)
}

// ELBV2Client は ALB ターゲットグループのヘルス確認に必要なメソッドを定義する。
type ELBV2Client interface {
	DescribeTargetHealth(
		ctx context.Context,
		input *elasticloadbalancingv2.DescribeTargetHealthInput,
		opts ...func(*elasticloadbalancingv2.Options),
	) (*elasticloadbalancingv2.DescribeTargetHealthOutput, error)
}

// DynamoDBClient は DynamoDB テーブル状態確認に必要なメソッドを定義する。
type DynamoDBClient interface {
	DescribeTable(
		ctx context.Context,
		input *dynamodb.DescribeTableInput,
		opts ...func(*dynamodb.Options),
	) (*dynamodb.DescribeTableOutput, error)
}

// ── レスポンス型 ──────────────────────────────────────────────────────────────

// ECSHealth は ECS サービスのヘルス状態を表す。
type ECSHealth struct {
	ClusterName  string `json:"cluster_name"`
	ServiceName  string `json:"service_name"`
	Status       string `json:"status"`
	RunningCount int32  `json:"running_count"`
	DesiredCount int32  `json:"desired_count"`
	Healthy      bool   `json:"healthy"`
}

// TargetStatus は ALB ターゲット 1 台分のヘルス状態を表す。
type TargetStatus struct {
	ID     string `json:"id"`
	Port   int32  `json:"port"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// ALBHealth は ALB ターゲットグループ全体のヘルス情報を表す。
type ALBHealth struct {
	TargetGroupARN string         `json:"target_group_arn"`
	Targets        []TargetStatus `json:"targets"`
	AllHealthy     bool           `json:"all_healthy"`
}

// DynamoDBHealth は DynamoDB テーブルの状態を表す。
type DynamoDBHealth struct {
	TableName string `json:"table_name"`
	Status    string `json:"status"`
	Healthy   bool   `json:"healthy"`
}

// HealthSummary は Lambda レスポンスのトップレベル構造体。
type HealthSummary struct {
	ECS      *ECSHealth      `json:"ecs,omitempty"`
	ALB      *ALBHealth      `json:"alb,omitempty"`
	DynamoDB *DynamoDBHealth `json:"dynamodb,omitempty"`
	Healthy  bool            `json:"healthy"`
}

// ── Checker ──────────────────────────────────────────────────────────────────

// Checker はヘルスチェックロジックをまとめた構造体。
type Checker struct {
	ecsCli  ECSClient
	elbv2   ELBV2Client
	dynoCli DynamoDBClient
}

// CheckECS は指定したクラスター・サービスのヘルス状態を返す。
// clusterName または serviceName が空の場合は nil を返す。
func (c *Checker) CheckECS(ctx context.Context, clusterName, serviceName string) (*ECSHealth, error) {
	if clusterName == "" || serviceName == "" {
		return nil, nil
	}
	out, err := c.ecsCli.DescribeServices(ctx, &ecs.DescribeServicesInput{
		Cluster:  aws.String(clusterName),
		Services: []string{serviceName},
	})
	if err != nil {
		return nil, fmt.Errorf("ECS DescribeServices: %w", err)
	}
	if len(out.Services) == 0 {
		return &ECSHealth{
			ClusterName: clusterName,
			ServiceName: serviceName,
			Status:      "not-found",
			Healthy:     false,
		}, nil
	}
	svc := out.Services[0]
	status := aws.ToString(svc.Status)
	healthy := status == "ACTIVE" &&
		svc.RunningCount >= svc.DesiredCount &&
		svc.DesiredCount > 0
	return &ECSHealth{
		ClusterName:  clusterName,
		ServiceName:  serviceName,
		Status:       status,
		RunningCount: svc.RunningCount,
		DesiredCount: svc.DesiredCount,
		Healthy:      healthy,
	}, nil
}

// CheckALB は指定したターゲットグループのヘルス状態を返す。
// targetGroupARN が空の場合は nil を返す。
func (c *Checker) CheckALB(ctx context.Context, targetGroupARN string) (*ALBHealth, error) {
	if targetGroupARN == "" {
		return nil, nil
	}
	out, err := c.elbv2.DescribeTargetHealth(ctx, &elasticloadbalancingv2.DescribeTargetHealthInput{
		TargetGroupArn: aws.String(targetGroupARN),
	})
	if err != nil {
		return nil, fmt.Errorf("ALB DescribeTargetHealth: %w", err)
	}

	health := &ALBHealth{
		TargetGroupARN: targetGroupARN,
		AllHealthy:     true,
	}
	for _, thd := range out.TargetHealthDescriptions {
		state := string(thd.TargetHealth.State)
		ts := TargetStatus{
			State:  state,
			Reason: string(thd.TargetHealth.Reason),
		}
		if thd.Target != nil {
			if thd.Target.Id != nil {
				ts.ID = *thd.Target.Id
			}
			if thd.Target.Port != nil {
				ts.Port = *thd.Target.Port
			}
		}
		if state != string(elbv2types.TargetHealthStateEnumHealthy) {
			health.AllHealthy = false
		}
		health.Targets = append(health.Targets, ts)
	}
	return health, nil
}

// CheckDynamoDB は指定したテーブルの状態を返す。
// tableName が空の場合は nil を返す。
func (c *Checker) CheckDynamoDB(ctx context.Context, tableName string) (*DynamoDBHealth, error) {
	if tableName == "" {
		return nil, nil
	}
	out, err := c.dynoCli.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName),
	})
	if err != nil {
		return nil, fmt.Errorf("DynamoDB DescribeTable: %w", err)
	}
	status := string(out.Table.TableStatus)
	return &DynamoDBHealth{
		TableName: tableName,
		Status:    status,
		Healthy:   status == string(dynamodbtypes.TableStatusActive),
	}, nil
}

// ── Lambda ハンドラー ────────────────────────────────────────────────────────

func handler(ctx context.Context, _ json.RawMessage) (*HealthSummary, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("AWS 設定の読み込みに失敗しました: %w", err)
	}

	checker := &Checker{
		ecsCli:  ecs.NewFromConfig(cfg),
		elbv2:   elasticloadbalancingv2.NewFromConfig(cfg),
		dynoCli: dynamodb.NewFromConfig(cfg),
	}

	clusterName := os.Getenv("ECS_CLUSTER_NAME")
	serviceName := os.Getenv("ECS_SERVICE_NAME")
	targetGroupARN := os.Getenv("ALB_TARGET_GROUP_ARN")
	tableName := os.Getenv("DYNAMODB_TABLE_NAME")

	summary := &HealthSummary{Healthy: true}

	ecsHealth, err := checker.CheckECS(ctx, clusterName, serviceName)
	if err != nil {
		log.Printf("ECS チェックエラー: %v", err)
		summary.Healthy = false
	} else {
		summary.ECS = ecsHealth
		if ecsHealth != nil && !ecsHealth.Healthy {
			summary.Healthy = false
		}
	}

	albHealth, err := checker.CheckALB(ctx, targetGroupARN)
	if err != nil {
		log.Printf("ALB チェックエラー: %v", err)
		summary.Healthy = false
	} else {
		summary.ALB = albHealth
		if albHealth != nil && !albHealth.AllHealthy {
			summary.Healthy = false
		}
	}

	dynoHealth, err := checker.CheckDynamoDB(ctx, tableName)
	if err != nil {
		log.Printf("DynamoDB チェックエラー: %v", err)
		summary.Healthy = false
	} else {
		summary.DynamoDB = dynoHealth
		if dynoHealth != nil && !dynoHealth.Healthy {
			summary.Healthy = false
		}
	}

	return summary, nil
}

func main() {
	lambda.Start(handler)
}
