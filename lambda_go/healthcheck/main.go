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
	"log/slog"
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

// ── 設定 ──────────────────────────────────────────────────────────────────────

// defaultMetricsNamespace はメトリクスの既定の名前空間。
// 環境変数 METRICS_NAMESPACE で上書きできる。
const defaultMetricsNamespace = "EcsBedrockChat/HealthCheck"

// checkConfig は handler が環境変数から読み取る設定をまとめたもの。
type checkConfig struct {
	ClusterName    string
	ServiceName    string
	TargetGroupARN string
	TableName      string
}

// loadCheckConfig は環境変数から checkConfig を組み立てる。
func loadCheckConfig() checkConfig {
	return checkConfig{
		ClusterName:    os.Getenv("ECS_CLUSTER_NAME"),
		ServiceName:    os.Getenv("ECS_SERVICE_NAME"),
		TargetGroupARN: os.Getenv("ALB_TARGET_GROUP_ARN"),
		TableName:      os.Getenv("DYNAMODB_TABLE_NAME"),
	}
}

// ── ヘルスチェックの実行 ──────────────────────────────────────────────────────

// boolMetric は true を 1、false を 0 として返す。CloudWatch へ真偽値を送るために使う。
func boolMetric(ok bool) float64 {
	if ok {
		return 1
	}
	return 0
}

// recordCount はメトリクスを 1 件記録する。
// metrics が nil の場合は何もしない。記録に失敗しても処理は継続し、
// 警告ログを残すだけに留める（ヘルスチェック自体を止めないため）。
func recordCount(logger *slog.Logger, metrics *Metrics, name string, value float64) {
	if metrics == nil {
		return
	}
	if err := metrics.Count(name, value); err != nil {
		logger.Warn("メトリクスの記録に失敗しました", "metric", name, "error", err.Error())
	}
}

// runHealthCheck は各リソースのヘルスを確認し、結果をログとメトリクスへ出力する。
// AWS クライアントの生成と環境変数の読み取りは呼び出し側が行うため、
// この関数はテストから直接呼び出せる。
func runHealthCheck(
	ctx context.Context,
	checker *Checker,
	logger *slog.Logger,
	metrics *Metrics,
	cfg checkConfig,
) *HealthSummary {
	summary := &HealthSummary{Healthy: true}

	// ── ECS ──
	ecsHealth, err := checker.CheckECS(ctx, cfg.ClusterName, cfg.ServiceName)
	if err != nil {
		logger.Error("ECS のヘルスチェックに失敗しました",
			"cluster", cfg.ClusterName, "service", cfg.ServiceName, "error", err.Error())
		summary.Healthy = false
		recordCount(logger, metrics, "EcsCheckErrors", 1)
	} else {
		summary.ECS = ecsHealth
		if ecsHealth != nil {
			if !ecsHealth.Healthy {
				logger.Warn("ECS サービスが正常ではありません",
					"cluster", ecsHealth.ClusterName, "service", ecsHealth.ServiceName,
					"status", ecsHealth.Status,
					"running", ecsHealth.RunningCount, "desired", ecsHealth.DesiredCount)
				summary.Healthy = false
			}
			recordCount(logger, metrics, "EcsHealthy", boolMetric(ecsHealth.Healthy))
			recordCount(logger, metrics, "EcsRunningTasks", float64(ecsHealth.RunningCount))
		}
	}

	// ── ALB ──
	albHealth, err := checker.CheckALB(ctx, cfg.TargetGroupARN)
	if err != nil {
		logger.Error("ALB のヘルスチェックに失敗しました",
			"target_group_arn", cfg.TargetGroupARN, "error", err.Error())
		summary.Healthy = false
		recordCount(logger, metrics, "AlbCheckErrors", 1)
	} else {
		summary.ALB = albHealth
		if albHealth != nil {
			unhealthy := 0
			for _, t := range albHealth.Targets {
				if t.State != string(elbv2types.TargetHealthStateEnumHealthy) {
					unhealthy++
				}
			}
			if !albHealth.AllHealthy {
				logger.Warn("ALB ターゲットに異常があります",
					"target_group_arn", albHealth.TargetGroupARN,
					"targets", len(albHealth.Targets), "unhealthy", unhealthy)
				summary.Healthy = false
			}
			recordCount(logger, metrics, "AlbAllHealthy", boolMetric(albHealth.AllHealthy))
			recordCount(logger, metrics, "AlbUnhealthyTargets", float64(unhealthy))
		}
	}

	// ── DynamoDB ──
	dynoHealth, err := checker.CheckDynamoDB(ctx, cfg.TableName)
	if err != nil {
		logger.Error("DynamoDB のヘルスチェックに失敗しました",
			"table", cfg.TableName, "error", err.Error())
		summary.Healthy = false
		recordCount(logger, metrics, "DynamoDbCheckErrors", 1)
	} else {
		summary.DynamoDB = dynoHealth
		if dynoHealth != nil {
			if !dynoHealth.Healthy {
				logger.Warn("DynamoDB テーブルが正常ではありません",
					"table", dynoHealth.TableName, "status", dynoHealth.Status)
				summary.Healthy = false
			}
			recordCount(logger, metrics, "DynamoDbHealthy", boolMetric(dynoHealth.Healthy))
		}
	}

	recordCount(logger, metrics, "OverallHealthy", boolMetric(summary.Healthy))
	if summary.Healthy {
		logger.Info("ヘルスチェックが完了しました", "healthy", true)
	} else {
		logger.Warn("ヘルスチェックで異常を検知しました", "healthy", false)
	}
	return summary
}

// ── Lambda ハンドラー ────────────────────────────────────────────────────────

func handler(ctx context.Context, _ json.RawMessage) (*HealthSummary, error) {
	logger := NewLoggerFromEnv(LoggerOptions{})

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		logger.Error("AWS 設定の読み込みに失敗しました", "error", err.Error())
		return nil, fmt.Errorf("AWS 設定の読み込みに失敗しました: %w", err)
	}

	namespace := os.Getenv("METRICS_NAMESPACE")
	if namespace == "" {
		namespace = defaultMetricsNamespace
	}
	metrics, err := NewMetrics(MetricsOptions{Namespace: namespace})
	if err != nil {
		// メトリクスを初期化できなくても、ヘルスチェック自体は継続する。
		logger.Warn("メトリクスを初期化できませんでした",
			"namespace", namespace, "error", err.Error())
		metrics = nil
	}

	checker := &Checker{
		ecsCli:  ecs.NewFromConfig(cfg),
		elbv2:   elasticloadbalancingv2.NewFromConfig(cfg),
		dynoCli: dynamodb.NewFromConfig(cfg),
	}

	summary := runHealthCheck(ctx, checker, logger, metrics, loadCheckConfig())

	if metrics != nil {
		if err := metrics.Flush(); err != nil {
			logger.Warn("メトリクスの送信に失敗しました", "error", err.Error())
		}
	}
	return summary, nil
}

func main() {
	lambda.Start(handler)
}
